#!/usr/bin/env python3
"""Resolve a released version, not a moving package.json on master."""
import json
import os
import re
import subprocess
from pathlib import Path
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
        # The tag endpoint can return 404 for an unpublished draft. Search the
        # authenticated release listing before deciding to create a new one.
        matches = []
        page = 1
        while True:
            releases = api(f'repos/{repo}/releases?per_page=100&page={page}')
            matches.extend(item for item in releases if item['tag_name'] == tag)
            if len(releases) < 100:
                break
            page += 1
        if len(matches) > 1:
            raise ValueError(f'Multiple releases exist for {tag}; reconcile duplicate drafts before building')
        release = matches[0] if matches else None
    pending = release is None or release['draft']
    supported = json.loads((Path(__file__).resolve().parent.parent /
                            'patches/sftp/supported-releases.json').read_text())['versions']
    if pending and tag not in supported:
        message = (f'{tag} is waiting for SFTP patch compatibility validation. '
                   'No build was attempted and no release was published. '
                   'Scheduled upstream checks remain enabled.')
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            output.write(f'pending=false\nversion={tag}\ncompatibility_pending=true\n')
        print(message)
        summary = os.environ.get('GITHUB_STEP_SUMMARY')
        if summary:
            with open(summary, 'a') as output:
                output.write('## Waiting for patch compatibility\n\n' + message + '\n')
        return
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
    elif release['draft']:
        # A retry may use a newer patch commit. Keep the draft tag target aligned
        # with the source used by the build jobs and its eventual release assets.
        subprocess.run(['gh', 'release', 'edit', tag, '--repo', repo,
                        '--target', os.environ['GITHUB_SHA']], check=True)
    print(f'Building {tag}; the release stays draft until every build succeeds.')


if __name__ == '__main__':
    main()
