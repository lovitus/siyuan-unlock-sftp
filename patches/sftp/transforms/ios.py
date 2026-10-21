"""Preserve lazy menu creation without an unavailable stored Swift property."""
from pathlib import Path
import re
import sys


def transform(source):
    pattern = (r'(?m)^(?P<indent>[ \t]*)@available\(iOS 16\.0, \*\)\s*'
               r'private lazy var textMenuInteraction\s*=\s*'
               r'UIEditMenuInteraction\(delegate: self\)[ \t]*$')
    def replacement(match):
        lines = '''private var textMenuInteractionStorage: AnyObject?
@available(iOS 16.0, *)
private var textMenuInteraction: UIEditMenuInteraction {
  if let interaction = textMenuInteractionStorage as? UIEditMenuInteraction {
    return interaction
  }
  let interaction = UIEditMenuInteraction(delegate: self)
  textMenuInteractionStorage = interaction
  return interaction
}'''.splitlines()
        return '\n'.join(match['indent'] + line for line in lines)
    result, count = re.subn(pattern, replacement, source)
    if count > 1:
        raise ValueError('ambiguous iOS text menu declaration')
    # Older sources have no menu, and an upstream computed-property fix needs
    # no change. Do not depend on line numbers or a particular release version.
    return result


if __name__ == '__main__':
    path = Path(sys.argv[1]) / 'siyuan-ios/ViewController.swift'
    path.write_text(transform(path.read_text()))
