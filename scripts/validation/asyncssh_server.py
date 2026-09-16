import argparse, asyncio, asyncssh, json, os
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--directory", required=True)
args = parser.parse_args()
base = Path(args.directory)
base.mkdir(parents=True, exist_ok=False)
root = base / "remote"
(root / "storage").mkdir(parents=True, exist_ok=True)
key = asyncssh.generate_private_key("ssh-ed25519")


class Server(asyncssh.SSHServer):
    def begin_auth(self, username):
        return True

    def password_auth_supported(self):
        return True

    def validate_password(self, username, password):
        return username == "tester" and password == "isolated-sftp-validation"


async def main():
    server = await asyncssh.create_server(
        Server,
        "127.0.0.1",
        0,
        server_host_keys=[key],
        sftp_factory=lambda chan: asyncssh.SFTPServer(chan, chroot=str(root)),
    )
    config = dict(
        host="127.0.0.1",
        port=server.get_port(),
        username="tester",
        password="isolated-sftp-validation",
        path="/storage",
        hostKey=key.get_fingerprint("sha256"),
        timeout=30,
        concurrentReqs=4,
    )
    p = base / "sftp.json"
    p.write_text(json.dumps(config))
    os.chmod(p, 0o600)
    await asyncio.Event().wait()


asyncio.run(main())
