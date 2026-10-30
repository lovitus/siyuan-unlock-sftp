import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('android', Path(__file__).resolve().parents[1] / 'patches/sftp/transforms/android.py')
android = importlib.util.module_from_spec(spec)
spec.loader.exec_module(android)


class AndroidSourceTest(unittest.TestCase):
    def source(self, root, version):
        files = {
            'app/build.gradle': "apply from: rootProject.file('signings.gradle')\nandroid {\n defaultConfig {\n  " + version + "\n }\n buildTypes {\n  release {\n   signingConfig = signingConfigs.siyuanConfig\n   resValue \"string\", \"app_package_name\", \"org.b3log.siyuan\"\n  }\n }\n}\n",
            'app/src/main/AndroidManifest.xml': '<manifest xmlns:android="http://schemas.android.com/apk/res/android"><activity android:taskAffinity="org.b3log.siyuan.shorthand" /></manifest>',
            'app/src/main/res/xml/shortcuts.xml': '<shortcuts xmlns:android="http://schemas.android.com/apk/res/android"><intent android:targetPackage="org.b3log.siyuan" /></shortcuts>',
        }
        for name, content in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def test_old_and_new_gradle_syntax_and_idempotence(self):
        for version in ('versionName siyuanVersionName', 'versionName = rootProject.siyuanVersionName'):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self.source(root, version)
                android.configure(root)
                self.assertIn(version, (root / 'app/build.gradle').read_text())
                self.assertNotIn('resValue', (root / 'app/build.gradle').read_text())
                self.assertIn('${applicationId}.shorthand', (root / 'app/src/main/AndroidManifest.xml').read_text())
                self.assertIn('@string/app_package_name', (root / 'app/src/main/res/xml/shortcuts.xml').read_text())
                self.assertEqual((root / 'signings.gradle').read_bytes(), (android.ROOT / 'overlays/android/signings.gradle').read_bytes())
                first = {p: p.read_bytes() for p in root.rglob('*') if p.is_file()}
                android.configure(root)
                self.assertEqual(first, {p: p.read_bytes() for p in root.rglob('*') if p.is_file()})

    def test_unknown_signing_selection_fails_before_writing(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.source(root, 'versionName = rootProject.siyuanVersionName')
            path = root / 'app/build.gradle'
            source = path.read_text().replace('signingConfigs.siyuanConfig', 'signingConfigs.changed')
            path.write_text(source)
            with self.assertRaises(ValueError): android.configure(root)
            self.assertEqual(path.read_text(), source)
            self.assertFalse((root / 'signings.gradle').exists())


if __name__ == '__main__':
    unittest.main()
