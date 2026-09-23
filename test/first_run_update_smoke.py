"""An existing release checkout upgrades without losing ignored setup state."""

import os
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


def command(*args, cwd=None):
    return subprocess.run(args, cwd=cwd, check=True, text=True, capture_output=True).stdout.strip()


def main():
    with tempfile.TemporaryDirectory() as temporary:
        root = Path(temporary)
        source = root / "source"
        remote = root / "remote.git"
        checkout = root / "checkout"
        source.mkdir()
        command("git", "init", str(source))
        command("git", "init", "--bare", str(remote))
        command("git", "config", "user.name", "TaskBridge test", cwd=source)
        command("git", "config", "user.email", "test@example.invalid", cwd=source)
        (source / "scripts").mkdir()
        (source / "scripts/setup.mjs").write_text("// old\n")
        command("git", "add", ".", cwd=source)
        command("git", "commit", "-m", "old", cwd=source)
        command("git", "tag", "v0.4.4", cwd=source)
        (source / "scripts/setup.mjs").write_text("// new\n")
        command("git", "commit", "-am", "new", cwd=source)
        command("git", "tag", "v0.4.6", cwd=source)
        command("git", "remote", "add", "origin", str(remote), cwd=source)
        command("git", "push", "origin", "--tags", cwd=source)
        command("git", "clone", "--branch", "v0.4.4", str(remote), str(checkout))
        (checkout / ".local").mkdir()
        state = checkout / ".local/setup.json"
        state.write_text('{"topic":"Cospeak3"}')

        fake_bin = root / "bin"
        fake_bin.mkdir()
        node = fake_bin / "node"
        node.write_text("#!/bin/sh\nif [ \"$1\" = -p ]; then echo 22; else echo setup-invoked-proxy=$NODE_USE_ENV_PROXY; fi\n")
        node.chmod(0o755)
        npm = fake_bin / "npm"
        npm.write_text("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 10.0; else echo npm-ci; fi\n")
        npm.chmod(0o755)
        home = root / "home"
        home.mkdir()
        env = os.environ.copy()
        env.update({"HOME": str(home), "PATH": str(fake_bin) + os.pathsep + env["PATH"], "TB_SETUP_DIR": str(checkout)})
        result = subprocess.run(
            ["bash", str(ROOT / "scripts/first-run.sh")], env=env, capture_output=True, text=True, timeout=30
        )
        assert result.returncode == 0, result.stdout + result.stderr
        assert "setup-invoked-proxy=1" in result.stdout
        assert command("git", "rev-parse", "HEAD", cwd=checkout) == command("git", "rev-list", "-n", "1", "v0.4.6", cwd=source)
        assert (checkout / "scripts/setup.mjs").read_text() == "// new\n"
        assert state.read_text() == '{"topic":"Cospeak3"}'
        print("first-run checkout update smoke test passed")


if __name__ == "__main__":
    main()
