"""Provide frontend resources and the OCR source build's patch utility."""
import re
from pathlib import Path
import sys


def frontend(source):
    stages = list(re.finditer(r'(?mi)^FROM\s+[^\n]+', source))
    matches = [(stage.end(), stages[i + 1].start() if i + 1 < len(stages) else len(source))
               for i, stage in enumerate(stages) if re.search(r'\bAS\s+node-build\s*$', stage.group(), re.I)]
    if len(matches) != 1:
        raise ValueError('expected one frontend Docker build stage')
    start, end = matches[0]
    stage = source[start:end]
    # Upstream may already copy the resource that required this workaround.
    if re.search(r'(?mi)^COPY (?:kernel/ /kernel/|kernel/av/lunar_calendar_data\.json /kernel/av/lunar_calendar_data\.json)\s*$', stage):
        return source
    runs = list(re.finditer(r'(?mi)^RUN\b', stage))
    builds = [run.start() for i, run in enumerate(runs)
              if 'pnpm run build' in stage[run.start():runs[i+1].start() if i+1 < len(runs) else len(stage)]]
    if len(builds) != 1 or not re.search(r'(?mi)^WORKDIR /app\s*$', stage):
        raise ValueError('frontend Docker build contract changed')
    at = start + builds[0]
    return source[:at] + 'COPY kernel/ /kernel/\n' + source[at:]


def transform(source):
    source = frontend(source)
    # Repair only the old direct ONNX source build, irrespective of its stage
    # name. Newer upstream toolchain/artifact stages already provide patch and
    # must not be required to retain the old OCR package-install instruction.
    command = re.compile(r'(?m)^RUN python3 /onnxruntime/tools/ci_build/build\.py\b')
    for match in reversed(list(command.finditer(source))):
        start = source.rfind('\nFROM ', 0, match.start())
        stage = source[max(0, start):match.start()]
        installs = re.findall(r'(?m)^RUN apk add --no-cache (?:[^\n]*\\\n)*[^\n]*', stage)
        if any('patch' in install.replace('\\', ' ').split() for install in installs):
            continue
        source = source[:match.start()] + 'RUN apk add --no-cache patch\n' + source[match.start():]
    return source


if __name__ == '__main__':
    try:
        path = Path(sys.argv[1]) / 'Dockerfile'
        path.write_text(transform(path.read_text()))
    except (ValueError, OSError) as error:
        print('PATCH_CONFLICT:', error, file=sys.stderr)
        sys.exit(42)
