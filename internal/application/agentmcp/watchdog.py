import base64
import json
import os
import select
import signal
import subprocess
import sys
import time

args = json.loads(base64.b64decode(sys.argv[1]))
marker = args["marker"]


def report(event):
    data = "\x1e" + marker + ":" + json.dumps(event, separators=(",", ":")) + "\x1f"
    os.write(2, data.encode("utf-8"))


probe = args.get("probe", False)
assert os.name == "posix"
stop_requested = False
stdin_stop = False
stdin_open = True
control_buffer = b""
stop_frame = (marker + ":stop").encode("ascii")


def request_stop(signum, frame):
    global stop_requested
    stop_requested = True


signal.signal(signal.SIGTERM, request_stop)
signal.signal(signal.SIGINT, request_stop)
environment = os.environ.copy()
environment.update(args.get("env", {}))
timeout_ms = 3000 if probe else args["timeout_ms"]
started = time.monotonic()
try:
    if stop_requested and not probe:
        report({"event": "not_started", "cancelled": True})
        sys.exit(0)
    command = [sys.executable, "-c", "import time;time.sleep(30)"] if probe else ["/bin/sh", "-c", args["command"]]
    child = subprocess.Popen(command, cwd=args.get("cwd"), env=environment,
                             stdin=subprocess.DEVNULL, start_new_session=True)
except (OSError, ValueError):
    report({"event": "context_failed"})
    sys.exit(125)

if probe:
    # 先确认子进程已建立，再由本执行 stdin 控制消息触发停止，验证整个转发路径。
    report({"event": "ready"})

timed_out = False
stop_sent = False
while child.poll() is None:
    if stdin_open and select.select([0], [], [], 0)[0]:
        data = os.read(0, 4096)
        if not data:
            # EOF 不是取消确认，也不能造成忙循环；远端截止仍然有效。
            stdin_open = False
        else:
            control_buffer += data
            while b"\n" in control_buffer:
                line, control_buffer = control_buffer.split(b"\n", 1)
                if line == stop_frame:
                    stdin_stop = True
                    stop_requested = True
            if len(control_buffer) > 4096:
                control_buffer = b""
    if stop_requested or time.monotonic() - started >= timeout_ms / 1000:
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
if probe:
    report({"event": "probe", "supported": stdin_stop and stop_sent and code < 0})
    sys.exit(0)
report({"event": "result", "exit_code": code if code >= 0 else None,
        "exit_signal": signal.Signals(-code).name[3:] if code < 0 else "",
        "timed_out": timed_out, "cancelled": stop_requested and stop_sent and code < 0})
# 原始退出信息来自控制记录；SSH 状态用于确认封装本身正常完成。
sys.exit(0)
