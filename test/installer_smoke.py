"""Exercise the Linux installer against local release assets and a mock Worker."""

import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


class DoctorHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/v1/doctor":
            self.send_error(404)
            return
        body = b'{"ok":true,"pending_notifications":0,"failed_notification_attempts":0}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


def run_installer(env):
    return subprocess.run(
        ["bash", str(ROOT / "scripts/install.sh")],
        env=env,
        text=True,
        capture_output=True,
        timeout=30,
        check=False,
    )


def main():
    if sys.platform == "darwin":
        target = "darwin-arm64" if platform.machine() == "arm64" else "darwin-amd64"
    else:
        target = "linux-amd64"
    archive_name = f"taskbridge-{target}.tar.gz"
    server = ThreadingHTTPServer(("127.0.0.1", 0), DoctorHandler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            home = root / "home"
            home.mkdir()
            env = os.environ.copy()
            env.update(
                {
                    "HOME": str(home),
                    "TB_RELEASE_BASE": (ROOT / "release").as_uri(),
                    "TB_INSTALL_DIR": str(root / "bin"),
                    "XDG_CONFIG_HOME": str(root / "config"),
                    "TB_WORKER_URL": f"http://127.0.0.1:{server.server_port}",
                    "TB_CLIENT_TOKEN": "test-client-token",
                    "TB_NTFY_TOPIC": "test-topic",
                    "TB_ENABLE_CODEX": "0",
                    "TB_ENABLE_RELAY": "0",
                }
            )
            result = run_installer(env)
            assert result.returncode == 0, result.stderr
            config_path = (
                home / "Library/Application Support/taskbridge/config.json"
                if sys.platform == "darwin"
                else root / "config/taskbridge/config.json"
            )
            config = json.loads(config_path.read_text())
            assert config["url"] == env["TB_WORKER_URL"]
            assert config["ntfy_topic"] == "test-topic"
            assert (root / "bin/tb").is_file()

            fake_commands = root / "fake-commands"
            fake_commands.mkdir()
            fake_codex = fake_commands / "codex"
            fake_codex.write_text(
                "#!/bin/sh\n"
                "if [ \"$1 $2\" = \"mcp get\" ]; then exit 1; fi\n"
                "if [ \"$1 $2\" = \"mcp add\" ]; then exit 0; fi\n"
                "exit 2\n"
            )
            fake_codex.chmod(0o755)
            env["PATH"] = str(fake_commands) + os.pathsep + env["PATH"]
            env["TB_ENABLE_CODEX"] = "1"
            result = run_installer(env)
            assert result.returncode == 0, result.stderr
            assert (config_path.parent / "codex.json").is_file()
            assert (home / ".codex/hooks.json").is_file()
            assert not (home / ".codex/AGENTS.md").exists()

            bad_release = root / "bad-release"
            bad_release.mkdir()
            shutil.copy(ROOT / "release/SHA256SUMS", bad_release / "SHA256SUMS")
            archive = bad_release / archive_name
            shutil.copy(ROOT / "release" / archive_name, archive)
            with archive.open("ab") as output:
                output.write(b"tampered")
            env["TB_RELEASE_BASE"] = bad_release.as_uri()
            env["TB_INSTALL_DIR"] = str(root / "bad-bin")
            result = run_installer(env)
            assert result.returncode != 0 and "checksum mismatch" in result.stderr.lower()
            assert not (root / "bad-bin/tb").exists()
        print("installer smoke tests passed")
    finally:
        server.shutdown()


if __name__ == "__main__":
    main()
