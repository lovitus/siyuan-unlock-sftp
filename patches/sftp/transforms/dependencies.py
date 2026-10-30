"""Add SFTP dependencies by module name; never depend on neighbouring versions."""
import json
import os
from pathlib import Path
import shutil
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

# Use the release's selected dependency, never a per-version pin or a modified
# global module cache. The local replacement is needed only for the lock bug.
module = 'github.com/siyuan-note/dejavu'
selected = json.loads(subprocess.check_output(['go', 'list', '-m', '-json', module], cwd=kernel))
selected = selected.get('Replace', selected)
if not selected.get('Dir'):
    selected = json.loads(subprocess.check_output(
        ['go', 'mod', 'download', '-json', selected['Path'] + '@' + selected['Version']], cwd=kernel))
destination = kernel / 'sftp-deps' / 'dejavu'
shutil.copytree(selected['Dir'], destination)
for path in [destination, *destination.rglob('*')]:
    path.chmod(path.stat().st_mode | (0o700 if path.is_dir() else 0o200))
result = subprocess.run([os.path.abspath(sys.argv[2]), '--dejavu', str(destination)], text=True, stdout=subprocess.PIPE)
if result.returncode:
    sys.exit(result.returncode)
if result.stdout.strip() == 'repaired':
    subprocess.run(['go', 'mod', 'edit', f'-replace={module}=./sftp-deps/dejavu'], cwd=kernel, check=True)
    print('Repaired DejaVu recursive snapshot lock; kept the upstream dependency version.')
else:
    shutil.rmtree(destination)
    destination.parent.rmdir()
