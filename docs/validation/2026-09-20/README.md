# v3.8.4 compatibility migration

Patch source: `08f08b655`. The version-specific source changes are in
`patches/sftp/source-v3.8.4.patch`; provider implementation/tests remain shared
in `overlays/kernel/sftpcloud`. The v3.8.3 preparation path is preserved.

The migration adapts the local account initialization to the new typed API
handler while retaining its administrator check and synchronized user setter.
The SFTP configuration field is included in the new typed application-config
response, so saving settings and reopening settings both work. Tagged backup
upload captures the SFTP instance through the new cloud-repository validation
wrapper and retains its full-operation lease. Source defaults no longer patch
the deleted account configuration file.

## Passed locally

- `go test -race ./sftpcloud -count=1`: passed in 27.480 seconds.
- Complete native kernel build with `fts5 sqlcipher`: passed.
- Fresh official-tag source preparation for Docker, including default-command
  patch and applied-source whitespace verification: passed.
- Nine application assertions against a standalone loopback AsyncSSH server:
  empty-path rejection, configuration API round-trip, A→B sync, B→A edits,
  equal document JSON, tagged backup transfer, deletion, backup download after
  cleanup, and restore into fresh workspace C: passed.

[Native execution evidence](v3.8.4-native.json) comes from a locally compiled
kernel with official source resources, not an installed/downloaded CI package.
It uses separate processes on one Mac and does not establish physical-device
or mixed-platform interoperability.

## Still required

[Full v3.8.4 review build](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35500058703)
is running. Clients have passed the formerly failing source preparation step;
final platform build results and downloaded artifact runtime validation remain
pending. [Provider CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35500051667)
now tests both v3.8.3 and v3.8.4.

Both provider CI matrix entries have since completed successfully. Commit
`fd5a60a3f` adds post-build runtime verification for exact review artifacts:
macOS downloads and mounts the ARM64 DMG read-only, Windows extracts the native
kernel from the installer, and Linux pulls the review image by revision then
pins its digest. The successful full review run triggers these checks; container
verification also checks its specific build job. Reports include hashes and
source provenance. These checks have been configured but have not yet run for
the in-progress v3.8.4 build, so no artifact runtime pass is claimed here.

The automatic-release supported-version list still contains only v3.8.3.
Do not add v3.8.4 until review builds and artifact verification pass. This keeps
the fixed scheduled check from restarting an unvalidated release matrix.

## Downloaded macOS ARM64 artifact: passed

The actual v3.8.4 DMG from review build `35500058703` has now been downloaded,
its `source-revision.txt` matched to the full `08f08b655` commit, and its image
checksums verified by `hdiutil` before mounting read-only. Its packaged kernel
passed all nine assertions on both a GitHub macOS runner and the local Mac.

- [Successful native artifact CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35500460807).
- [Local run of the downloaded packaged kernel](v3.8.4-dmg-native.json).
- [DMG SHA-256 and provenance](v3.8.4-dmg-sha256.json).

The local read-only mount has been detached after testing. Android and Windows
artifacts have also appeared; Windows runtime verification was dispatched as
[run 35500596314](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35500596314).
The full matrix and container runtime results are still pending; the supported
automatic-release list remains unchanged.

## Downloaded Windows artifact: passed

Run `35500596314` completed successfully: the extracted v3.8.4 Windows installer
kernel passed all nine lifecycle/configuration assertions on Windows Server
2025. [Runtime report](v3.8.4-windows-native.json). The full matrix currently has
successful Android, Windows, macOS Intel and macOS ARM64 builds; Linux packaging,
iOS and Docker are still running without a reported failure at this snapshot.
Independent macOS and Windows runs do not prove live mixed-platform convergence.

## Linux native runtime and Android package checks

[Linux runtime CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35500809204)
downloaded the exact AMD64 desktop archive, checked its provenance, and executed
the packaged kernel on Ubuntu 24.04. All nine configuration/sync/backup/restore
assertions passed against the independent SFTP server.
[Native report](v3.8.4-linux-native.json).

Both Android ARM64 APK variants passed ZIP integrity checks, and their native
libraries contain the SFTP ownership-protection code. The downloaded Linux
archive's kernel is x86-64 ELF and has the same marker. These package inspections
and hashes are in [package evidence](v3.8.4-android-linux-packages.json).
Android runtime behavior has not been tested.

All six desktop build jobs and Android have now succeeded. iOS and the
four-platform container build remain in progress; no failure is reported in
the latest snapshot. Automatic release eligibility is still withheld.

## Live macOS → Windows → macOS SFTP round trip: passed

The downloaded macOS ARM64 packaged kernel created and uploaded a document.
The downloaded Windows installer kernel, running on Windows Server 2025,
downloaded it from the same live isolated SFTP server, appended a paragraph,
and uploaded the change. The Mac then downloaded the Windows change. Both
markers were present and the final document JSON matched exactly on both ends.

- [Windows peer run](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35501168826).
- [All three phase reports and equality assertion](v3.8.4-cross-platform.json).

The temporary relay forwarded SSH to an isolated test server with a random
password and a pinned host key. This establishes sequential mixed-platform
round-trip behavior for the tested packaged kernels, not simultaneous editing,
GUI interaction, or mobile runtime behavior. iOS and Docker build completion
and container runtime verification remain pending.
