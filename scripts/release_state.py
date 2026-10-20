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


def version_marker(tag):
    return f'<!-- sftp-release:{tag} -->'


def is_draft_for(release, tag):
    body = release.get('body') or ''
    # Old drafts may have been stored under an untagged-* tag. Require both
    # the exact generated title and upstream URL, never a version substring.
    legacy = (release.get('name') == f'SiYuan SFTP {tag}' and
              re.search(r'https://github\.com/appdev/siyuan-unlock/releases/tag/' +
                        re.escape(tag) + r'(?=[.\s<)]|$)', body))
    return release['draft'] and (release.get('tag_name') == tag or
                                version_marker(tag) in body or bool(legacy))


def find_release(repo, tag, request=None):
    request = request or api
    matches = {}
    try:
        release = request(f'repos/{repo}/releases/tags/{tag}')
        if release:
            if not release['draft']:
                return release
            matches[release['id']] = release
    except HTTPError as error:
        if error.code != 404:
            raise
    page = 1
    while True:
        releases = request(f'repos/{repo}/releases?per_page=100&page={page}')
        for item in releases:
            if item.get('tag_name') == tag and not item['draft']:
                return item
            if is_draft_for(item, tag):
                matches[item['id']] = item
        if len(releases) < 100:
            break
        page += 1
    if not matches:
        return None
    # Stable choice across API ordering changes. Preserve all existing assets;
    # duplicates contribute failure evidence but are never deleted implicitly.
    release = dict(min(matches.values(), key=lambda r: (
        r.get('tag_name') != tag, version_marker(tag) not in (r.get('body') or ''), r['id'])))
    release['_failure_bodies'] = [r.get('body') or '' for r in matches.values()]
    return release


def update_draft(repo, tag, release, request=None, **changes):
    if not release['draft']:
        raise ValueError('Refusing to mutate a published release')
    body = changes.pop('body', release.get('body') or '')
    if version_marker(tag) not in body:
        body = body.rstrip() + '\n\n' + version_marker(tag)
    # Every PATCH carries the real tag. Do not rely on tag-based CLI lookup
    # or on omitted fields retaining a draft's tag identity.
    return (request or api)(f'repos/{repo}/releases/{release["id"]}', method='PATCH',
                           data=dict(changes, tag_name=tag, body=body))


def fingerprint(tag, source_sha, root=ROOT):
    digest = hashlib.sha256(f'{tag}\n{source_sha}\n'.encode())
    for directory in ('patches', 'overlays', 'scripts', '.github/workflows', '.github/actions'):
        for path in sorted((root / directory).rglob('*')):
            if not path.is_file() or any(part in ('__pycache__', 'node_modules', '.git') for part in path.parts) or path.suffix == '.pyc':
                continue
            digest.update(path.relative_to(root).as_posix().encode() + b'\0')
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def failed_before(release, key):
    return bool(release and release['draft'] and any(
        f'{BEGIN}{key} -->' in body for body in
        [release.get('body') or '', *release.get('_failure_bodies', [])]))


def without_failure(body):
    return re.sub(r'\n*' + re.escape(BEGIN) + r'.*?' + re.escape(END), '', body or '', flags=re.S).rstrip()


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
    update_draft(repo, tag, release, body=body)


def clear_failure(repo, tag):
    release = find_release(repo, tag)
    if release and release['draft'] and BEGIN in (release.get('body') or ''):
        update_draft(repo, tag, release, body=without_failure(release['body']))
