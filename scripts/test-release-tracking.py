import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError
import base64
import json

spec = importlib.util.spec_from_file_location('tracking', Path(__file__).with_name('check-upstream-release.py'))
tracking = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tracking)


class ReleaseTrackingTest(unittest.TestCase):
    def run_check(self, release=None, error=None, tag='v3.8.3', manager='pnpm@11.25.0'):
        calls = []
        def api(path):
            calls.append(path)
            if path == 'repos/appdev/siyuan-unlock/releases/latest':
                return dict(tag_name=tag, draft=False, prerelease=False, html_url='https://github.com/appdev/siyuan-unlock/releases/tag/' + tag)
            if '/contents/' in path:
                return {'content': base64.b64encode(json.dumps({'packageManager': manager}).encode()).decode()}
            if error:
                raise HTTPError(path, error, 'test error', {}, None)
            return release
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'output'
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/siyuan', 'GITHUB_SHA': 'abc', 'GITHUB_OUTPUT': str(output)}), patch.object(tracking, 'api', side_effect=api), patch.object(tracking.subprocess, 'run') as run:
                tracking.main()
                return output.read_text(), run.call_args_list, calls

    def test_published_is_skipped(self):
        output, mutations, calls = self.run_check({'draft': False})
        self.assertIn('pending=false', output)
        self.assertFalse(mutations)
        self.assertIn('repos/siyuan-note/siyuan/contents/app/package.json?ref=v3.8.3', calls)

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

    def test_api_failure_never_creates_release(self):
        with self.assertRaises(HTTPError):
            self.run_check(error=403)

    def test_untrusted_tag_and_package_manager_rejected(self):
        with self.assertRaises(ValueError):
            self.run_check(tag='v3.8.3;echo bad')
        with self.assertRaises(ValueError):
            self.run_check(manager='pnpm@11;echo bad')


if __name__ == '__main__':
    unittest.main()
