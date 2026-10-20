import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('upload', Path(__file__).with_name('upload-release-assets.py'))
upload = importlib.util.module_from_spec(spec)
spec.loader.exec_module(upload)


class AssetUploadTest(unittest.TestCase):
    def test_uploads_to_identified_draft_without_mutating_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / 'test package.dmg'
            artifact.write_bytes(b'artifact')
            release = dict(id=42, draft=True, assets=[{'id': 3, 'name': artifact.name}, {'id': 4, 'name': 'other.apk'}])
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'owner/repo'}), \
                 patch.object(upload, 'find_release', return_value=release), \
                 patch.object(upload.subprocess, 'run') as run:
                upload.main('v3.8.7', [str(artifact)])
                self.assertEqual(run.call_count, 2)
                self.assertIn('repos/owner/repo/releases/assets/3', run.call_args_list[0].args[0])
                self.assertIn('https://uploads.github.com/repos/owner/repo/releases/42/assets?name=test+package.dmg', run.call_args_list[1].args[0])
                self.assertIn(str(artifact), run.call_args_list[1].args[0])
                self.assertFalse(any('PATCH' in call.args[0] or 'release' in call.args[0] for call in run.call_args_list))

    def test_published_or_missing_draft_never_uploads(self):
        for release in (None, {'draft': False}):
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'owner/repo'}), \
                 patch.object(upload, 'find_release', return_value=release), \
                 patch.object(upload.subprocess, 'run') as run:
                with self.assertRaises(ValueError): upload.main('v3.8.7', ['unused'])
                run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
