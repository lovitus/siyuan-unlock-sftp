import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('source_clone', Path(__file__).with_name('clone-release-source.py'))
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)


class SourceCloneTest(unittest.TestCase):
    def test_partial_transport_failure_retries_then_installs_complete_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp) / 'release'
            attempts = []
            def run(command, **options):
                partial = Path(command[-1])
                self.assertFalse(partial.exists())
                self.assertEqual(options['timeout'], 180)
                attempts.append(command)
                partial.mkdir()
                (partial / 'source').write_text('complete' if len(attempts) == 2 else 'partial')
                return Mock(returncode=0 if len(attempts) == 2 else 128,
                            stdout='' if len(attempts) == 2 else 'error: RPC failed; early EOF\n')
            with patch.object(source.subprocess, 'run', side_effect=run), patch.object(source.time, 'sleep') as sleep:
                self.assertEqual(source.clone('v3.8.9', destination), 0)
                self.assertEqual((destination / 'source').read_text(), 'complete')
                self.assertEqual(len(attempts), 2)
                sleep.assert_called_once_with(5)
                self.assertEqual(list(Path(tmp).iterdir()), [destination])

    def test_timeouts_are_bounded_and_leave_no_partial_destination(self):
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(source.subprocess, 'run', side_effect=subprocess.TimeoutExpired('git', 180)) as run, \
             patch.object(source.time, 'sleep') as sleep:
            self.assertEqual(source.clone('v3.8.9', Path(tmp) / 'release'), 128)
            self.assertEqual(run.call_count, 3)
            self.assertEqual([c.args[0] for c in sleep.call_args_list], [5, 10])
            self.assertFalse(list(Path(tmp).iterdir()))

    def test_invalid_tag_failure_is_not_retried(self):
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(source.subprocess, 'run', return_value=Mock(returncode=128, stdout='fatal: Remote branch not found\n')) as run, \
             patch.object(source.time, 'sleep') as sleep:
            self.assertEqual(source.clone('v3.8.9', Path(tmp) / 'release'), 128)
            run.assert_called_once()
            sleep.assert_not_called()

    def test_existing_destination_is_never_changed(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(source.subprocess, 'run') as run:
            existing = Path(tmp) / 'keep'
            existing.write_text('user data')
            with self.assertRaises(ValueError): source.clone('v3.8.9', tmp)
            self.assertEqual(existing.read_text(), 'user data')
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
