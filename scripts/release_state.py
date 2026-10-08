"""Durable patch-conflict records stored in the unpublished release notes."""
import hashlib
import json
import os
from pathlib import Path
import re
from urllib.error import HTTPError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parent.parent
BEGIN = '<!-- sftp-preflight-failure:'
END = '<!-- /sftp-preflight-failure -->'


def api(path, method='GET', data=None):
    request = Request('https://api.github.com/' + path, method=method,
                      data=None if data is None else json.dumps(data).encode(), headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
        'Accept': 'application/vnd.github+json',
        'Content-Type': 'application/json',
        'X-GitHub-Api-Version': '2022-11-28',
    })
    with urlopen(request, timeout=30) as response:
        return json.load(response)


def find_release(repo, tag, request=None):
    request = request or api
    try:
        return request(f'repos/{repo}/releases/tags/{tag}')
    except HTTPError as error:
        if error.code != 404:
            raise
        matches, page = [], 1
        while True:
            releases = request(f'repos/{repo}/releases?per_page=100&page={page}')
            matches.extend(item for item in releases if item['tag_name'] == tag)
            if len(releases) < 100:
                break
            page += 1
        if len(matches) > 1:
            raise ValueError(f'Multiple releases exist for {tag}; reconcile duplicate drafts before building')
        return matches[0] if matches else None


def fingerprint(tag, source_sha, root=ROOT):
    digest = hashlib.sha256(f'{tag}\n{source_sha}\n'.encode())
    for directory in ('patches', 'overlays', 'scripts', '.github/workflows'):
        for path in sorted((root / directory).rglob('*')):
            if not path.is_file() or '__pycache__' in path.parts or path.suffix == '.pyc':
                continue
            digest.update(path.relative_to(root).as_posix().encode() + b'\0')
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def failed_before(release, key):
    return bool(release and release['draft'] and f'{BEGIN}{key} -->' in (release.get('body') or ''))


def without_failure(body):
    return re.sub(re.escape(BEGIN) + r'.*?' + re.escape(END), '', body or '', flags=re.S).rstrip()


def record_failure(repo, tag, key, run_url, source_sha):
    release = find_release(repo, tag)
    if not release or not release['draft']:
        raise ValueError('Refusing to record patch failure without an unpublished draft')
    body = without_failure(release.get('body'))
    body += (f'\n\n{BEGIN}{key} -->\n'
             f'### Patch compatibility blocked\n\n'
             f'Patch preparation failed for `{tag}` at source `{source_sha}`. '
             f'[Original failure]({run_url}).\n\n'
             'Identical scheduled attempts are skipped. A new upstream commit, '
             'patch/build-input change, or manual workflow dispatch retries automatically.\n'
             f'{END}')
    api(f'repos/{repo}/releases/{release["id"]}', method='PATCH', data={'body': body})


def clear_failure(repo, tag):
    release = find_release(repo, tag)
    if release and release['draft'] and BEGIN in (release.get('body') or ''):
        api(f'repos/{repo}/releases/{release["id"]}', method='PATCH',
            data={'body': without_failure(release['body'])})
