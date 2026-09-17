# Published container execution — 2026-09-17

## Tested artifact and results

Tests pulled the actual published image for patch commit
`a54ed858544f3d9fd625c62e09db25914f55b837`, then used its immutable digest:

`ghcr.io/lovitus/siyuan-unlock-sftp@sha256:38e39d9c606f0cbebda39d6fd038406e30974dc4dcf3609e84a502681669dc63`

Execution used Linux AMD64 GitHub runners, the image's original entrypoint,
separate bind-mounted workspaces with the runner's PUID/PGID, and an independent
loopback AsyncSSH SFTP server. No production workspace or server was involved.

| Check | Result | Evidence |
| --- | --- | --- |
| Explicit `serve` command: sync, backup, cleanup, fresh restore, restart | Nine checks passed | [CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35196643145), [report](container-normal.json) |
| 80 ms delay on every SFTP read/write, 16 MiB random binary asset | Ten checks passed, including byte-equal sync and fresh backup restore | [CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35196772155), [report](container-delayed-io.json) |
| Original ENTRYPOINT **and default CMD**, without command override | Failed: duplicate kernel executable argument | [CI](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35196964788), [report](container-default-before-fix.json), [log](container-default-before-fix.log) |

The normal smoke's initial attempt edited a newly synchronized document before
the receiving application's block index was ready. The harness now polls
`getBlockInfo` before editing; that endpoint also supports the application's
normal index recovery. The subsequent successful run used the same unmodified
image. This harness correction does not establish an immediate-edit guarantee.

The delayed-I/O run transferred a 16,777,216-byte incompressible asset. Its two
sync calls took 16.213 s and 22.951 s on this runner. These are individual API
durations, not benchmark averages or estimates for a production repository.
The injected delay is per SFTP read/write request, not measured network RTT.

## Confirmed default-startup defect and fix

The image's default CMD included `/opt/siyuan/kernel`, while `entrypoint.sh`
already prepends that executable. Starting with no command override produced:

```text
Error: unknown command "/opt/siyuan/kernel" for "kernel"
```

Commit `9662df9d6` adds `patches/sftp/docker-command.patch`, changing CMD to
`["serve"]`. `prepare-sftp-source.sh` applies this patch for container builds;
the existing four-platform matrix remains unchanged. No kernel or SFTP provider
code changed in this follow-up.

[Rebuild and automatic verification](https://github.com/lovitus/siyuan-unlock-sftp/actions/runs/35197219454)
was dispatched from that commit. It must finish successfully before the
default-startup fix is considered validated in a newly published image.
Its verification checks both default startup/PUID ownership and the normal and
delayed SFTP lifecycle cases. It publishes an isolated review tag, not a formal
release or `latest` promotion.

## Remaining scope

- Await the rebuilt image's default-startup and lifecycle results.
- Cross-platform clients using the same live SFTP server still need validation;
  no Windows/Android device has been connected in this task.
- The container execution above covers AMD64. Manifest presence and compilation
  for other architectures are not runtime validation of those architectures.
- A 16 MiB asset with injected I/O delay does not cover very large repositories,
  long-running production workloads, mobile suspension, or mixed old/new clients.
- Formal release promotion remains separate from these isolated review builds.

The reproducible workflow is `verify-sftp-container.yml`; it can test an existing
review image without rebuilding it. `build-container-review.yml` first rebuilds
the original four-platform matrix and then runs those checks automatically.
