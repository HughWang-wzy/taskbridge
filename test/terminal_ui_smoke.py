"""Exercise real terminal handoff and cancellation using a local mock service."""

import json
import os
import platform
import re
import select
import struct
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


def main():
    if sys.platform == "win32":
        print("PTY smoke requires Unix; Windows behavior is covered by Go/CI tests")
        return
    import fcntl
    import pty
    import termios

    target = "linux-amd64" if sys.platform != "darwin" else ("darwin-arm64" if platform.machine() == "arm64" else "darwin-amd64")
    binary = ROOT / "dist" / target / "tb"
    calls = []
    sessions = []

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers.get("content-length", 0))))
            calls.append((self.path, body))
            data = b'{"ok":true}'
            self.send_response(200)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()

    try:
        with tempfile.TemporaryDirectory() as temporary:
            temp = Path(temporary)
            config = temp / "config.json"
            codex = temp / "codex.json"
            url = f"http://127.0.0.1:{server.server_port}"
            initial = json.dumps({"url": url, "token": "test", "ntfy_url": url, "ntfy_topic": "test", "min_success_seconds": -1})
            config.write_text(initial)
            codex.write_text(initial)
            env = dict(os.environ, TB_CONFIG=str(config), TERM="xterm-256color", NO_COLOR="1")

            class Session:
                def __init__(self, *args):
                    self.master, self.slave = pty.openpty()
                    fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 100, 0, 0))
                    self.original = termios.tcgetattr(self.slave)
                    self.proc = subprocess.Popen([str(binary), *args], stdin=self.slave, stdout=self.slave, stderr=self.slave, env=env, cwd=temp, start_new_session=True)
                    self.output = b""
                    self.position = 0
                    self.closed = False
                    sessions.append(self)

                def send(self, value):
                    os.write(self.master, value.encode() if isinstance(value, str) else value)

                def expect(self, value, timeout=8, title=False):
                    needle = value.encode()
                    pattern = re.compile(rb"TaskBridge\x1b\[0m  \xc2\xb7  [^\r\n]*" + re.escape(needle)) if title else None
                    deadline = time.monotonic() + timeout
                    while time.monotonic() < deadline:
                        match = pattern.search(self.output, self.position) if pattern else None
                        found = self.output.find(needle, self.position) if not pattern else -1
                        if match or found >= 0:
                            self.position = match.end() if match else found + len(needle)
                            return
                        ready, _, _ = select.select([self.master], [], [], 0.05)
                        if ready:
                            try:
                                self.output += os.read(self.master, 65536)
                            except OSError:
                                break
                    raise AssertionError(f"missing {value!r}: {self.output[-4500:]!r}")

                def expect_title(self, value):
                    self.expect(value, title=True)

                def finish(self, code=0):
                    try:
                        result = self.proc.wait(timeout=8)
                        assert result == code, (result, self.output[-3000:])
                        restored = termios.tcgetattr(self.slave)
                        differences = {name: (before, after) for name, before, after in zip(
                            ("iflag", "oflag", "cflag", "lflag", "ispeed", "ospeed", "cc"), self.original, restored
                        ) if before != after}
                        assert not differences, f"terminal mode was not restored: {differences!r}"
                    finally:
                        if self.proc.poll() is None:
                            self.proc.kill()
                            self.proc.wait()
                        os.close(self.master)
                        os.close(self.slave)
                        self.closed = True

            session = Session()
            session.expect_title("Tasks & reminders")
            session.send(b"\x1b")
            session.finish()
            assert not calls and config.read_text() == initial

            session = Session("settings")
            session.expect_title("Reminder settings")
            session.send(b"\x1b[B\x1b[D\x1b")
            session.finish()
            assert config.read_text() == initial, "Esc saved a settings draft"

            session = Session("settings")
            session.expect_title("Reminder settings")
            session.send(b"\x1b[D\r")  # standard -> low-noise, then save
            session.finish()
            assert json.loads(config.read_text())["alert_priorities"]["success"] == 2

            session = Session()
            session.expect_title("Tasks & reminders")
            session.send("1")
            session.expect_title("Run task")
            raw = "printf 'TASK_READY\\n'; IFS= read -r value; printf 'CHILD:%s\\n' \"$value\""
            session.send("\x1b[200~" + raw + "\x1b[201~\r")
            session.expect_title("Task name")
            session.send("\r")
            session.expect_title("Review task")
            session.send("1")
            session.expect("TASK_READY\r\n")
            assert termios.tcgetattr(session.slave)[3] & termios.ICANON, "child inherited raw mode"
            session.send("hello from terminal\n")
            session.expect("CHILD:hello from terminal")
            session.finish()
            assert any(path == "/v1/tasks/start" for path, _ in calls)
            assert any(path.endswith("/finish") for path, _ in calls)

            # A saved multiline command is editable and remains exact on disk.
            session = Session()
            session.expect_title("Tasks & reminders")
            session.send("1")
            session.expect_title("Run task")
            session.send("\x1b[200~printf '%s\\n' '$literal words' \\\n  | cat\x1b[201~\r")
            session.expect_title("Task name")
            session.send("\r")
            session.expect_title("Review task")
            session.send("5")
            session.expect_title("Favorite key")
            session.send("\x15favorite1\r")
            session.expect_title("Review task")
            session.send(b"\x1b")
            session.expect_title("Tasks & reminders")
            session.send("2")
            session.expect_title("Saved tasks")
            session.send("1")
            session.expect_title("Review task")
            session.send("2")
            session.expect_title("Edit command")
            session.send("\x15printf 'EDITED_MARK\\n'\r")
            session.expect_title("Review task")
            session.send("5")
            session.expect_title("Review task")
            session.send("1")
            session.expect("EDITED_MARK\r\n")
            session.finish()
            tasks_path = temp / "tasks.json"
            tasks = json.loads(tasks_path.read_text())
            assert tasks[0]["key"] == "favorite1" and tasks[0]["command"] == ["printf 'EDITED_MARK\\n'"]

            session = Session()
            session.expect_title("Tasks & reminders")
            session.send("2")
            session.expect_title("Saved tasks")
            session.send("1")
            session.expect_title("Review task")
            session.send("6")
            session.expect_title("Delete favorite?")
            session.send(b"\x1b")
            session.expect_title("Review task")
            assert json.loads(tasks_path.read_text()), "cancel deleted favorite"
            session.send("6")
            session.expect_title("Delete favorite?")
            session.send("1")
            session.expect_title("Tasks & reminders")
            session.send(b"\x1b")
            session.finish()
            assert json.loads(tasks_path.read_text()) == []
            assert codex.read_text() == initial, "TB_CONFIG changed another client config"
        print("terminal UI PTY smoke tests passed")
    finally:
        for session in sessions:
            if not session.closed:
                if session.proc.poll() is None:
                    session.proc.kill()
                    session.proc.wait()
                os.close(session.master)
                os.close(session.slave)
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
