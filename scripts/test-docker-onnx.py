import importlib.util
from pathlib import Path
from unittest import TestCase, main
from unittest.mock import Mock


spec = importlib.util.spec_from_file_location('resolve_onnx', Path(__file__).with_name('resolve-docker-onnx.py'))
resolver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(resolver)


class RuntimeResolution(TestCase):
    def setUp(self):
        self.onnx = Mock()
        self.onnx.PLATFORMS = {'linux/arm64': 'arm64'}
        self.onnx.inputs.return_value = ('a' * 64, '1.24.3')
        self.onnx.resolve.return_value = 'b3log/siyuan@sha256:' + 'b' * 64

    def resolve(self):
        return resolver.resolve(self.onnx, Path('source'), 'ghcr.io/owner/project', 'linux/arm64', Path('records'))

    def test_matching_upstream_artifact_is_validated_without_building(self):
        result = self.resolve()
        self.assertEqual(result['origin'], 'upstream')
        self.onnx.validate.assert_called_once_with(result['image'], 'linux/arm64', 'a' * 64, '1.24.3')
        self.onnx.ensure.assert_not_called()

    def test_changed_inputs_select_a_new_artifact_without_version_rules(self):
        self.onnx.inputs.return_value = ('c' * 64, '2.0.0')
        self.resolve()
        self.onnx.resolve.assert_called_once_with('b3log/siyuan:onnx-arm64-' + 'c' * 64)

    def test_only_a_missing_artifact_builds_in_our_registry(self):
        self.onnx.resolve.return_value = None
        self.onnx.ensure.return_value = {'image': 'ghcr.io/owner/project@sha256:' + 'd' * 64}
        result = self.resolve()
        self.assertEqual(result['origin'], 'ghcr')
        self.onnx.ensure.assert_called_once_with(Path('source'), 'ghcr.io/owner/project', 'linux/arm64', 2, Path('records'))

    def test_registry_failure_does_not_start_a_rebuild(self):
        self.onnx.resolve.side_effect = RuntimeError('registry unavailable')
        with self.assertRaisesRegex(RuntimeError, 'registry unavailable'):
            self.resolve()
        self.onnx.ensure.assert_not_called()

    def test_invalid_artifact_does_not_start_a_rebuild(self):
        self.onnx.validate.side_effect = ValueError('inputs do not match')
        with self.assertRaisesRegex(ValueError, 'inputs do not match'):
            self.resolve()
        self.onnx.ensure.assert_not_called()


if __name__ == '__main__':
    main()
