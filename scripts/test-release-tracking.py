import importlib.util
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch
from urllib.error import HTTPError
import base64
import json

spec = importlib.util.spec_from_file_location('tracking', Path(__file__).with_name('check-upstream-release.py'))
tracking = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tracking)


class ReleaseTrackingTest(unittest.TestCase):
    def run_check(self, release=None, error=None, tag='v3.8.3', manager='pnpm@11.25.0', listed=None, event='schedule', source_sha='a' * 40, attempt='1'):
        calls = []
        def api(path):
            calls.append(path)
            if path == 'repos/appdev/siyuan-unlock/releases/latest':
                return dict(tag_name=tag, draft=False, prerelease=False, html_url='https://github.com/appdev/siyuan-unlock/releases/tag/' + tag)
            if '/commits/' in path:
                return {'sha': source_sha}
            if '/contents/' in path:
                return {'content': base64.b64encode(json.dumps({'packageManager': manager}).encode()).decode()}
            if '/releases?per_page=' in path:
                return listed or []
            if error:
                raise HTTPError(path, error, 'test error', {}, None)
            return release
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'output'
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/siyuan', 'GITHUB_SHA': 'abc', 'GITHUB_OUTPUT': str(output), 'GITHUB_EVENT_NAME': event, 'GITHUB_RUN_ATTEMPT': attempt, 'GITHUB_STEP_SUMMARY': str(Path(tmp) / 'summary')}), patch.object(tracking, 'api', side_effect=api), patch.object(tracking.subprocess, 'run') as run:
                tracking.main()
                return output.read_text(), run.call_args_list, calls

    def test_published_is_skipped(self):
        output, mutations, calls = self.run_check({'draft': False})
        self.assertIn('pending=false', output)
        self.assertFalse(mutations)
        self.assertFalse(any('/contents/' in path or '/commits/' in path for path in calls))

    def test_draft_is_retried(self):
        output, mutations, _ = self.run_check({'draft': True})
        self.assertIn('pending=true', output)
        self.assertEqual(mutations[0].args[0], [
            'gh', 'release', 'edit', 'v3.8.3', '--repo', 'example/siyuan',
            '--target', 'abc',
        ])

    def test_new_release_starts_draft(self):
        output, mutations, _ = self.run_check(error=404)
        self.assertIn('pending=true', output)
        args = mutations[0].args[0]
        self.assertIn('--draft', args)
        self.assertIn('example/siyuan', args)

    def test_new_unlisted_version_starts_build(self):
        output, mutations, calls = self.run_check(error=404, tag='v9.9.9')
        self.assertIn('pending=true', output)
        self.assertEqual(len(mutations), 1)
        self.assertEqual(mutations[0].args[0][2], 'create')
        self.assertIn('v9.9.9', mutations[0].args[0])
        self.assertIn('repos/siyuan-note/siyuan/contents/app/package.json?ref=v9.9.9', calls)

    def test_new_unlisted_draft_is_retried(self):
        output, mutations, _ = self.run_check({'draft': True}, tag='v9.9.9')
        self.assertIn('pending=true', output)
        self.assertEqual(mutations[0].args[0][2], 'edit')
        self.assertIn('v9.9.9', mutations[0].args[0])

    def test_validated_v384_starts_build(self):
        output, mutations, calls = self.run_check(error=404, tag='v3.8.4')
        self.assertIn('pending=true', output)
        self.assertNotIn('compatibility_pending=true', output)
        self.assertIn('v3.8.4', mutations[0].args[0])
        self.assertIn('repos/siyuan-note/siyuan/contents/app/package.json?ref=v3.8.4', calls)

    def test_identical_conflict_is_skipped_but_manual_and_changed_source_retry(self):
        key = tracking.fingerprint('v9.9.9', 'a' * 40)
        release = {'draft': True, 'body': f'<!-- sftp-preflight-failure:{key} -->'}
        output, mutations, calls = self.run_check(release, tag='v9.9.9')
        self.assertIn('patch_blocked=true', output)
        self.assertIn('pending=false', output)
        self.assertFalse(mutations)
        self.assertFalse(any('/contents/' in path for path in calls))
        for kwargs in [{'event': 'workflow_dispatch'}, {'attempt': '2'}, {'source_sha': 'b' * 40}]:
            with self.subTest(**kwargs):
                output, mutations, _ = self.run_check(release, tag='v9.9.9', **kwargs)
                self.assertIn('pending=true', output)
                self.assertTrue(mutations)
        with patch.object(tracking, 'fingerprint', return_value='c' * 64):
            output, mutations, _ = self.run_check(release, tag='v9.9.9')
            self.assertIn('pending=true', output)
            self.assertTrue(mutations)

    def test_api_failure_never_creates_release(self):
        with self.assertRaises(HTTPError):
            self.run_check(error=403)

    def test_draft_missing_from_tag_endpoint_is_reused(self):
        output, mutations, calls = self.run_check(error=404, listed=[
            {'tag_name': 'v3.8.3', 'draft': True},
        ])
        self.assertIn('pending=true', output)
        self.assertEqual(len(mutations), 1)
        self.assertEqual(mutations[0].args[0][2], 'edit')
        self.assertIn('repos/example/siyuan/releases?per_page=100&page=1', calls)

    def test_duplicate_drafts_are_not_silently_selected(self):
        with self.assertRaises(ValueError):
            self.run_check(error=404, listed=[
                {'tag_name': 'v3.8.3', 'draft': True},
                {'tag_name': 'v3.8.3', 'draft': True},
            ])

    def test_untrusted_tag_and_package_manager_rejected(self):
        with self.assertRaises(ValueError):
            self.run_check(tag='v3.8.3;echo bad')
        with self.assertRaises(ValueError):
            self.run_check(manager='pnpm@11;echo bad')


class SourcePreparationTest(unittest.TestCase):
    def prepare(self, version, mode='desktop', fail_patch=False, fail_clone=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fake_git = root / 'git'
            fake_git.write_text("""#!/bin/sh
printf '%s\n' "$*" >> "$GIT_TEST_LOG"
if [ "$1" = clone ]; then
    [ "${FAIL_CLONE:-0}" = 0 ] || exit 128
    for destination do :; done
    mkdir -p "$destination/kernel/api"
elif [ "$1" = apply ] && [ "$FAIL_PATCH" = 1 ]; then
    exit 1
fi
""")
            fake_git.chmod(0o755)
            fake_go = root / 'go'
            fake_go.write_text("#!/bin/sh\nprintf '#!/bin/sh\\nexit 0\\n' > \"$3\"\nchmod +x \"$3\"\n")
            fake_go.chmod(0o755)
            log = root / 'git.log'
            destination = root / 'source'
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
                       GIT_TEST_LOG=str(log), FAIL_PATCH=str(int(fail_patch)), FAIL_CLONE=str(int(fail_clone)))
            result = subprocess.run([
                'bash', str(Path(__file__).with_name('prepare-sftp-source.sh').resolve()),
                version, str(destination), mode,
            ], env=env, capture_output=True, text=True)
            return result, log.read_text(), (destination / 'kernel/sftpcloud').exists()

    def test_future_versions_use_current_patch(self):
        for version in ['v3.8.4', 'v3.8.5', 'v3.8.6', 'v4.0.0']:
            for mode in ['desktop', 'docker']:
                with self.subTest(version=version, mode=mode):
                    result, log, overlay = self.prepare(version, mode)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn('source-v3.8.4.patch' if version == 'v3.8.4' else 'current/provider.patch', log)
                    self.assertNotIn('hide-account-entry.patch', log)
                    self.assertTrue(overlay)

    def test_clone_failure_is_not_a_patch_conflict(self):
        result, log, overlay = self.prepare('v3.8.6', fail_clone=True)
        self.assertEqual(result.returncode, 128)
        self.assertFalse(overlay)
        self.assertNotIn('apply', log)

    def test_historical_version_keeps_its_patch_set(self):
        result, log, overlay = self.prepare('v3.8.3')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('account-v3.8.3.patch', log)
        self.assertIn('provider.patch', log)
        self.assertNotIn('source-v3.8.4.patch', log)
        self.assertTrue(overlay)

    def test_patch_failure_stops_source_preparation(self):
        for mode in ['desktop', 'docker']:
            with self.subTest(mode=mode):
                result, log, overlay = self.prepare('v3.8.5', mode, fail_patch=True)
                self.assertEqual(result.returncode, 42)
                self.assertFalse(overlay)
                self.assertNotIn('diff --check', log)
                self.assertNotIn('docker-command.patch', log)


if __name__ == '__main__':
    unittest.main()
