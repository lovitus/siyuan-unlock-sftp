"""Clone an official release with bounded retries for transport failures only."""
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time

TRANSIENT = re.compile(r'RPC failed|early EOF|timed out|Failed to connect|Could not resolve host|'
                       r'(?:HTTP code|returned error:) (?:429|50[234])', re.I)


def clone(tag, destination):
    if not re.fullmatch(r'v\d+\.\d+\.\d+', tag):
        raise ValueError('Invalid release tag')
    destination = Path(destination).absolute()
    if destination.exists() or destination.is_symlink():
        raise ValueError('Source destination already exists')
    # All partial downloads belong to this invocation, on the destination disk.
    with tempfile.TemporaryDirectory(prefix='.sftp-clone-', dir=destination.parent) as tmp:
        for attempt in range(3):
            partial = Path(tmp) / f'source-{attempt}'
            try:
                result = subprocess.run([
                    'git', '-c', 'http.lowSpeedLimit=1024', '-c', 'http.lowSpeedTime=60',
                    'clone', '--branch', tag, '--depth=1',
                    'https://github.com/siyuan-note/siyuan.git', str(partial),
                ], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=180)
                output, code = result.stdout, result.returncode
            except subprocess.TimeoutExpired:
                output, code = 'Official source clone timed out after 180 seconds.\n', 128
            print(output, end='', flush=True)
            if code == 0:
                partial.rename(destination)
                return 0
            if attempt == 2 or not TRANSIENT.search(output):
                return code
            shutil.rmtree(partial, ignore_errors=True)
            delay = 5 * (attempt + 1)
            print(f'Retrying source transport in {delay}s ({attempt + 2}/3).', flush=True)
            time.sleep(delay)
    return 128


if __name__ == '__main__':
    sys.exit(clone(sys.argv[1], sys.argv[2]))
