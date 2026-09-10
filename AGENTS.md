# Project preferences

- The user’s default GitHub owner is `lovitus`. This project publishes to `lovitus/siyuan-unlock-sftp`.
- Track stable releases of `appdev/siyuan-unlock` through scheduled GitHub Actions. Do not create local scheduled tasks.
- Require an explicit SFTP path. Publish container images only to GHCR; preserve the upstream build matrices.
- Source changes for release builds belong in `patches/sftp/` and `overlays/`; builds clone the official version tag via `scripts/prepare-sftp-source.sh`.
