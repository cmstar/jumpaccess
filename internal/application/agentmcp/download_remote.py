"""通过一次 SSH exec 运行；不在服务器安装程序或创建控制文件。"""
import base64
import errno
import io
import json
import os
import selectors
import shutil
import signal
import stat
import sys
import time


class Problem(Exception):
    pass


def utc():
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())


def open_path(name, directory=False):
    # 所有父目录也拒绝链接；O_NONBLOCK 避免恶意替换成 FIFO 后挂起。
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        parts = [p for p in name.split('/') if p]
        if not name.startswith('/') or any(p in ('.', '..') for p in parts):
            raise Problem('UNSUPPORTED_PATH')
        for index, part in enumerate(parts):
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            if directory or index < len(parts) - 1:
                flags |= os.O_DIRECTORY
            child = os.open(part, flags, dir_fd=fd)
            os.close(fd)
            fd = child
        return fd
    except BaseException:
        os.close(fd)
        raise


def identity(st):
    return [st.st_dev, st.st_ino]


class Capture(io.RawIOBase):
    def __init__(self, fd):
        self.file = os.fdopen(fd, 'rb')
        self.size = os.fstat(fd).st_size
        self.remaining = self.size
        self.started = utc()

    def read(self, size=-1):
        size = min(self.remaining, size if size >= 0 else 1024 * 1024)
        if not size:
            return b''
        data = self.file.read(size)
        if len(data) != size:
            raise Problem('SOURCE_CHANGED')
        self.remaining -= len(data)
        return data

    def close(self):
        self.file.close()
        super().close()


class LimitedWriter:
    """限制传输包的实际写入量，原始内容大小不参与此限制。"""
    def __init__(self, target, limit):
        self.target, self.limit, self.written = target, limit, 0

    def write(self, data):
        if len(data) > self.limit - self.written:
            raise Problem('TRANSFER_TOO_LARGE')
        size = self.target.write(data)
        self.written += size
        return size

    def flush(self):
        self.target.flush()

    def tell(self):
        return self.written


def independent_paths(paths):
    # 先验证每个请求路径，再去重；不存在或不支持的子路径不能被父路径掩盖。
    validated = {}
    for source in paths:
        fd = open_path(source)
        try:
            mode = os.fstat(fd).st_mode
            if not stat.S_ISDIR(mode) and not stat.S_ISREG(mode):
                raise Problem('UNSUPPORTED_FILE_TYPE')
            validated[source] = stat.S_ISDIR(mode)
        finally:
            os.close(fd)
    return [source for source in validated if not any(
        other != source and directory and os.path.commonpath([source, other]) == other
        for other, directory in validated.items())]


def inventory(paths, stage):
    entries, names = [], set()

    def visit(source, name):
        fd = open_path(source)
        try:
            st = os.fstat(fd)
            if not stat.S_ISDIR(st.st_mode) and not stat.S_ISREG(st.st_mode):
                raise Problem('UNSUPPORTED_FILE_TYPE')
            entry = dict(source=source, name=name, directory=stat.S_ISDIR(st.st_mode), identity=identity(st))
            entries.append(entry)
            if len(entries) > 1000:
                raise Problem('TOO_MANY_FILES')
            if entry['directory']:
                if os.path.commonpath([source, stage]) == source:
                    raise Problem('STAGING_INSIDE_SOURCE')
                for item in sorted(os.listdir(fd)):
                    visit(os.path.join(source, item), name + '/' + item)
        finally:
            os.close(fd)

    for source in paths:
        name = os.path.basename(source) or 'root'
        original, suffix = name, 1
        while name.casefold() in names:
            suffix += 1
            name = original + ' (' + str(suffix) + ')'
        names.add(name.casefold())
        visit(source, name)
    return entries


def prepare(p):
    stage = p['stage']
    paths = independent_paths(p['paths'])
    entries = inventory(paths, stage)
    wrapped = len(paths) > 1
    root_name = entries[0]['name']
    if wrapped:
        root_name = p.get('result_name', 'JumpAccess-download')
        if not root_name or '/' in root_name or root_name in ('.', '..'):
            raise Problem('UNSUPPORTED_PATH')
        for entry in entries:
            entry['name'] = root_name + '/' + entry['name']
        entries.insert(0, dict(source='', name=root_name, directory=True))
    single = len(entries) == 1 and not entries[0]['directory']
    compressed = ('.gz', '.zip', '.bz2', '.xz', '.7z', '.rar', '.zst', '.tgz', '.jar', '.war')
    mode = 'raw' if single and entries[0]['source'].lower().endswith(compressed) else ('gzip' if single else 'tar.gz')
    compression_skipped = False
    try:
        import gzip
        import tarfile
    except ImportError:
        if not single:
            raise Problem('COMPRESSION_UNAVAILABLE')
        compression_skipped = mode != 'raw'
        mode = 'raw'
    suffix = {'raw': '.bin', 'gzip': '.gz', 'tar.gz': '.tar.gz'}[mode]
    name = p['stem'] + suffix
    directory = open_path(stage, True)
    try:
        fd = os.open(name + '.part', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
        artifact_identity = identity(os.fstat(fd))
        print('\n' + p['marker'] + ':' + json.dumps(dict(artifact=dict(path=os.path.join(stage, name + '.part'), identity=artifact_identity))), file=sys.stderr, flush=True)
        with os.fdopen(fd, 'wb') as raw:
            limited = LimitedWriter(raw, min(p.get('max_bytes', 1 << 30), 1 << 30))
            archive = None
            stream = limited
            if mode == 'gzip':
                stream = gzip.GzipFile(filename='', mode='wb', fileobj=limited, mtime=0)
            elif mode == 'tar.gz':
                archive = tarfile.open(fileobj=limited, mode='w|gz', format=tarfile.PAX_FORMAT)
            try:
                for entry in entries:
                    if entry['directory']:
                        info = tarfile.TarInfo(entry['name'])
                        info.type, info.mode = tarfile.DIRTYPE, 0o700
                        archive.addfile(info)
                        continue
                    source = open_path(entry['source'])
                    st = os.fstat(source)
                    if not stat.S_ISREG(st.st_mode) or identity(st) != entry['identity']:
                        os.close(source)
                        raise Problem('SOURCE_CHANGED')
                    with Capture(source) as capture:
                        if archive:
                            info = tarfile.TarInfo(entry['name'])
                            info.size, info.mode = capture.size, 0o600
                            archive.addfile(info, capture)
                        else:
                            while capture.remaining:
                                stream.write(capture.read(1024 * 1024))
                        entry.update(size=capture.size, captured_at=capture.started)
            finally:
                if archive:
                    archive.close()
                elif stream is not limited:
                    stream.close()
            raw.flush()
            os.fsync(raw.fileno())
        # 排他发布同一 inode，不复制内容，也不覆盖同名文件。
        os.link(name + '.part', name, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
        print('\n' + p['marker'] + ':' + json.dumps(dict(artifact=dict(path=os.path.join(stage, name), identity=artifact_identity))), file=sys.stderr, flush=True)
        os.unlink(name + '.part', dir_fd=directory)
        # 准备完毕后只读取文件大小，不为内容哈希再次扫描整个暂存包。
        with os.fdopen(os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory), 'rb') as result:
            size = os.fstat(result.fileno()).st_size
        suggested_name = root_name + {'raw': '', 'gzip': '.gz', 'tar.gz': '.tar.gz'}[mode]
        return dict(path=os.path.join(stage, name), format=mode, compression_skipped=compression_skipped, size=size, identity=artifact_identity, entries=entries, suggested_name=suggested_name, root_name=root_name, wrapped=wrapped)
    finally:
        os.close(directory)


def worker(p):
    op = p['op']
    if op == 'environment':
        return dict(home=os.path.expanduser('~'), account=__import__('pwd').getpwuid(os.getuid()).pw_name, sz=bool(shutil.which('sz')))
    if op == 'prepare':
        return prepare(p)
    if op == 'probe':
        for source in p.get('paths', []):
            try:
                fd = open_path(source)
            except OSError as error:
                if error.errno in (errno.ELOOP, errno.ENOTDIR):
                    raise Problem('UNSUPPORTED_FILE_TYPE')
                raise Problem('SOURCE_UNREADABLE')
            try:
                mode = os.fstat(fd).st_mode
                if not stat.S_ISDIR(mode) and not stat.S_ISREG(mode):
                    raise Problem('UNSUPPORTED_FILE_TYPE')
                if stat.S_ISDIR(mode) and os.path.commonpath([source, p['stage']]) == source:
                    raise Problem('STAGING_INSIDE_SOURCE')
            finally:
                os.close(fd)
        directory = open_path(p['stage'], True)
        try:
            fd = os.open(p['name'], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
            with os.fdopen(fd, 'wb') as out:
                owned = identity(os.fstat(out.fileno()))
                print('\n' + p['marker'] + ':' + json.dumps(dict(artifact=dict(path=os.path.join(p['stage'], p['name']), identity=owned))), file=sys.stderr, flush=True)
                out.write(p['value'].encode('ascii'))
            return dict(identity=owned)
        finally:
            os.close(directory)
    if op == 'cleanup':
        removed = []
        for item in p['files']:
            directory = open_path(os.path.dirname(item['path']), True)
            try:
                name = os.path.basename(item['path'])
                if not name.startswith(('.jumpaccess-probe-', '.jumpaccess-download-')):
                    raise Problem('INVALID_TEMPORARY_FILE')
                try:
                    st = os.stat(name, dir_fd=directory, follow_symlinks=False)
                except FileNotFoundError:
                    continue
                if not stat.S_ISREG(st.st_mode) or identity(st) != item['identity']:
                    raise Problem('TEMPORARY_FILE_CHANGED')
                os.unlink(name, dir_fd=directory)
                removed.append(item['path'])
            finally:
                os.close(directory)
        return dict(removed=removed)
    if op == 'sz':
        executable = shutil.which('sz')
        if not executable:
            raise Problem('ZMODEM_UNAVAILABLE')
        os.execv(executable, [executable, '-b', '-e', '--', p['path']])
    raise Problem('INVALID_OPERATION')


def supervise(p):
    # 父进程只转发二进制 stdin；EOF 是取消请求，结果确认独立写往 stderr。
    incoming, outgoing = os.pipe()
    pid = os.fork()
    if pid == 0:
        os.close(outgoing)
        os.setsid()
        os.dup2(incoming, 0)
        os.close(incoming)
        try:
            result = worker(p)
            print(json.dumps(dict(ok=True, data=result), ensure_ascii=True), flush=True)
            os._exit(0)
        except BaseException as error:
            # 不输出 traceback、文件正文或环境变量。
            code = str(error) if isinstance(error, Problem) else ('FILE_PERMISSION_DENIED' if isinstance(error, PermissionError) else 'REMOTE_PREPARATION_FAILED')
            if isinstance(error, OSError) and error.errno in (errno.ELOOP, errno.ENOTDIR):
                code = 'UNSUPPORTED_FILE_TYPE'
            print(json.dumps(dict(ok=False, code=code)), flush=True)
            os._exit(1)
    os.close(incoming)
    selector = selectors.DefaultSelector()
    selector.register(0, selectors.EVENT_READ)
    os.set_blocking(outgoing, False)
    pending = b''
    deadline = time.monotonic() + p['timeout_ms'] / 1000
    reason, stopping = '', None
    while True:
        ended, status = os.waitpid(pid, os.WNOHANG)
        if ended:
            break
        now = time.monotonic()
        if not reason and now >= deadline:
            reason = 'timed_out'
        for key, _ in selector.select(0.02):
            chunk = os.read(key.fd, 8192)
            if chunk:
                pending += chunk
                if len(pending) > 1024 * 1024:
                    reason = 'input_overflow'
            else:
                selector.unregister(0)
                if not reason:
                    reason = 'cancelled'
        if reason:
            sig = signal.SIGTERM if stopping is None else signal.SIGKILL
            if stopping is None or now - stopping >= 0.5:
                try:
                    os.killpg(pid, sig)
                except ProcessLookupError:
                    # child 可能还未完成 setsid；终止自己的直属子进程。
                    try:
                        os.kill(pid, sig)
                    except ProcessLookupError:
                        pass
                if stopping is None:
                    stopping = now
        elif pending:
            try:
                sent = os.write(outgoing, pending)
                pending = pending[sent:]
            except BlockingIOError:
                pass
            except BrokenPipeError:
                pending = b''
    os.close(outgoing)
    exit_code = os.WEXITSTATUS(status) if os.WIFEXITED(status) else -os.WTERMSIG(status)
    print('\n' + p['marker'] + ':' + json.dumps(dict(stopped=True, reason=reason, exit_code=exit_code)), file=sys.stderr, flush=True)


if __name__ == '__main__':
    supervise(json.loads(base64.b64decode(sys.argv[1])))
