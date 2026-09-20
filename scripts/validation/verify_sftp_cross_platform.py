"""Sequential native clients using one live SFTP repository across OS hosts."""

import argparse
import importlib.util
import json
from pathlib import Path
import secrets
import shutil
import sys
import time

p = argparse.ArgumentParser()
p.add_argument("phase", choices=["seed", "peer", "verify"])
p.add_argument("--kernel", required=True)
p.add_argument("--resources", required=True)
p.add_argument("--sftp-config", required=True)
p.add_argument("--output", required=True)
p.add_argument("--handoff", required=True)
p.add_argument("--seed-output")
args = p.parse_args()
sys.argv = [
    sys.argv[0],
    "--kernel",
    args.kernel,
    "--resources",
    args.resources,
    "--sftp-config",
    args.sftp_config,
    "--output",
    args.output,
]
spec = importlib.util.spec_from_file_location(
    "native_smoke", Path(__file__).with_name("verify_sftp_artifact.py")
)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
report = m.report
report["phase"] = args.phase
try:
    if args.phase == "verify":
        if not args.seed_output:
            raise ValueError("--seed-output required for verification")
        shutil.copytree(Path(args.seed_output) / "client", m.output / "client")
    d = m.launch("client", resume=args.phase == "verify")
    if args.phase != "verify":
        m.call(d, "/api/setting/getCloudUser")
        m.call(d, "/api/sync/setSyncEnable", {"enabled": False})
        m.call(
            d,
            "/api/repo/initRepoKeyFromPassphrase",
            {"pass": "isolated cross platform test key"},
        )
        m.call(d, "/api/sync/setSyncProviderSFTP", {"sftp": m.sftp})
        m.call(d, "/api/sync/setSyncProvider", {"provider": 5})
        m.call(d, "/api/sync/setSyncMode", {"mode": 2})
    if args.phase == "seed":
        cloud = "cross" + secrets.token_hex(4)
        m.call(d, "/api/sync/createCloudSyncDir", {"name": cloud})
        m.call(d, "/api/sync/setCloudSyncDir", {"name": cloud})
        m.call(d, "/api/sync/setSyncEnable", {"enabled": True})
        book = m.call(
            d,
            "/api/notebook/createNotebook",
            {"name": "Cross platform SFTP validation"},
        )
        book = book["notebook"]["id"] if "notebook" in book else book["id"]
        doc = m.call(
            d,
            "/api/filetree/createDocWithMd",
            {
                "notebook": book,
                "path": "/cross-platform",
                "markdown": "from-macOS-through-SFTP",
            },
        )
        time.sleep(1)
        m.call(d, "/api/sync/performSync")
        handoff = {"cloud": cloud, "notebook": book, "document": doc}
        Path(args.handoff).write_text(json.dumps(handoff), encoding="utf-8")
    else:
        handoff = json.loads(Path(args.handoff).read_text(encoding="utf-8-sig"))
        if args.phase == "peer":
            m.call(d, "/api/sync/setCloudSyncDir", {"name": handoff["cloud"]})
            m.call(d, "/api/sync/setSyncEnable", {"enabled": True})
        m.call(d, "/api/sync/performSync")
        path = d["ws"] / "data" / handoff["notebook"] / (handoff["document"] + ".sy")
        m.waitfor(path.exists, "cross-platform document was not downloaded")
        assert "from-macOS-through-SFTP" in path.read_text(encoding="utf-8")
        if args.phase == "peer":
            m.waitfor(
                lambda: m.call(
                    d,
                    "/api/block/getBlockInfo",
                    {"id": handoff["document"]},
                    expect=None,
                )
                is not None,
                "document not indexed",
            )
            m.call(
                d,
                "/api/block/appendBlock",
                {
                    "parentID": handoff["document"],
                    "dataType": "markdown",
                    "data": "edited-on-Windows-through-SFTP",
                },
            )
            time.sleep(1)
            m.call(d, "/api/sync/performSync")
        assert "edited-on-Windows-through-SFTP" in path.read_text(encoding="utf-8")
        report["document_json"] = json.loads(path.read_text(encoding="utf-8"))
    report.update(status="passed", handoff=handoff)
except BaseException as error:
    report.update(status="failed", error=str(error))
    raise
finally:
    for proc, log in m.processes:
        proc.terminate()
        try:
            proc.wait(timeout=15)
        except Exception:
            proc.kill()
            proc.wait()
        log.close()
    (m.output / "report.json").write_text(
        json.dumps(report, indent=2), encoding="utf-8"
    )
