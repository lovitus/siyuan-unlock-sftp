"""Upload to the canonical draft by ID; never create or edit release metadata."""
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlencode

from release_state import api, find_release


def main(tag, paths):
    repo = os.environ['GITHUB_REPOSITORY']
    release = find_release(repo, tag)
    if not release or not release['draft']:
        raise ValueError('An unpublished release draft is required before uploading assets')
    files = [Path(path) for path in paths]
    if not files or any(not path.is_file() for path in files):
        raise ValueError('Release artifacts are missing')
    for path in files:
        for asset in release.get('assets', []):
            if asset['name'] == path.name:
                subprocess.run(['gh', 'api', '--method', 'DELETE',
                                f'repos/{repo}/releases/assets/{asset["id"]}'], check=True)
        url = (f'https://uploads.github.com/repos/{repo}/releases/{release["id"]}/assets?'
               + urlencode({'name': path.name}))
        subprocess.run(['gh', 'api', '--method', 'POST', '-H', 'Content-Type: application/octet-stream',
                        url, '--input', str(path), '--silent'], check=True)
        print(f'Uploaded {path.name} to release {release["id"]}')


if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2:])
