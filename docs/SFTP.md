# SFTP cloud storage and release CI

This fork adds SFTP as provider `5`, on top of the unlock patches from
`appdev/siyuan-unlock`. The patch is currently validated against SiYuan `v3.8.3`.
The checked-in `app/` and `kernel/` trees are inherited upstream snapshots;
release builds use the official version tag plus `patches/` and `overlays/`.

## SFTP configuration

In Settings → Sync, select **SFTP**, enter the settings, then click **Save**.

| Field | Meaning |
| --- | --- |
| Host | SSH hostname or IP, without a scheme or port |
| Port | SSH port; defaults to 22 |
| Username / Password | SSH password authentication; password whitespace is preserved |
| Path | **Required**, existing absolute remote directory, e.g. `/srv/siyuan` |
| Host key | Server key SHA256 fingerprint, e.g. from `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the server |
| Timeout | Total deadline per SFTP operation, 7–300 seconds; default 30 |
| Concurrent Reqs | 1–16; default 4 |

The account must have read/write access to Path. For a chrooted SFTP account,
use the absolute path visible inside its SFTP session. An empty/whitespace-only
path, a relative path, and `..` traversal are rejected by both save-time backend
validation and provider construction. The provider never silently uses the SSH
home directory or creates the configured root.

Repositories live under `<path>/<cloud-name>/siyuan/repo/`. You can create and
remove cloud repositories through the existing cloud directory UI. Removing a
repository only removes its `siyuan/repo` subtree. Object paths beneath the root
must not contain symlinks. The root itself may resolve through a symlink.

Uploads use temporary files plus rename; OpenSSH's `posix-rename@openssh.com`
extension provides atomic replacement of existing references. Servers without
that extension must support replacement with standard rename, or overwriting
will fail explicitly. Connections close after each operation because the
upstream Cloud interface does not provide a lifecycle/Close hook. This bounds
connection lifetime; high latency servers may benefit from fewer concurrent
requests and a higher timeout. SSH private-key authentication is not implemented.

## Automated GitHub release

`.github/workflows/release-cron.yml` runs every six hours at minute 17, and can
also starts when release workflows or patches change on `master`, and can be
started manually using **Track upstream releases → Run workflow**.
Everything runs in GitHub Actions; no local scheduler or Codex task is required.

1. Query the latest published, stable release of `appdev/siyuan-unlock`.
2. Read the package-manager version from the matching official SiYuan tag.
3. Skip an already published destination release, or create/reuse a draft.
4. Clone that SiYuan tag and apply the unlock patches, SFTP integration patch,
   and SFTP provider overlay. Patch failures stop the build.
5. Run the existing desktop, Android, iOS and container build matrices through
   reusable workflows. Upload the client assets to the draft release.
6. After all four workflows succeed, promote the versioned GHCR image to
   `latest`, then publish the completed release.

Failed builds leave the release in draft and the next scheduled run retries.
The workflow uses a concurrency group to serialize release runs. Future upstream
changes that conflict with a patch require updating the patch in this repository;
CI reports the conflict rather than releasing a build without SFTP support.

The preserved outputs are Linux amd64 tar.gz/AppImage, Linux arm64 tar.gz,
macOS amd64/arm64 DMG, Windows amd64 EXE, Android arm64 original/AppDev APKs,
iOS IPA, and Docker linux/amd64, linux/arm64, linux/arm/v7, linux/arm/v8.
Android keeps the original signing requirements: configure the `KEYSTORE` and
`KEYSTORE_PASSWORD` repository secrets. The full release waits for Android too.

The current repository is the release target automatically (`GITHUB_REPOSITORY`).
The workflow needs `contents: write` and `packages: write`, granted in the YAML.
For a fork, enable GitHub Actions and put the workflow on the default branch.
No `DOCKER_HUB_USER`, `DOCKER_HUB_PWD`, or original-repository marker is needed.
Images publish only to `ghcr.io/lovitus/siyuan-unlock-sftp:vX.Y.Z` and `:latest` (lowercase).
Configure the GHCR package's visibility if public unauthenticated pulls are wanted.

Example for `lovitus/siyuan-unlock-sftp`:

```sh
docker run -d --name siyuan \
  -p 6806:6806 -v /srv/siyuan-workspace:/siyuan/workspace \
  ghcr.io/lovitus/siyuan-unlock-sftp:latest \
  serve --workspace=/siyuan/workspace --accessAuthCode=change-me
```

## Validation

```sh
bash scripts/prepare-sftp-source.sh v3.8.3 /tmp/siyuan-sftp-build
cd /tmp/siyuan-sftp-build/kernel
go test -race ./sftpcloud
go build -tags 'fts5 sqlcipher' .
cd ../app
pnpm install --no-frozen-lockfile
pnpm run typecheck
```

From this repository, run `python3 scripts/test-release-tracking.py` and
`actionlint .github/workflows/{release-cron,release-docker,release-android,release-ios,desktop-release}.yml`.
