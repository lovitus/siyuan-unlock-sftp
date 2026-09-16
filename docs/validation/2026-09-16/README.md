# SFTP review and artifact validation — 2026-09-16

## Exact source

- Official source tag: `v3.8.3`.
- Patch/build commit: `a54ed858544f3d9fd625c62e09db25914f55b837`.
- [Review build run](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35079166149).
- [SFTP patch integration CI: passed](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35079165299).
- Downloaded client artifacts contain matching `source-revision.txt` files.
- Review packages are Actions artifacts; existing release assets were not replaced.

## Build status snapshot

At 2026-09-16 09:49 UTC, all six desktop jobs, Android, and iOS succeeded.
The Docker job was still building the preserved four-platform matrix
(`linux/amd64`, `linux/arm64`, `linux/arm/v7`, `linux/arm/v8`). Its expected tag is
`ghcr.io/lovitus/siyuan-unlock-sftp:review-a54ed858544f3d9fd625c62e09db25914f55b837`.
The image was not yet published or runtime-validated at this snapshot; do not
interpret the client results below as a successful container build. The linked
Actions run is authoritative for later status.

## Review fixes included in this build

1. Serialize lock acquisition, refresh, release, and final repository mutations
   with an exclusive SFTP directory. Concurrent clients that read an absent lock
   can no longer both become its owner.
2. Require a random ownership token. A stale client cannot overwrite or remove
   repository objects after another client takes over, including takeover after
   upload staging but before publication. Shared device IDs do not bypass this.
3. Hold and refresh a lease for the entire tagged backup upload, so another
   backup or purge cannot interleave between its object writes.
4. Reject malformed lock contents before DejaVu's unchecked type assertions;
   leave the original lock untouched instead of panicking or deleting it.
5. Keep the SFTP save and cloud-cleanup controls available together. Use Bash
   consistently for desktop workflow commands, including Windows runners.

Regression coverage includes deterministic old-fail/new-pass lock races, stale
owner writes/deletes, independent-process concurrent sync, conflicting edits
with preserved history, truncated TCP uploads, restart/retry, and backup/purge
exclusion. The restart test simulates lease expiration by aging the test lock.
Local `go test -race ./sftpcloud -count=1`, a full kernel build, patch application
checks, workflow lint, and release-detection tests passed before dispatch.

## Native package execution

The downloaded macOS ARM64 DMG passed `hdiutil` checksum verification and was
mounted read-only. Its packaged kernel was executed directly, with the packaged
resources, against a separate AsyncSSH server using password authentication and
a pinned host-key fingerprint. The server listened only on loopback and used a
new disposable root with explicit `/storage` path.

Three separate native kernel processes used fresh workspaces on **one Mac**.
This is application-kernel execution over a real SSH/SFTP connection. It is not
testing three physical devices or driving the Electron UI.

All eight assertions passed, including a repeat using the committed validator:

1. Reject an empty SFTP path through the application API.
2. Create on A, sync through SFTP, and read the document on B.
3. Edit on B and sync the change back to A.
4. Compare the synchronized document JSON for equality.
5. Upload and download a tagged backup.
6. Delete on A and verify deletion propagates to B.
7. Download the retained tagged backup after cloud cleanup.
8. Download and restore that backup into fresh workspace C, verifying both the
   original content and B's edit.

The validator initializes the local unlock account via `getCloudUser`, matching
the normal UI setup. An early harness attempt omitted this prerequisite and
produced a no-op sync response; it was corrected before the passing runs.
Assertions inspect workspace documents, not only successful HTTP responses.

See [native execution evidence](macos-native.json) and the reproducible commands
in [SFTP documentation](../../SFTP.md#review-builds).

| Artifact | SHA-256 |
| --- | --- |
| macOS ARM64 DMG | `0b1211fb9b7706c9ef877dac3d8d52c02947bebd9ef436d94cdab27549a8707c` |
| Packaged macOS ARM64 kernel | `27eb0d6c930695c5261f412ce9f11cead2b3d33799435a8eab9770a2f1450fdf` |

## Other package checks

- Android: both APKs passed ZIP integrity checks and contain the SFTP ownership
  protection code in `libgojni.so`. CI checked both application IDs.
  [Evidence and hashes](android-packages.json).
- iOS: IPA passed ZIP integrity checks and its application executable contains
  the SFTP ownership protection code. [Evidence and hash](ios-package.json).
- Linux AMD64: downloaded archive contains an x86-64 ELF kernel with the same
  protection code. [Evidence and hash](linux-package.json).
- Packaged app, desktop, mobile, and export bundles contain the SFTP save API and
  cleanup control strings. This is a static bundle check, not a UI interaction
  test. [Evidence](ui-bundles.json).

Android, iOS, Windows, Linux, and macOS Intel were not executed on physical
devices in this validation. Large repositories, high-latency networks, and
mixed-version clients were not performance/compatibility tested.

All clients sharing a repository must upgrade for the new lock protocol to
protect their writes. An abandoned `.sftp-lock-guard` deliberately blocks
mutations; recovery requires stopping all clients before inspecting/removing
that empty directory. A restarted process may need to wait for the 65-second
lease to expire. See the operational details in [SFTP documentation](../../SFTP.md).
