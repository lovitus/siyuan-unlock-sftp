"""Only deterministic patch incompatibilities suppress identical future attempts."""
import os
from pathlib import Path
import subprocess
import sys

from release_state import ROOT, clear_failure, record_failure


def main():
    repo, tag = os.environ['GITHUB_REPOSITORY'], os.environ['VERSION']
    for mode in ('desktop', 'docker'):
        result = subprocess.run([
            'bash', str(ROOT / 'scripts/prepare-sftp-source.sh'), tag,
            str(Path(os.environ['RUNNER_TEMP']) / f'sftp-{mode}'), mode,
        ])
        if result.returncode:
            if result.returncode == 42:
                record_failure(repo, tag, os.environ['PATCH_FINGERPRINT'],
                               f'https://github.com/{repo}/actions/runs/{os.environ["GITHUB_RUN_ID"]}',
                               os.environ['EXPECTED_SOURCE_SHA'])
            # Clone/network/toolchain failures remain retryable, not cached conflicts.
            return result.returncode
    clear_failure(repo, tag)
    return 0


if __name__ == '__main__':
    sys.exit(main())
