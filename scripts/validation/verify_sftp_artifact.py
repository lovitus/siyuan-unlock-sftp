# Run against an extracted native CI package and a disposable SFTP server.
# Uses fresh workspaces only; never pass a real user workspace as --output.
import argparse, hashlib, json, os, secrets, socket, subprocess, time, urllib.request
from pathlib import Path

# The normal UI initializes the local unlock account with getCloudUser.
# Reproduce that setup before testing API-driven synchronization.
p = argparse.ArgumentParser()
mode = p.add_mutually_exclusive_group(required=True)
mode.add_argument("--kernel")
mode.add_argument(
    "--image",
    help="Run the published image through its original entrypoint (Linux Docker host)",
)
p.add_argument("--resources")
p.add_argument("--sftp-config", required=True)
p.add_argument("--output", required=True)
p.add_argument("--asset-mib", type=int, default=0)
args = p.parse_args()
if args.asset_mib < 0 or args.asset_mib > 256:
    p.error("--asset-mib must be between 0 and 256")
if args.kernel and not args.resources:
    p.error("--resources is required with --kernel")
output = Path(args.output).resolve()
output.mkdir(parents=True, exist_ok=False)
sftp = json.loads(Path(args.sftp_config).read_text(encoding="utf-8"))
processes = []
containers = []
events = []
report = {
    "runtime": "docker" if args.image else "native",
    "sftp_server": "AsyncSSH standalone, localhost, password authentication",
    "events": events,
}

if args.image:
    if os.environ.get("SFTP_REVIEW_BASE_IMAGE"):
        report["base_image"] = os.environ["SFTP_REVIEW_BASE_IMAGE"]
        report["local_derivative_cmd"] = ["serve"]
    report["image"] = json.loads(
        subprocess.check_output(["docker", "image", "inspect", args.image])
    )[0]
    report["image"] = {
        k: report["image"][k] for k in ("Id", "RepoDigests", "Architecture", "Os")
    }
else:
    report.update(
        kernel=str(Path(args.kernel).resolve()),
        kernel_sha256=hashlib.sha256(Path(args.kernel).read_bytes()).hexdigest(),
    )


def call(d, endpoint, data=None, expect=0):
    req = urllib.request.Request(
        d["url"] + endpoint,
        data=json.dumps(data or {}).encode(),
        headers={
            "Content-Type": "application/json",
            "Authorization": "Token " + d["token"],
        },
    )
    started = time.monotonic()
    with urllib.request.urlopen(req, timeout=300) as response:
        result = json.load(response)
    events.append(
        {
            "device": d["name"],
            "endpoint": endpoint,
            "code": result.get("code"),
            "seconds": round(time.monotonic() - started, 3),
        }
    )
    print(d["name"], endpoint, result.get("code"), flush=True)
    if expect is not None and result.get("code") != expect:
        raise RuntimeError(endpoint + ": " + str(result))
    return result.get("data")


def launch(name):
    ws = output / name
    ws.mkdir()
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    log = (output / (name + ".log")).open("wb")
    kernel_args = [
        "serve",
        "--wd",
        "/opt/siyuan" if args.image else args.resources,
        "--port",
        str(port),
        "--accessAuthCode",
        secrets.token_hex(16),
        "--lang",
        "en_US",
    ]
    container = None
    if args.image:
        container = "sftp-review-" + name + "-" + secrets.token_hex(4)
        containers.append(container)
        command = [
            "docker",
            "run",
            "--name",
            container,
            "--network",
            "host",
            "-e",
            f"PUID={os.getuid()}",
            "-e",
            f"PGID={os.getgid()}",
            "-v",
            f"{ws}:/siyuan/workspace",
            args.image,
        ] + kernel_args
    else:
        command = [args.kernel] + kernel_args + ["--workspace", str(ws)]
    proc = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
    processes.append((proc, log))
    d = {"name": name, "ws": ws, "url": f"http://127.0.0.1:{port}", "token": ""}
    d["container"] = container
    ready(d, proc)
    return d


def ready(d, proc=None):
    ws, name = d["ws"], d["name"]
    deadline = time.monotonic() + 90
    last_error = "no boot response"
    while time.monotonic() < deadline:
        if proc is not None and proc.poll() is not None:
            raise RuntimeError(
                name + " exited; inspect " + str(output / (name + ".log"))
            )
        try:
            conf = json.loads((ws / "conf/conf.json").read_text(encoding="utf-8"))
            d["token"] = conf["api"]["token"]
            with urllib.request.urlopen(
                d["url"] + "/api/system/bootProgress", timeout=2
            ) as r:
                progress = json.load(r)
            if progress.get("data", {}).get("progress", 0) >= 100:
                return d
        except (OSError, ValueError, KeyError) as e:
            last_error = repr(e)
        time.sleep(0.3)
    raise TimeoutError("kernel boot: " + name + "; last probe: " + last_error)


def waitfor(fn, label):
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        if fn():
            return
        time.sleep(0.3)
    raise AssertionError(label)


try:
    a, b = launch("a"), launch("b")
    cloud = "review" + secrets.token_hex(3)
    for d in (a, b):
        call(d, "/api/setting/getCloudUser")
        call(d, "/api/sync/setSyncEnable", {"enabled": False})
        call(
            d,
            "/api/repo/initRepoKeyFromPassphrase",
            {"pass": "isolated artifact validation passphrase"},
        )
        call(
            d,
            "/api/sync/setSyncProviderSFTP",
            {"sftp": dict(sftp, path="")},
            expect=None,
        )
        assert events[-1]["code"] != 0, "empty SFTP path was accepted"
        call(d, "/api/sync/setSyncProviderSFTP", {"sftp": sftp})
        config = call(d, "/api/system/getConf")["conf"]["sync"]["sftp"]
        for field in ("host", "port", "username", "path", "hostKey"):
            assert config[field] == sftp[field], (
                "saved SFTP config missing from UI response: " + field
            )
        call(d, "/api/sync/setSyncProvider", {"provider": 5})
        call(d, "/api/sync/setSyncMode", {"mode": 2})
    call(a, "/api/sync/createCloudSyncDir", {"name": cloud})
    for d in (a, b):
        call(d, "/api/sync/setCloudSyncDir", {"name": cloud})
        call(d, "/api/sync/setSyncEnable", {"enabled": True})
    notebook = call(
        a, "/api/notebook/createNotebook", {"name": "SFTP artifact validation"}
    )
    notebook = notebook["notebook"]["id"] if "notebook" in notebook else notebook["id"]
    doc = call(
        a,
        "/api/filetree/createDocWithMd",
        {
            "notebook": notebook,
            "path": "/SFTP verification",
            "markdown": "artifact-original-A",
        },
    )
    time.sleep(1)
    call(a, "/api/sync/performSync")
    call(b, "/api/sync/performSync")
    docpath = Path("data") / notebook / (doc + ".sy")
    waitfor(lambda: (b["ws"] / docpath).exists(), "A document missing on B")
    assert "artifact-original-A" in (b["ws"] / docpath).read_text(encoding="utf-8")
    # Sync writes files before its asynchronous application index is ready.
    waitfor(
        lambda: call(b, "/api/block/getBlockInfo", {"id": doc}, expect=None)
        is not None,
        "synced document never became available in B's block index",
    )
    call(
        b,
        "/api/block/appendBlock",
        {"parentID": doc, "dataType": "markdown", "data": "artifact-update-B"},
    )
    time.sleep(1)
    call(b, "/api/sync/performSync")
    call(a, "/api/sync/performSync")
    waitfor(
        lambda: "artifact-update-B" in (a["ws"] / docpath).read_text(encoding="utf-8"),
        "B edit missing on A",
    )
    assert json.loads((a["ws"] / docpath).read_text(encoding="utf-8")) == json.loads(
        (b["ws"] / docpath).read_text(encoding="utf-8")
    )
    if args.asset_mib:
        asset = Path("data/assets/sftp-validation.bin")
        payload = os.urandom(args.asset_mib * 1024 * 1024)
        (a["ws"] / asset).parent.mkdir(parents=True, exist_ok=True)
        (a["ws"] / asset).write_bytes(payload)
        call(a, "/api/sync/performSync")
        call(b, "/api/sync/performSync")
        assert (b["ws"] / asset).read_bytes() == payload, "binary asset differs on B"
        report["asset"] = {
            "bytes": len(payload),
            "sha256": hashlib.sha256(payload).hexdigest(),
        }
    call(a, "/api/repo/createSnapshot", {"memo": "artifact SFTP backup"})
    snapshots = call(a, "/api/repo/getRepoSnapshots", {"page": 1})
    snapshot = snapshots["snapshots"][0]["id"]
    call(a, "/api/repo/tagSnapshot", {"id": snapshot, "name": "artifact-check"})
    call(a, "/api/repo/uploadCloudSnapshot", {"id": snapshot, "tag": "artifact-check"})
    call(
        b, "/api/repo/downloadCloudSnapshot", {"id": snapshot, "tag": "artifact-check"}
    )
    call(
        a, "/api/filetree/removeDoc", {"notebook": notebook, "path": "/" + doc + ".sy"}
    )
    call(a, "/api/sync/performSync")
    call(b, "/api/sync/performSync")
    waitfor(lambda: not (b["ws"] / docpath).exists(), "deletion missing on B")
    call(a, "/api/repo/purgeCloudRepo")
    call(
        b, "/api/repo/downloadCloudSnapshot", {"id": snapshot, "tag": "artifact-check"}
    )
    c = launch("c")
    call(c, "/api/setting/getCloudUser")
    call(c, "/api/sync/setSyncEnable", {"enabled": False})
    call(
        c,
        "/api/repo/initRepoKeyFromPassphrase",
        {"pass": "isolated artifact validation passphrase"},
    )
    call(c, "/api/sync/setSyncProviderSFTP", {"sftp": sftp})
    call(c, "/api/sync/setSyncProvider", {"provider": 5})
    call(c, "/api/sync/setCloudSyncDir", {"name": cloud})
    assert not (c["ws"] / docpath).exists()
    call(
        c, "/api/repo/downloadCloudSnapshot", {"id": snapshot, "tag": "artifact-check"}
    )
    call(c, "/api/repo/checkoutRepo", {"id": snapshot})
    waitfor(
        lambda: (c["ws"] / docpath).exists(), "empty workspace restore missing document"
    )
    restored = (c["ws"] / docpath).read_text(encoding="utf-8")
    assert "artifact-original-A" in restored and "artifact-update-B" in restored
    if args.asset_mib:
        assert (
            c["ws"] / asset
        ).read_bytes() == payload, "binary asset differs after fresh restore"
    if args.image:
        before = (c["ws"] / docpath).read_bytes()
        subprocess.run(["docker", "restart", c["container"]], check=True, timeout=60)
        ready(c)
        assert (
            c["ws"] / docpath
        ).read_bytes() == before, "workspace changed after container restart"
        call(
            c,
            "/api/repo/downloadCloudSnapshot",
            {"id": snapshot, "tag": "artifact-check"},
        )
    report.update(
        status="passed",
        cloud=cloud,
        document=doc,
        snapshot=snapshot,
        checks=[
            "reject empty SFTP path",
            "saved SFTP settings returned by application configuration API",
            "A to B document sync",
            "B to A edit sync",
            "equal document JSON",
            "tag backup upload/download",
            "deletion propagation",
            "tag download after cloud purge",
            "restore into fresh third workspace after purge",
        ],
    )
    if args.asset_mib:
        report["checks"].append(
            "random binary asset synchronized and restored byte-for-byte"
        )
    if args.image:
        report["checks"].append(
            "container restart preserves restored document and SFTP access"
        )
except BaseException as e:
    report.update(status="failed", error=str(e))
    raise
finally:
    for container in containers:
        subprocess.run(
            ["docker", "rm", "-f", container], check=False, stdout=subprocess.DEVNULL
        )
    for proc, log in processes:
        proc.terminate()
    for proc, log in processes:
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        log.close()
    (output / "report.json").write_text(json.dumps(report, indent=2))
    print(json.dumps({k: v for k, v in report.items() if k != "events"}, indent=2))
