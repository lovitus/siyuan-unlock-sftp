import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch, Mock

import release_state as state

spec = importlib.util.spec_from_file_location('preflight', Path(__file__).with_name('release-preflight.py'))
preflight = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preflight)


class ConflictStateTest(unittest.TestCase):
    def test_fingerprint_changes_for_inputs_not_docs_or_python_cache(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'patches').mkdir()
            source = root / 'patches/current.patch'
            source.write_text('one')
            first = state.fingerprint('v1.0.0', 'a' * 40, root)
            (root / 'README.md').write_text('docs')
            (root / 'scripts/__pycache__').mkdir(parents=True)
            (root / 'scripts/__pycache__/release_state.pyc').write_bytes(b'cache')
            self.assertEqual(first, state.fingerprint('v1.0.0', 'a' * 40, root))
            (root / 'patches/node_modules').mkdir()
            (root / 'patches/node_modules/cache.js').write_text('generated dependency')
            self.assertEqual(first, state.fingerprint('v1.0.0', 'a' * 40, root))
            source.write_text('two')
            self.assertNotEqual(first, state.fingerprint('v1.0.0', 'a' * 40, root))
            self.assertNotEqual(first, state.fingerprint('v1.0.1', 'a' * 40, root))

    def test_failure_record_preserves_notes_and_is_replaced_not_appended(self):
        release = {'id': 12, 'tag_name': 'v1.0.0', 'draft': True, 'body': 'Original release notes'}
        def request(path, method='GET', data=None):
            if method == 'PATCH':
                self.assertTrue(path.endswith('/releases/12'))
                self.assertEqual(data['tag_name'], 'v1.0.0')
                release.update(data)
            if '/releases?per_page=' in path: return [release]
            return release
        with patch.object(state, 'api', side_effect=request):
            state.record_failure('owner/repo', 'v1.0.0', 'a' * 64, 'https://github.com/run/1', 'b' * 40)
            self.assertTrue(state.failed_before(release, 'a' * 64))
            state.record_failure('owner/repo', 'v1.0.0', 'c' * 64, 'https://github.com/run/2', 'b' * 40)
            self.assertFalse(state.failed_before(release, 'a' * 64))
            self.assertTrue(state.failed_before(release, 'c' * 64))
            self.assertEqual(release['body'].count(state.BEGIN), 1)
            state.clear_failure('owner/repo', 'v1.0.0')
            self.assertEqual(release['body'].strip(), 'Original release notes\n\n' + state.version_marker('v1.0.0'))

    def test_published_release_cannot_be_marked_blocked(self):
        with patch.object(state, 'api', return_value={'draft': False}):
            with self.assertRaises(ValueError):
                state.record_failure('owner/repo', 'v1.0.0', 'a' * 64, 'url', 'b' * 40)

    def test_only_conflicts_are_persisted_and_success_clears_previous_record(self):
        env = {'GITHUB_REPOSITORY': 'owner/repo', 'VERSION': 'v1.0.0',
               'RUNNER_TEMP': '/tmp/test-only', 'PATCH_FINGERPRINT': 'a' * 64,
               'EXPECTED_SOURCE_SHA': 'b' * 40, 'GITHUB_RUN_ID': '123'}
        for results, expected in [([42], 42), ([128], 128), ([1], 1), ([0, 1], 42), ([0, 0, 42], 42), ([0, 0, 0], 0)]:
            with self.subTest(results=results), patch.dict(os.environ, env), \
                 patch.object(preflight.subprocess, 'run', side_effect=[Mock(returncode=c) for c in results]) as run, \
                 patch.object(preflight, 'record_failure') as record, \
                 patch.object(preflight, 'clear_failure') as clear:
                self.assertEqual(preflight.main(), expected)
                self.assertEqual(run.call_count, len(results))
                self.assertEqual(record.call_count, int(expected == 42))
                self.assertEqual(clear.call_count, 0)


    def test_compile_errors_deduplicate_but_network_failures_remain_retryable(self):
        env = {'GITHUB_REPOSITORY': 'owner/repo', 'VERSION': 'v1.0.0',
               'RUNNER_TEMP': '/tmp/test-only', 'PATCH_FINGERPRINT': 'a' * 64,
               'EXPECTED_SOURCE_SHA': 'b' * 40, 'GITHUB_RUN_ID': '123'}
        for code, log, blocked in [(1, 'model/sftp.go:42:2: undefined: changedAPI\n', True),
                                    (1, 'go: downloading dependency: dial tcp: timeout\n', False),
                                    (0, '', False)]:
            with self.subTest(log=log), patch.dict(os.environ, env), \
                 patch.object(preflight.subprocess, 'run', return_value=Mock(returncode=code, stdout=log)), \
                 patch.object(preflight, 'record_failure') as record, \
                 patch.object(preflight, 'clear_failure') as clear:
                self.assertEqual(preflight.compile_kernel(), code)
                self.assertEqual(record.call_count, int(blocked))
                self.assertEqual(clear.call_count, int(code == 0))


if __name__ == '__main__':
    unittest.main()
