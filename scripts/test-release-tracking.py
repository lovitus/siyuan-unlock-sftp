import base64
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import release_state as state

spec = importlib.util.spec_from_file_location('tracking', Path(__file__).with_name('check-upstream-release.py'))
tracking = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tracking)


class ReleaseTrackingTest(unittest.TestCase):
    def run_check(self, releases=None, tag='v3.8.7', event='schedule', source_sha='a' * 40,
                  attempt='1', error=None, manager='pnpm@12.6.0', lose_tag=False,
                  ios_sha='c' * 40, run_id='456', runs=None, write_error=None):
        releases = releases if releases is not None else []
        calls, mutations = [], []
        def api(path, method='GET', data=None):
            calls.append(path)
            if method != 'GET':
                if write_error:
                    raise HTTPError(path, write_error, 'test', {}, None)
                mutations.append((path, method, data))
                if method == 'POST':
                    releases.append(dict(data, id=99))
                    return releases[-1]
                release = next(r for r in releases if path.endswith('/' + str(r['id'])))
                release.update(data)
                if lose_tag:
                    release['tag_name'] = 'untagged-abcdef'
                return release
            if path == 'repos/appdev/siyuan-unlock/releases/latest':
                return dict(tag_name=tag, draft=False, prerelease=False, html_url='https://github.com/appdev/siyuan-unlock/releases/tag/' + tag)
            if '/actions/runs/' in path:
                return runs[path.rsplit('/', 1)[-1]]
            if path == 'repos/siyuan-note/siyuan-ios/commits/HEAD':
                return {'sha': ios_sha}
            if '/commits/' in path:
                return {'sha': source_sha}
            if '/contents/' in path:
                return {'content': base64.b64encode(json.dumps({'packageManager': manager}).encode()).decode()}
            if error:
                raise HTTPError(path, error, 'test', {}, None)
            if '/releases?per_page=' in path:
                return releases
            release = next((r for r in releases if r['tag_name'] == tag and not r['draft']), None)
            if release:
                return release
            raise HTTPError(path, 404, 'test', {}, None)
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'output'
            env = {'GITHUB_REPOSITORY': 'example/siyuan', 'GITHUB_SHA': 'abc',
                   'GITHUB_OUTPUT': str(output), 'GITHUB_EVENT_NAME': event,
                   'GITHUB_RUN_ID': run_id,
                   'GITHUB_RUN_ATTEMPT': attempt, 'GITHUB_STEP_SUMMARY': str(Path(tmp) / 'summary')}
            with patch.dict(os.environ, env), patch.object(tracking, 'api', side_effect=api):
                tracking.main()
            return output.read_text(), mutations, calls

    def draft(self, id=12, tag='v3.8.7', body='', actual_tag=None):
        return dict(id=id, tag_name=actual_tag or tag, name=f'SiYuan SFTP {tag}', draft=True,
                    body=f'Based on https://github.com/appdev/siyuan-unlock/releases/tag/{tag}.\n{body}')

    def test_published_is_skipped_even_with_legacy_drafts(self):
        released = dict(self.draft(), draft=False)
        output, mutations, calls = self.run_check([self.draft(13, actual_tag='untagged-one'), released])
        self.assertIn('pending=false', output)
        self.assertFalse(mutations)
        self.assertFalse(any('/contents/' in p or '/commits/' in p for p in calls))

    def test_future_version_automatically_creates_draft(self):
        output, mutations, _ = self.run_check(tag='v9.9.9')
        self.assertIn('pending=true', output)
        self.assertIn('release_id=99', output)
        self.assertEqual(mutations[0][1], 'POST')
        self.assertTrue(mutations[0][2]['draft'])
        self.assertEqual(mutations[0][2]['tag_name'], 'v9.9.9')
        self.assertEqual(state.previous_attempt(mutations[0][2], tracking.fingerprint('v9.9.9', 'a' * 40, ios_sha='c' * 40), '457'), '456')
        self.assertIn('ios_sha=' + 'c' * 40, output)

    def test_drafts_reused_by_id_and_updates_always_keep_tag(self):
        for actual in ('v3.8.7', 'untagged-one'):
            with self.subTest(actual=actual):
                output, mutations, _ = self.run_check([self.draft(actual_tag=actual)])
                self.assertIn('pending=true', output)
                self.assertIn('release_id=12', output)
                self.assertEqual(len(mutations), 1)
                path, method, data = mutations[0]
                self.assertTrue(path.endswith('/releases/12'))
                self.assertEqual(method, 'PATCH')
                self.assertEqual(data['tag_name'], 'v3.8.7')
                self.assertEqual(data['target_commitish'], 'abc')
                self.assertIn(state.version_marker('v3.8.7'), data['body'])

    def test_duplicate_untagged_drafts_contribute_failure_evidence(self):
        key = tracking.fingerprint('v3.8.7', 'a' * 40, ios_sha='c' * 40)
        releases = [self.draft(22, body=f'{state.BEGIN}{key} -->', actual_tag='untagged-two'),
                    self.draft(12, actual_tag='untagged-one')]
        output, mutations, calls = self.run_check(releases, lose_tag=True)
        self.assertIn('build_blocked=true', output)
        self.assertEqual(mutations[0][0], 'repos/example/siyuan/releases/12')
        self.assertFalse(any('/contents/' in p for p in calls))
        # Simulate GitHub continuing to return an untagged draft after PATCH.
        output, mutations, _ = self.run_check(releases)
        self.assertIn('pending=false', output)
        self.assertFalse(any(method == 'POST' for _, method, _ in mutations))
        self.assertEqual(len(releases), 2)

    def test_changed_source_inputs_or_manual_request_retries(self):
        key = tracking.fingerprint('v3.8.7', 'a' * 40, ios_sha='c' * 40)
        release = self.draft(body=f'{state.version_marker("v3.8.7")}\n{state.BEGIN}{key} -->')
        output, mutations, _ = self.run_check([release.copy()])
        self.assertIn('pending=false', output)
        self.assertFalse(mutations)
        for kwargs in ({'event': 'workflow_dispatch'}, {'attempt': '2'}, {'source_sha': 'b' * 40}, {'ios_sha': 'd' * 40}):
            with self.subTest(**kwargs):
                output, mutations, _ = self.run_check([release.copy()], **kwargs)
                self.assertIn('pending=true', output)
                self.assertTrue(mutations)
        with patch.object(tracking, 'fingerprint', return_value='c' * 64):
            output, _, _ = self.run_check([release.copy()])
            self.assertIn('pending=true', output)
        output, _, _ = self.run_check([release], tag='v3.8.8')
        self.assertIn('pending=true', output)

    def test_downstream_failure_and_missing_finalizer_stop_duplicate_builds(self):
        for conclusion in ('failure', 'timed_out', 'cancelled', 'action_required', 'success'):
            with self.subTest(conclusion=conclusion):
                releases = [self.draft()]
                output, _, _ = self.run_check(releases)
                self.assertIn('pending=true', output)
                # No preflight marker and no finalizer: only the saved run ID.
                self.assertNotIn(state.BEGIN, releases[0]['body'])
                output, _, calls = self.run_check(releases, run_id='457', runs={
                    '456': {'status': 'completed', 'conclusion': conclusion}})
                self.assertIn('build_blocked=true', output)
                self.assertIn('/actions/runs/456', releases[0]['body'])
                self.assertFalse(any('/contents/' in path for path in calls))
                # Subsequent schedules use the durable failure, without polling.
                output, mutations, calls = self.run_check(releases, run_id='458')
                self.assertIn('pending=false', output)
                self.assertFalse(mutations)
                self.assertFalse(any('/actions/runs/' in path for path in calls))
                output, _, _ = self.run_check(releases, run_id='459', event='workflow_dispatch')
                self.assertIn('pending=true', output)
                self.assertNotIn(state.BEGIN, releases[0]['body'])

    def test_running_attempt_is_not_duplicated_or_marked_failed(self):
        releases = [self.draft()]
        self.run_check(releases)
        output, mutations, _ = self.run_check(releases, run_id='457', runs={
            '456': {'status': 'in_progress', 'conclusion': None}})
        self.assertIn('build_in_progress=true', output)
        self.assertFalse(mutations)
        self.assertNotIn(state.BEGIN, releases[0]['body'])

    def test_ios_updates_do_not_retry_known_core_preflight_failures(self):
        key = tracking.fingerprint('v3.8.8', 'a' * 40)
        release = self.draft(tag='v3.8.8', body=f'{state.version_marker("v3.8.8")}\n{state.BEGIN}{key} -->')
        for ios_sha in ('c' * 40, 'd' * 40):
            output, mutations, calls = self.run_check([release.copy()], tag='v3.8.8', ios_sha=ios_sha)
            self.assertIn('build_blocked=true', output)
            self.assertFalse(mutations)
            self.assertFalse(any('siyuan-ios' in path or '/contents/' in path for path in calls))
        for kwargs in ({'source_sha': 'b' * 40}, {'tag': 'v3.8.9'}, {'event': 'workflow_dispatch'}, {'attempt': '2'}):
            output, _, _ = self.run_check([release.copy()], **dict({'tag': 'v3.8.8'}, **kwargs))
            self.assertIn('pending=true', output)
        output, _, _ = self.run_check(tag='v3.8.8')
        self.assertIn(f'preflight_fingerprint={key}', output)

    def test_attempt_in_legacy_duplicate_also_blocks(self):
        key = tracking.fingerprint('v3.8.7', 'a' * 40, ios_sha='c' * 40)
        releases = [self.draft(), self.draft(13, actual_tag='untagged-old',
                    body=state.with_attempt('notes', key, '123'))]
        output, _, _ = self.run_check(releases, runs={
            '123': {'status': 'completed', 'conclusion': 'failure'}})
        self.assertIn('pending=false', output)
        self.assertIn('/actions/runs/123', releases[0]['body'])

    def test_cannot_start_build_when_attempt_cannot_be_saved(self):
        with self.assertRaises(HTTPError):
            self.run_check([self.draft()], write_error=403)

    def test_legacy_identity_never_matches_by_title_or_substring_alone(self):
        for release in [self.draft(tag='v3.8.70', actual_tag='untagged-x'),
                        dict(self.draft(actual_tag='untagged-x'), body='unrelated notes')]:
            with self.subTest(release=release):
                self.assertFalse(state.is_draft_for(release, 'v3.8.7'))

    def test_api_failure_and_invalid_input_fail_closed(self):
        with self.assertRaises(HTTPError): self.run_check(error=403)
        with self.assertRaises(ValueError): self.run_check(tag='v3.8.7;echo bad')
        with self.assertRaises(ValueError): self.run_check(manager='pnpm@11;echo bad')


if __name__ == '__main__':
    unittest.main()
