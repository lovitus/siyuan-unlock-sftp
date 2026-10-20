"""Only deterministic patch incompatibilities suppress identical future attempts."""
import os
import re
from pathlib import Path
import subprocess
import sys

from release_state import ROOT, clear_failure, record_failure


def compile_kernel():
    source = Path(os.environ['RUNNER_TEMP']) / 'sftp-desktop'
    commands = [['go', 'test', '-count=1', '-timeout', '120s', './sftpcloud'],
                ['go', 'build', '-tags', 'fts5 sqlcipher', '-o',
                 str(source.parent / 'sftp-kernel-preflight'), '.']]
    for command in commands:
        result = subprocess.run(command, cwd=source / 'kernel', stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True)
        print(result.stdout, end='')
        if result.returncode: break
    if result.returncode:
        # Test failures and compiler diagnostics are compatibility failures. Download,
        # runner and toolchain failures stay retryable on the next schedule.
        if re.search(r'(?m)^.+\.go:\d+:\d+:|^--- FAIL:|updates to go.mod needed', result.stdout):
            repo = os.environ['GITHUB_REPOSITORY']
            record_failure(repo, os.environ['VERSION'], os.environ['PATCH_FINGERPRINT'],
                           f'https://github.com/{repo}/actions/runs/{os.environ["GITHUB_RUN_ID"]}',
                           os.environ['EXPECTED_SOURCE_SHA'])
    else:
        clear_failure(os.environ['GITHUB_REPOSITORY'], os.environ['VERSION'])
    return result.returncode


def main():
    repo, tag = os.environ['GITHUB_REPOSITORY'], os.environ['VERSION']
    for mode in ('desktop', 'docker'):
        result = subprocess.run([
            'bash', str(ROOT / 'scripts/prepare-sftp-source.sh'), tag,
            str(Path(os.environ['RUNNER_TEMP']) / f'sftp-{mode}'), mode,
        ])
        if result.returncode == 0 and mode == 'desktop':
            validation = subprocess.run(['node', str(ROOT / 'scripts/check-account-ui.mjs'),
                                         str(Path(os.environ['RUNNER_TEMP']) / 'sftp-desktop')])
            if validation.returncode:
                # Parser dependencies were already used by prepare-source. This
                # exercises the patched upstream settings contract, not a download.
                result.returncode = 42
        if result.returncode:
            if result.returncode == 42:
                record_failure(repo, tag, os.environ['PATCH_FINGERPRINT'],
                               f'https://github.com/{repo}/actions/runs/{os.environ["GITHUB_RUN_ID"]}',
                               os.environ['EXPECTED_SOURCE_SHA'])
            # Clone/network/toolchain failures remain retryable, not cached conflicts.
            return result.returncode
    return 0


if __name__ == '__main__':
    sys.exit(compile_kernel() if '--compile' in sys.argv else main())
