# SFTP cloud storage and release CI

This fork adds SFTP as provider `5`, on top of the unlock patches from
`appdev/siyuan-unlock`. Historical patches cover `v3.8.3` and `v3.8.4`; shared structural integration covers `v3.8.5` onward.
This repository stores only patches, provider overlays, build/validation scripts,
workflows and documentation. Release builds fetch the official version tag and
apply `patches/` and `overlays/`; upstream source and dependencies are not vendored.

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
repository removes its `siyuan/repo` subtree and SFTP staging directory, while
leaving unrelated files in the configured root untouched. Object paths beneath
the root must not contain symlinks. The root itself may resolve through a symlink.

Uploads stage temporary files under `<path>/<cloud-name>/siyuan/.sftp-tmp/`,
outside the `repo/` object and reference namespace, then rename them into place.
The staging directory and repository must reside on the same filesystem for
atomic rename. Interrupted uploads can leave staging files, but these are never
interpreted as snapshots or objects. Successful uploads remove their staging
file. No tag names are reserved based on the `.sftp-` substring.

Older builds wrote temporary files beside their destination. Such legacy files
cannot safely be distinguished from legitimate tags by name alone; this version
does not automatically hide or delete them. If a legacy interrupted upload
causes an invalid-reference error, inspect its content and actual snapshot
references before removing it. Existing files are never guessed to be disposable.

Uploads use temporary files plus rename; OpenSSH's `posix-rename@openssh.com`
extension provides atomic replacement of existing references. Servers without
that extension must support replacement with standard rename, or overwriting
will fail explicitly. Connections close after each operation because the
upstream Cloud interface does not provide a lifecycle/Close hook. This bounds
connection lifetime; high latency servers may benefit from fewer concurrent
requests and a higher timeout. SSH private-key authentication is not implemented.

## Concurrent synchronization and recovery

SFTP lock acquisition rechecks `lock-sync` while holding an exclusive server-side
directory, `<path>/<cloud-name>/siyuan/.sftp-lock-guard`. The same guard serializes
refresh, release, object publication/deletion, and repository creation/removal.
Each provider instance stamps its lock with a random owner token. Ownership is
checked under the guard immediately before publishing a staged upload or deleting
an object, so an instance that lost its lock cannot overwrite or delete the new
owner's data. Upload staging remains parallel; the final checks and mutations
are serialized. This adds SFTP round trips and has not yet been benchmarked on
high-latency servers or large repositories.
This closes the upstream read-missing-then-overwrite window that allowed two
independent processes to enter sync together and lose an offline addition.
A competing sync may fail to obtain the lock; retry after the other sync finishes.

Upgrade **every client** accessing a shared SFTP repository before relying on
this protection. Older builds do not honor the guard. This is a source fix, not
a claim that previously published installation packages contain it.

The guard is removed after each protected operation, not held for the whole sync
or the whole upload. If a process crashes or loses its connection while holding it, a guard
may remain. Subsequent lock operations fail closed. A failed guard removal is
not retried on a fresh connection: the server might already have removed it and
another client might now own a new guard at the same path.

To recover a persistent guard error, stop all clients using this cloud repository
and ensure no sync remains active. Inspect the exact repository's guard directory
and remove it only if it is empty, using `rmdir` (never recursive deletion).
Then restart synchronization. Do not remove a guard while another client could
still be operating. Lock leases retain the 65-second expiry, but a new instance
cannot bypass a live lock just because the device IDs match. This also protects
administrative operations whose upstream IDs are shared strings such as `purge`.
A restarted client may need to wait for the old lock's last refresh to expire
before retrying. Once a new owner takes over, the old token cannot publish,
delete, refresh, or release, even if the old process resumes. Clock differences
can still cause premature or delayed takeover and failed sync attempts; keep
client clocks synchronized.

Remote lock JSON is size-limited and type-checked before passing it to DejaVu.
Malformed locks cause synchronization to fail instead of panicking or silently
removing unknown lock data. Inspect/repair them only after stopping all clients.

## Automated GitHub release

`.github/workflows/release-cron.yml` runs every six hours at minute 17, and can
also start when release workflows or patches change on `master`, and can be
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

Failed builds leave the release in draft. Unchanged failed build inputs are deduplicated across the entire workflow; manual dispatch/rerun can retry transient failures.
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
Images publish only to GHCR, with release tags `ghcr.io/lovitus/siyuan-unlock-sftp:vX.Y.Z` and `:latest` (lowercase).
Configure the GHCR package's visibility if public unauthenticated pulls are wanted.

Docker builds run separately for all four platforms; amd64 and arm64 use native
runners, and QEMU is limited to ARM32. If the official source provides its ONNX
artifact helper, CI uses that helper to match and validate the official runtime
by build-input hash, then passes its immutable digest to Docker. Only a missing
runtime triggers a build in GHCR, where it is reusable by later releases. No
release-version list or copied Dockerfile is needed. Per-platform application
caches persist between runs. The version tag is assembled only after all four
platforms succeed; `latest` still waits for the complete release workflow.

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

The **Test SFTP patch** workflow runs independently on patch changes and pull
requests, even when the matching release already exists. It applies patches to
the regression versions `v3.8.3` through `v3.8.7` and tests the real DejaVu sync → tagged backup →
purge → restore lifecycle with an empty destination repository. It does not
rebuild or replace published release assets.

`TestTwoDeviceSync` additionally exercises normal bidirectional sync using two
different device IDs, separate local repositories, and a shared SSH/SFTP test
server. It checks initial download, reverse-direction edits, independent offline
additions, and deletion propagation. Both workspaces contain initial data because
DejaVu does not create an index for an empty data directory. This is a provider
integration test, not a test of installed desktop/mobile release applications.
`TestConcurrentLockExclusion` deterministically makes two independent processes
observe an absent lock before either writes it, and requires exactly one sync to
succeed. It fails with the old unconditional lock overwrite behavior.
`TestIndependentProcessSync` checks concurrent offline additions, lock-contention
retries, convergence, and a TCP-interrupted upload followed by client restart,
lease expiry, retry, and byte-for-byte download verification on the other device.
Lease expiry in this test is simulated by aging the isolated test server's lock
timestamp, rather than sleeping for 65 seconds.
`TestInterruptedUploadRecovery` separately verifies that a cut TCP connection
cannot replace an existing complete object with a partial upload; a retry succeeds
and partial staging files stay outside object enumeration.
`TestExpiredOwnerCannotMutateRepository` and `TestTakeoverBeforePublish` verify
that a replaced owner cannot mutate the repository, including when takeover
happens after upload staging and before rename. Administrative operations also
check the target repository's lock when it differs from the selected repository.
`TestIndependentProcessConflictHistory` checks concurrent offline edits to the
same file, convergence, a reported conflict, and preservation of the losing edit
in history. `TestMalformedRemoteLockDoesNotPanic` exercises invalid JSON lock
shapes through real DejaVu Sync calls and requires the original lock to remain.
Tagged backup uploads now hold a refreshed lease across the complete operation
through the model integration and `SFTP.WithLease`. The lifecycle test uses this
lease, and `TestBackupLeaseExcludesOtherOperations` verifies that another backup
or purge cannot acquire the lock between object uploads, including failure cleanup.
Large repositories, high-latency servers, mixed old/new clients, and installed
mobile applications still need separate validation.


## Review builds

### Scheduled release patch checks

Every six hours, the workflow checks the latest stable `appdev/siyuan-unlock`
release. Every unpublished version automatically attempts patch preparation;
there is no version allowlist. Historical patch sets remain for `v3.8.3` and
`v3.8.4`. Newer versions share the code under `patches/sftp/transforms/`
and `overlays/`, with no per-version copies.

Go and TypeScript AST transforms locate the provider selector, settings mount,
configuration fields and repository hooks by structure. SFTP form persistence,
API handlers, configuration and provider implementation live in independent
files. Existing upstream S3/WebDAV rendering and API authorization remain intact.
The SFTP configuration endpoint binds its typed request with Gin directly, without
depending on upstream's internal JSON argument helpers.
The config adapter selects the pointer or immutable `ConfSection` interface by
its actual type. Dependency additions match module names and preserve upstream
versions. Missing or ambiguous hooks stop preparation with exit 42.

Source preparation also detects the upstream DejaVu snapshot deadlock where
`DownloadTagIndex` calls `AddTag` while both acquire the same repository mutex.
Only affected code shapes receive a local copy of the release's selected module
under `kernel/sftp-deps/dejavu`: both public operations retain their locks and
share an internal tag-writing helper. The global module cache stays untouched.
Older implementations and upstream repairs need no replacement. Container builds
copy this local dependency before downloading Go modules.

All workflows install the host Go/Node tools and a locked TypeScript parser.
Desktop/container preparation, real upstream settings regression checks and a
SFTP provider tests and a Linux kernel compile with `fts5 sqlcipher` must pass before platform builds start.

Before starting builds, the check saves the input fingerprint and workflow run ID
in the unpublished release notes. The next schedule reads that run once and records
any incomplete result, including packaging, upload, publication, cancellation and
timeout failures. This works even when cancellation prevents a finalizer from running.
Known preflight compatibility failures are recorded immediately. Later checks use
the saved failure without polling the old run or rebuilding identical inputs.
The fingerprint covers the upstream version/commit, the resolved iOS commit and
local build inputs, excluding installed dependencies and caches. The iOS checkout
uses that exact commit instead of a moving branch. New versions, source changes,
patch/build configuration changes, **Run workflow** and **Re-run jobs** retry.
Unchanged failed inputs skip builds with a **build blocked** summary and the original
failure link. Transient failures can be retried manually; they do not retry forever.
Failure to save the attempt prevents the build from starting.
Preflight failures use a separate fingerprint without the iOS commit, because
desktop/container preparation and kernel validation do not consume iOS sources.
Unrelated iOS branch updates therefore cannot unblock a known preflight failure.
SFTP test panics and test timeouts are recorded immediately with this preflight
fingerprint, just like compiler and assertion failures.

Drafts carry a `sftp-release:<version>` marker. Legacy `untagged-*` drafts require
both the exact generated title and upstream release URL. A deterministic canonical
draft is reused by ID, and all matching drafts contribute known-failure evidence.
Every metadata update explicitly includes the real tag. Platform jobs only upload
to the existing draft; the final job publishes that same ID after every build
succeeds. Existing duplicate drafts and their assets are preserved.
