"""Keep shared kernel resources available to the upstream frontend build."""
import re
from pathlib import Path
import sys


def transform(source):
    stages = list(re.finditer(r'(?mi)^FROM\s+[^\n]+', source))
    matches = [(stage.end(), stages[i + 1].start() if i + 1 < len(stages) else len(source))
               for i, stage in enumerate(stages) if re.search(r'\bAS\s+node-build\s*$', stage.group(), re.I)]
    if len(matches) != 1:
        raise ValueError('expected one frontend Docker build stage')
    start, end = matches[0]
    stage = source[start:end]
    if re.search(r'(?mi)^COPY kernel/ /kernel/\s*$', stage):
        return source
    runs = list(re.finditer(r'(?mi)^RUN\b', stage))
    builds = [run.start() for i, run in enumerate(runs)
              if 'pnpm run build' in stage[run.start():runs[i+1].start() if i+1 < len(runs) else len(stage)]]
    if len(builds) != 1 or not re.search(r'(?mi)^WORKDIR /app\s*$', stage):
        raise ValueError('frontend Docker build contract changed')
    at = start + builds[0]
    return source[:at] + 'COPY kernel/ /kernel/\n' + source[at:]


if __name__ == '__main__':
    try:
        path = Path(sys.argv[1]) / 'Dockerfile'
        path.write_text(transform(path.read_text()))
    except (ValueError, OSError) as error:
        print('PATCH_CONFLICT:', error, file=sys.stderr)
        sys.exit(42)
