"""Configure Android through the upstream Gradle hook, independent of version syntax."""
from pathlib import Path
import re
import shutil
import sys
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[3]


def configure(root):
    root = Path(root)
    gradle = root / 'app/build.gradle'
    source = gradle.read_text()
    if not re.search(r"(?m)^\s*apply\s+from:\s*rootProject\.file\(['\"]signings\.gradle['\"]\)", source):
        raise ValueError('Android signing script hook changed')
    if not re.search(r'\bsigningConfigs\.siyuanConfig\b', source):
        raise ValueError('Android release signing selection changed')
    # A build-type resource overrides the AppDev flavor's package name. Put
    # the original package default in the hook, below flavor precedence.
    source = re.sub(r'''(?m)^[ \t]*resValue\s+["']string["']\s*,\s*["']app_package_name["']\s*,\s*["']org\.b3log\.siyuan["'][ \t]*\r?\n''', '', source)
    changes = {gradle: source}
    for relative, attribute, before, after in (
        ('app/src/main/AndroidManifest.xml', 'taskAffinity', 'org.b3log.siyuan.shorthand', '${applicationId}.shorthand'),
        ('app/src/main/res/xml/shortcuts.xml', 'targetPackage', 'org.b3log.siyuan', '@string/app_package_name'),
    ):
        path = root / relative
        source = path.read_text()
        document = ET.fromstring(source)
        key = '{http://schemas.android.com/apk/res/android}' + attribute
        values = [element.get(key) for element in document.iter() if key in element.attrib]
        if before not in values and after not in values:
            raise ValueError(f'Android {attribute} hook changed')
        changes[path] = re.sub(r'(android:' + attribute + r'''\s*=\s*["'])''' + re.escape(before) + r'''(["'])''',
                               lambda match: match[1] + after + match[2], source)
    for path, source in changes.items():
        path.write_text(source)
    shutil.copyfile(ROOT / 'overlays/android/signings.gradle', root / 'signings.gradle')


if __name__ == '__main__':
    try:
        configure(sys.argv[1])
    except (ValueError, OSError, ET.ParseError) as error:
        print('PATCH_CONFLICT:', error, file=sys.stderr)
        sys.exit(42)
