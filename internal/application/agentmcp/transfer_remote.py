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


def supervise(p, worker):
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


