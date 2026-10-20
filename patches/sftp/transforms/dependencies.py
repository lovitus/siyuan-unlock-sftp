"""Add SFTP dependencies by module name; never depend on neighbouring versions."""
import json
from pathlib import Path
import subprocess
import sys

kernel = Path(sys.argv[1]) / 'kernel'
mod = json.loads(subprocess.check_output(['go', 'mod', 'edit', '-json'], cwd=kernel))
existing = {item['Path']: item['Version'] for item in mod.get('Require', [])}
for name, version in {'github.com/pkg/sftp': 'v1.13.9', 'github.com/kr/fs': 'v0.1.0'}.items():
    if name not in existing:
        subprocess.run(['go', 'mod', 'edit', f'-require={name}@{version}'], cwd=kernel, check=True)
sums = kernel / 'go.sum'
entries = {tuple(line.split()[:2]): line for line in sums.read_text().splitlines()}
for line in (Path(__file__).parent.parent / 'dependencies.sum').read_text().splitlines():
    key = tuple(line.split()[:2])
    if key in entries and entries[key] != line:
        raise ValueError('Conflicting dependency checksum: ' + ' '.join(key))
    entries[key] = line
sums.write_text('\n'.join(entries[key] for key in sorted(entries)) + '\n')
