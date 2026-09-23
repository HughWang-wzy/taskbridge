"""Exercise the Linux first-run repair path with a local Node release fixture."""

import hashlib
import os
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


def main():
    with tempfile.TemporaryDirectory() as temporary:
        root = Path(temporary)
        home = root / "home"
        home.mkdir()
        release = root / "release"
        release.mkdir()
        package = root / "node-v22.1.0-linux-x64"
        (package / "bin").mkdir(parents=True)
        node = package / "bin/node"
        node.write_text(
            "#!/bin/sh\n"
            "case \"$1\" in\n"
            "  -p) echo 22;;\n"
            "  --version) echo v22.1.0;;\n"
            "  *) echo setup-invoked;;\n"
            "esac\n"
        )
        node.chmod(0o755)
        npm = package / "bin/npm"
        npm.write_text("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 10.0.0; else echo npm-ci; fi\n")
        npm.chmod(0o755)
        archive = release / f"{package.name}.tar.gz"
        with tarfile.open(archive, "w:gz") as bundle:
            bundle.add(package, arcname=package.name)
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (release / "SHASUMS256.txt").write_text(f"{digest}  {archive.name}\n")

        commands = root / "commands"
        commands.mkdir()
        git = commands / "git"
        git.write_text(
            "#!/bin/sh\n"
            "if [ \"$1\" = --version ]; then echo 'git version 2'; exit 0; fi\n"
            "if [ \"$1\" = clone ]; then\n"
            "  for last; do :; done\n"
            "  mkdir -p \"$last/scripts\"\n"
            "  : > \"$last/scripts/setup.mjs\"\n"
            "  exit 0\n"
            "fi\nexit 2\n"
        )
        git.chmod(0o755)
        for name in ("curl", "tar", "gzip", "sha256sum", "mktemp", "mkdir", "rm", "mv", "date", "uname", "id", "ln"):
            (commands / name).symlink_to(shutil.which(name))

        environment = os.environ.copy()
        environment.pop("NVM_DIR", None)
        environment.update({
            "HOME": str(home),
            "PATH": str(commands),
            "TB_AUTO_REPAIR": "1",
            "TB_NODE_RELEASE_BASE": release.as_uri(),
            "TB_SETUP_DIR": str(root / "checkout"),
        })
        result = subprocess.run(
            ["/bin/bash", str(ROOT / "scripts/first-run.sh")],
            env=environment,
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        assert result.returncode == 0, result.stdout + result.stderr
        assert "Installed v22.1.0" in result.stdout
        assert "npm-ci" in result.stdout
        assert "setup-invoked" in result.stdout
        assert (home / ".local/share/taskbridge/node-22/bin/node").is_file()
        assert (home / ".local/bin/node").is_symlink()

        # A broken archive must be rejected before it is installed.
        archive.write_bytes(archive.read_bytes() + b"tampered")
        shutil.rmtree(home / ".local/share/taskbridge/node-22")
        result = subprocess.run(
            ["/bin/bash", str(ROOT / "scripts/first-run.sh")],
            env=environment,
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        assert result.returncode != 0
        assert "verification failed" in result.stderr
        assert not (home / ".local/share/taskbridge/node-22/bin/node").exists()

    print("first-run Node repair smoke tests passed")


if __name__ == "__main__":
    main()
