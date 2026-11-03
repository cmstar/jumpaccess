"""上传任务的远端暂存、还原与排他发布；只操作本任务记录的对象。"""
import base64
import ctypes
import errno
import gzip
import json
import os
import shutil
import stat
import sys
import tarfile
import uuid

from transfer_remote import Problem, open_path, identity, supervise


def owned_directory(item):
    fd = open_path(item['path'], True)
    if identity(os.fstat(fd)) != item['identity']:
        os.close(fd)
        raise Problem('TEMPORARY_FILE_CHANGED')
    return fd


def announce(p, item):
    print('\n' + p['marker'] + ':' + json.dumps(dict(artifact=item)), file=sys.stderr, flush=True)


def create_stage(p):
    name = p['name']
    if not name.startswith('.jumpaccess-upload-') or '/' in name or '\\' in name or len(name) > 200:
        raise Problem('UNSUPPORTED_PATH')
    parent = open_path(p['stage'], True)
    try:
        os.mkdir(name, 0o700, dir_fd=parent)
        st = os.stat(name, dir_fd=parent, follow_symlinks=False)
        item = dict(path=os.path.join(p['stage'], name), identity=identity(st))
        announce(p, item)
        return item
    finally:
        os.close(parent)


def check_manifest(p):
    entries = p['entries']
    if not 1 <= len(entries) <= 1000 or not 0 <= p['size'] <= 1 << 30:
        raise Problem('INVALID_MANIFEST')
    seen = {}
    for e in entries:
        name = e['name']
        parts = name.split('/')
        if not name or len(name) > 4096 or any(part in ('', '.', '..') for part in parts) or '\\' in name or any(ord(c) < 32 for c in name):
            raise Problem('UNSAFE_ARCHIVE')
        key = name.casefold()
        if key in seen or not isinstance(e['size'], int) or not 0 <= e['size'] <= 1 << 53 or (e['directory'] and e['size']):
            raise Problem('INVALID_MANIFEST')
        for i in range(1, len(parts)):
            parent = '/'.join(parts[:i]).casefold()
            if parent not in seen or not seen[parent]['directory'] or seen[parent]['name'] != '/'.join(parts[:i]):
                raise Problem('INVALID_MANIFEST')
        seen[key] = e
    if p['format'] not in ('raw', 'gzip', 'tar.gz'):
        raise Problem('INVALID_MANIFEST')
    if p['format'] != 'tar.gz' and (len(entries) != 1 or entries[0]['directory'] or '/' in entries[0]['name']):
        raise Problem('INVALID_MANIFEST')
    return entries


def check_target(p):
    entries = check_manifest(p)
    fd = open_path(p['destination'], True)
    try:
        for e in entries:
            if '/' not in e['name']:
                try:
                    os.stat(e['name'], dir_fd=fd, follow_symlinks=False)
                except FileNotFoundError:
                    continue
                raise Problem('TARGET_CONFLICT')
        # 以实际创建检查写权限，不能只依赖访问模式位。
        name = '.jumpaccess-upload-probe-' + uuid.uuid4().hex
        probe = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=fd)
        os.close(probe)
        os.unlink(name, dir_fd=fd)
        return dict(identity=identity(os.fstat(fd)))
    finally:
        os.close(fd)


def rename_noreplace(srcfd, source, dstfd, destination):
    # rename 默认可替换目标；只采用平台提供的排他重命名。
    libc = ctypes.CDLL(None, use_errno=True)
    if sys.platform.startswith('linux'):
        fn = getattr(libc, 'renameat2', None)
        flag = 1  # RENAME_NOREPLACE
    elif sys.platform == 'darwin':
        fn = getattr(libc, 'renameatx_np', None)
        flag = 4  # RENAME_EXCL
    else:
        fn = None
    if fn is None:
        raise Problem('PUBLISH_UNSUPPORTED')
    fn.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    fn.restype = ctypes.c_int
    if fn(srcfd, os.fsencode(source), dstfd, os.fsencode(destination), flag) != 0:
        code = ctypes.get_errno()
        if code in (errno.EEXIST, errno.ENOTEMPTY): raise Problem('TARGET_CONFLICT')
        if code in (errno.ENOSYS, errno.EINVAL, errno.ENOTSUP): raise Problem('PUBLISH_UNSUPPORTED')
        raise OSError(code, os.strerror(code))


def write_entry(directory, entry, source):
    parts = entry['name'].split('/')
    fd = os.dup(directory)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd); fd = child
        if entry['directory']:
            os.mkdir(parts[-1], 0o700, dir_fd=fd)
            return
        out = os.open(parts[-1], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=fd)
        with os.fdopen(out, 'wb') as target:
            remaining = entry['size']
            while remaining:
                data = source.read(min(remaining, 1024 * 1024))
                if not data: raise Problem('INTEGRITY_FAILED')
                target.write(data); remaining -= len(data)
            if source.read(1): raise Problem('INTEGRITY_FAILED')
            target.flush(); os.fsync(target.fileno())
    finally:
        os.close(fd)


def restore(p):
    entries = check_manifest(p)
    check_target(p)
    parent = open_path(p['destination'], True)
    if p.get('destination_identity') and identity(os.fstat(parent)) != p['destination_identity']:
        os.close(parent); raise Problem('TEMPORARY_FILE_CHANGED')
    staging = owned_directory(p['stage'])
    restore_fd = None
    try:
        package = os.open('payload', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=staging)
        with os.fdopen(package, 'rb') as source:
            st = os.fstat(source.fileno())
            if not stat.S_ISREG(st.st_mode) or st.st_size != p['size']: raise Problem('INTEGRITY_FAILED')
            restore_name = '.jumpaccess-upload-' + uuid.uuid4().hex
            os.mkdir(restore_name, 0o700, dir_fd=parent)
            restore_fd = os.open(restore_name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
            restore_stage = dict(path=os.path.join(p['destination'], restore_name), identity=identity(os.fstat(restore_fd)))
            announce(p, restore_stage)
            try:
                if p['format'] == 'raw':
                    write_entry(restore_fd, entries[0], source)
                else:
                    with gzip.GzipFile(fileobj=source, mode='rb') as compressed:
                        if p['format'] == 'gzip':
                            write_entry(restore_fd, entries[0], compressed)
                        else:
                            with tarfile.open(fileobj=compressed, mode='r|') as archive:
                                count = 0
                                for header in archive:
                                    if count >= len(entries): raise Problem('UNSAFE_ARCHIVE')
                                    entry = entries[count]; count += 1
                                    if header.name != entry['name'] or header.size != entry['size'] or (header.isdir() if entry['directory'] else header.isreg()) is not True:
                                        raise Problem('UNSAFE_ARCHIVE')
                                    if header.issym() or header.islnk() or header.issparse(): raise Problem('UNSAFE_ARCHIVE')
                                    write_entry(restore_fd, entry, None if entry['directory'] else archive.extractfile(header))
                                if count != len(entries): raise Problem('INTEGRITY_FAILED')
                            # tar 结束后仍读到 gzip 尾部，验证校验和与截断。
                            while compressed.read(1024 * 1024): pass
            except (EOFError, OSError, tarfile.TarError) as error:
                if isinstance(error, PermissionError): raise
                raise Problem('INTEGRITY_FAILED')
        files = []
        failed = ''
        for e in entries:
            if '/' in e['name']: continue
            item = dict(path=os.path.join(p['destination'], e['name']), name=e['name'], size=e['size'], directory=e['directory'], status='not_published')
            if not failed:
                try:
                    # 路径替换或移走后，不向已脱离原路径的目录发布。
                    current = open_path(p['destination'], True)
                    try:
                        if identity(os.fstat(current)) != identity(os.fstat(parent)): raise Problem('TEMPORARY_FILE_CHANGED')
                    finally: os.close(current)
                    rename_noreplace(restore_fd, e['name'], parent, e['name'])
                    item['status'] = 'completed'
                except (Problem, OSError) as error:
                    failed = str(error) if isinstance(error, Problem) else 'FILE_PERMISSION_DENIED' if isinstance(error, PermissionError) else 'UPLOAD_PUBLISH_FAILED'
                    item['status'], item['error'] = 'failed', failed
            files.append(item)
        return dict(files=files, error=failed, restore_stage=restore_stage)
    finally:
        if restore_fd is not None: os.close(restore_fd)
        os.close(staging); os.close(parent)


def remove_tree(fd):
    for name in os.listdir(fd):
        st = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if stat.S_ISDIR(st.st_mode):
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            try: remove_tree(child)
            finally: os.close(child)
            os.rmdir(name, dir_fd=fd)
        else:
            os.unlink(name, dir_fd=fd)


def cleanup(p):
    for item in p['files']:
        parent = open_path(os.path.dirname(item['path']), True)
        name = os.path.basename(item['path'])
        try:
            if not name.startswith('.jumpaccess-upload-'): raise Problem('INVALID_TEMPORARY_FILE')
            try: fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
            except FileNotFoundError: continue
            try:
                if identity(os.fstat(fd)) != item['identity']: raise Problem('TEMPORARY_FILE_CHANGED')
                remove_tree(fd)
                if identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) != item['identity']: raise Problem('TEMPORARY_FILE_CHANGED')
                os.rmdir(name, dir_fd=parent)
            finally: os.close(fd)
        finally: os.close(parent)
    return dict(removed=True)


def worker(p):
    if 'input_bytes' in p:
        size = p['input_bytes']
        if not isinstance(size, int) or not 0 < size <= 8 * 1024 * 1024: raise Problem('INVALID_MANIFEST')
        data = sys.stdin.buffer.read(size)
        if len(data) != size: raise Problem('INVALID_MANIFEST')
        p.update(json.loads(data))
    op = p['op']
    if op == 'environment':
        return dict(home=os.path.expanduser('~'), account=__import__('pwd').getpwuid(os.getuid()).pw_name, rz=bool(shutil.which('rz')))
    if op == 'create_stage': return create_stage(p)
    if op == 'check_target': return check_target(p)
    if op == 'restore': return restore(p)
    if op == 'cleanup': return cleanup(p)
    if op == 'check_probe':
        fd = owned_directory(p['stage'])
        try:
            probe = os.open('probe', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
            with os.fdopen(probe, 'rb') as f:
                if not stat.S_ISREG(os.fstat(f.fileno()).st_mode) or f.read(1024) != p['value'].encode('ascii'): raise Problem('SFTP_ROOT_UNCONFIRMED')
            os.unlink('probe', dir_fd=fd)
            return dict(confirmed=True)
        finally: os.close(fd)
    if op == 'rz':
        fd = owned_directory(p['stage'])
        os.fchdir(fd); os.close(fd)
        executable = shutil.which('rz')
        if not executable: raise Problem('ZMODEM_UNAVAILABLE')
        os.execv(executable, [executable, '-b', '-e', '-y'])
    raise Problem('INVALID_OPERATION')


if __name__ == '__main__':
    supervise(json.loads(base64.b64decode(sys.argv[1])), worker)
