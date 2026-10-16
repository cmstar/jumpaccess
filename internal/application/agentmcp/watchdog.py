import base64
import json
import os
import signal
import subprocess
import sys
import time

args = json.loads(base64.b64decode(sys.argv[1]))
marker = args["marker"]


def report(event):
    data = "\x1e" + marker + ":" + json.dumps(event, separators=(",", ":")) + "\x1f"
    os.write(2, data.encode("utf-8"))


if args.get("probe"):
    # 只探测本封装所依赖的能力，不执行用户命令、不写入远端文件。
    assert os.name == "posix"
    child = subprocess.Popen([sys.executable, "-c", "import time;time.sleep(30)"], start_new_session=True)
    try:
        assert os.getpgid(child.pid) == child.pid
        os.killpg(child.pid, signal.SIGTERM)
        child.wait(timeout=2)
        assert child.returncode == -signal.SIGTERM
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
    report({"event": "probe", "supported": True})
    sys.exit(0)

stop_requested = False


def request_stop(signum, frame):
    global stop_requested
    stop_requested = True


signal.signal(signal.SIGTERM, request_stop)
signal.signal(signal.SIGINT, request_stop)
environment = os.environ.copy()
environment.update(args["env"])
started = time.monotonic()
try:
    if stop_requested:
        report({"event": "not_started", "cancelled": True})
        sys.exit(0)
    child = subprocess.Popen(["/bin/sh", "-c", args["command"]], cwd=args["cwd"], env=environment,
                             stdin=subprocess.DEVNULL, start_new_session=True)
except (OSError, ValueError):
    report({"event": "context_failed"})
    sys.exit(125)

timed_out = False
stop_sent = False
while child.poll() is None:
    if stop_requested or time.monotonic() - started >= args["timeout_ms"] / 1000:
        timed_out = not stop_requested
        try:
            os.killpg(child.pid, signal.SIGTERM)
            stop_sent = True
        except ProcessLookupError:
            pass
        grace = time.monotonic() + 5
        # 即使主进程提前退出，也清理仍留在本次进程组内的子进程。
        while time.monotonic() < grace:
            child.poll()
            try:
                os.killpg(child.pid, 0)
            except ProcessLookupError:
                break
            time.sleep(0.02)
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        break
    time.sleep(0.02)

code = child.wait()
report({"event": "result", "exit_code": code if code >= 0 else None,
        "exit_signal": signal.Signals(-code).name[3:] if code < 0 else "",
        "timed_out": timed_out, "cancelled": stop_requested and stop_sent and code < 0})
# 原始退出信息来自控制记录；SSH 状态用于确认封装本身正常完成。
sys.exit(0)
