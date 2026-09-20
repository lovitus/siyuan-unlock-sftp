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
