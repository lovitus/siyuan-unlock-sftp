"""Check the image's original ENTRYPOINT and CMD without command overrides."""

import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time
import urllib.request

p = argparse.ArgumentParser()
p.add_argument("--image", required=True)
p.add_argument("--report", required=True)
args = p.parse_args()
report_path = Path(args.report)
report_path.parent.mkdir(parents=True, exist_ok=True)
name = "sftp-default-" + secrets.token_hex(4)
report = {"image": args.image, "command_override": False, "status": "failed"}
if os.environ.get("SFTP_REVIEW_BASE_IMAGE"):
    report["base_image"] = os.environ["SFTP_REVIEW_BASE_IMAGE"]
    report["local_derivative_cmd"] = ["serve"]
with tempfile.TemporaryDirectory(prefix="sftp-default-") as directory:
    try:
        subprocess.run(
            [
                "docker",
                "run",
                "-d",
                "--name",
                name,
                "-p",
                "127.0.0.1::6806",
                "-e",
                f"PUID={os.getuid()}",
                "-e",
                f"PGID={os.getgid()}",
                "-v",
                f"{directory}:/siyuan/workspace",
                args.image,
            ],
            check=True,
            stdout=subprocess.DEVNULL,
        )
        address = subprocess.check_output(
            ["docker", "port", name, "6806/tcp"], text=True
        ).strip()
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(
                    "http://" + address + "/api/system/bootProgress", timeout=2
                ) as response:
                    progress = json.load(response)
                if progress.get("data", {}).get("progress", 0) >= 100:
                    break
            except (OSError, ValueError):
                pass
            time.sleep(0.5)
        else:
            raise TimeoutError("Default image command did not finish booting")
        conf = Path(directory) / "conf/conf.json"
        assert conf.is_file(), "Default command did not use the mounted workspace"
        assert (
            conf.stat().st_uid == os.getuid()
        ), "Workspace ownership differs from PUID"
        report.update(
            status="passed",
            checks=[
                "original ENTRYPOINT and CMD boot",
                "mounted workspace used",
                "PUID ownership respected",
            ],
        )
    except BaseException as e:
        report["error"] = str(e)
        raise
    finally:
        with report_path.with_suffix(".log").open("w") as log:
            subprocess.run(
                ["docker", "logs", name],
                stdout=log,
                stderr=subprocess.STDOUT,
                check=False,
            )
        subprocess.run(
            ["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, check=False
        )
        report_path.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
