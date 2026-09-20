import argparse, asyncio, asyncssh, json, os, secrets
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--directory", required=True)
parser.add_argument(
    "--io-delay-ms",
    type=int,
    default=0,
    help="Artificial delay for each SFTP read/write request",
)
args = parser.parse_args()
if args.io_delay_ms < 0:
    parser.error("--io-delay-ms must be nonnegative")
base = Path(args.directory)
base.mkdir(parents=True, exist_ok=False)
root = base / "remote"
(root / "storage").mkdir(parents=True, exist_ok=True)
key = asyncssh.generate_private_key("ssh-ed25519")
password = secrets.token_urlsafe(32)


class Server(asyncssh.SSHServer):
    def begin_auth(self, username):
        return True

    def password_auth_supported(self):
        return True

    def validate_password(self, username, supplied_password):
        return username == "tester" and secrets.compare_digest(
            supplied_password, password
        )


class DelayedSFTP(asyncssh.SFTPServer):
    async def read(self, file_obj, offset, size):
        await asyncio.sleep(args.io_delay_ms / 1000)
        return super().read(file_obj, offset, size)

    async def write(self, file_obj, offset, data):
        await asyncio.sleep(args.io_delay_ms / 1000)
        return super().write(file_obj, offset, data)


async def main():
    server = await asyncssh.create_server(
        Server,
        "127.0.0.1",
        0,
        server_host_keys=[key],
        sftp_factory=lambda chan: DelayedSFTP(chan, chroot=str(root)),
    )
    config = dict(
        host="127.0.0.1",
        port=server.get_port(),
        username="tester",
        password=password,
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
