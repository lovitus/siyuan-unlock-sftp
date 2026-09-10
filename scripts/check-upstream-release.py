#!/usr/bin/env python3
"""Resolve a released version, not a moving package.json on master."""
import json
import os
import re
import subprocess
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def api(path):
    request = Request('https://api.github.com/' + path, headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
        'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
    })
    with urlopen(request, timeout=30) as response:
        return json.load(response)


def main():
    repo = os.environ['GITHUB_REPOSITORY']
    upstream = api('repos/appdev/siyuan-unlock/releases/latest')
    tag = upstream['tag_name']
    if not re.fullmatch(r'v\d+\.\d+\.\d+', tag):
        raise ValueError('Unsupported upstream release tag: ' + tag)
    if upstream['draft'] or upstream['prerelease']:
        raise ValueError('Expected a published stable release')
    try:
        release = api(f'repos/{repo}/releases/tags/{tag}')
    except HTTPError as error:
        if error.code != 404:
            raise
        release = None
    pending = release is None or release['draft']
    package = api(f'repos/siyuan-note/siyuan/contents/app/package.json?ref={tag}')
    import base64
    package = json.loads(base64.b64decode(package['content']))
    manager = package['packageManager']
    if not re.fullmatch(r'pnpm@\d+\.\d+\.\d+(?:\+sha\d+\.[a-fA-F0-9]+)?', manager):
        raise ValueError('Unexpected package manager: ' + manager)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'pending={str(pending).lower()}\nversion={tag}\npackage_manager={manager}\n')
    if not pending:
        print(f'{tag} is already published; nothing to do.')
        return
    if release is None:
        subprocess.run(['gh', 'release', 'create', tag, '--repo', repo,
                        '--target', os.environ['GITHUB_SHA'], '--draft', '--title', f'SiYuan SFTP {tag}',
                        '--notes', f"Based on {upstream['html_url']}. Adds SFTP cloud storage with an explicit remote path. Container images are published to GHCR."], check=True)
    print(f'Building {tag}; the release stays draft until every build succeeds.')


if __name__ == '__main__':
    main()
