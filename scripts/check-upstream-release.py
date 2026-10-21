#!/usr/bin/env python3
"""Resolve a released version, not a moving package.json on master."""
import json
import os
import re
from pathlib import Path
from release_state import (api, find_release, fingerprint, failed_before, update_draft,
                           version_marker, previous_attempt, with_attempt, record_failure)


def skip(tag, message, flag='build_blocked'):
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'pending=false\nversion={tag}\n{flag}=true\n')
    print(message)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        Path(os.environ['GITHUB_STEP_SUMMARY']).write_text(message)


def main():
    repo = os.environ['GITHUB_REPOSITORY']
    upstream = api('repos/appdev/siyuan-unlock/releases/latest')
    tag = upstream['tag_name']
    if not re.fullmatch(r'v\d+\.\d+\.\d+', tag):
        raise ValueError('Unsupported upstream release tag: ' + tag)
    if upstream['draft'] or upstream['prerelease']:
        raise ValueError('Expected a published stable release')
    release = find_release(repo, tag, request=api)
    if release and not release['draft']:
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            output.write(f'pending=false\nversion={tag}\n')
        print(f'{tag} is already published; nothing to do.')
        return
    source_sha = api(f'repos/siyuan-note/siyuan/commits/{tag}')['sha']
    if not re.fullmatch(r'[a-f0-9]{40}', source_sha):
        raise ValueError('Invalid upstream source commit')
    # The iOS repository has no version tags. Resolve its moving branch once,
    # fingerprint it and pass that exact commit to the build.
    ios_sha = api('repos/siyuan-note/siyuan-ios/commits/HEAD')['sha']
    if not re.fullmatch(r'[a-f0-9]{40}', ios_sha):
        raise ValueError('Invalid iOS source commit')
    key = fingerprint(tag, source_sha, ios_sha=ios_sha)
    manual_retry = (os.environ.get('GITHUB_EVENT_NAME') == 'workflow_dispatch' or
                    int(os.environ.get('GITHUB_RUN_ATTEMPT', '1')) > 1)
    run_id = os.environ['GITHUB_RUN_ID']
    previous = previous_attempt(release, key, run_id)
    if previous and not manual_retry and not failed_before(release, key):
        # One status read on the next schedule covers every downstream job and
        # even cancellation/timeouts that prevent a finalizer from running.
        run = api(f'repos/{repo}/actions/runs/{previous}')
        url = f'https://github.com/{repo}/actions/runs/{previous}'
        if run['status'] != 'completed':
            skip(tag, f'{tag}: [build already running]({url}); duplicate build skipped.', 'build_in_progress')
            return
        # A successful workflow with an unpublished draft is also incomplete.
        reason = f'Release workflow ended with {run["conclusion"]}; release is still draft'
        record_failure(repo, tag, key, url, source_sha, reason=reason, request=api)
        release = find_release(repo, tag, request=api)
    if failed_before(release, key) and not manual_retry:
        if release.get('tag_name') != tag or version_marker(tag) not in (release.get('body') or ''):
            update_draft(repo, tag, release, request=api)
        message = (f'{tag}: unchanged failed build inputs; build skipped. '
                   'No release was published. Change build inputs or use Run workflow to retry.\n\n'
                   + (release.get('body') or ''))
        skip(tag, message)
        return
    package = api(f'repos/siyuan-note/siyuan/contents/app/package.json?ref={tag}')
    import base64
    package = json.loads(base64.b64decode(package['content']))
    manager = package['packageManager']
    if not re.fullmatch(r'pnpm@\d+\.\d+\.\d+(?:\+sha\d+\.[a-fA-F0-9]+)?', manager):
        raise ValueError('Unexpected package manager: ' + manager)
    if release is None:
        release = api(f'repos/{repo}/releases', method='POST', data={
            'tag_name': tag, 'target_commitish': os.environ['GITHUB_SHA'],
            'draft': True, 'name': f'SiYuan SFTP {tag}',
            'body': with_attempt(f"Based on {upstream['html_url']}. Adds SFTP cloud storage with an explicit remote path. Container images are published to GHCR.\n\n" + version_marker(tag), key, run_id),
        })
    else:
        release = update_draft(repo, tag, release, request=api,
                               target_commitish=os.environ['GITHUB_SHA'],
                               body=with_attempt(release.get('body'), key, run_id))
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'pending=true\nversion={tag}\npackage_manager={manager}\nsource_sha={source_sha}\nios_sha={ios_sha}\npatch_fingerprint={key}\nrelease_id={release["id"]}\n')
    print(f'Building {tag}; the release stays draft until every build succeeds.')


if __name__ == '__main__':
    main()
