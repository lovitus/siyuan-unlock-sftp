#!/usr/bin/env python3
"""Reuse upstream's input-matched OCR runtime before building our own in GHCR."""
import argparse
import importlib.util
import json
from pathlib import Path
import time


def resolve(onnx, context, repository, platform, records):
    key, version = onnx.inputs(context, platform)
    reference = f'b3log/siyuan:onnx-{onnx.PLATFORMS[platform]}-{key}'
    image = onnx.resolve(reference)
    if image is not None:
        # Upstream validates the immutable digest, architecture, input labels,
        # license files and shared-library dependencies before returning it.
        onnx.validate(image, platform, key, version)
        return dict(image=image, inputs=key, version=version, platform=platform,
                    reused=True, origin='upstream')
    # Only an explicitly missing artifact returns None. Authentication, network
    # errors or invalid artifacts must not silently trigger a costly rebuild.
    return dict(onnx.ensure(context, repository, platform, 4 if platform == 'linux/arm/v7' else 2,
                            records), origin='ghcr')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--context', type=Path, required=True)
    parser.add_argument('--repository', required=True)
    parser.add_argument('--platform', required=True)
    parser.add_argument('--records', type=Path, required=True)
    parser.add_argument('--github-output', type=Path, required=True)
    args = parser.parse_args()
    context = args.context.resolve()
    args.records.mkdir(parents=True, exist_ok=True)
    started = time.monotonic()
    report = dict(status='failed', platform=args.platform)
    try:
        script = context / 'scripts/build-docker-onnx.py'
        if not script.exists():
            # Older official source tags have no independent ONNX artifact.
            report.update(status='passed', origin='source-default')
            return
        spec = importlib.util.spec_from_file_location('upstream_onnx', script)
        onnx = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(onnx)
        report.update(resolve(onnx, context, args.repository, args.platform, args.records))
        with args.github_output.open('a', encoding='utf-8') as output:
            output.write(f'onnx_image={report["image"]}\n')
        report['status'] = 'passed'
    except Exception as error:
        report['error'] = str(error)
        raise
    finally:
        report['seconds'] = round(time.monotonic() - started, 3)
        args.records.joinpath('report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report), flush=True)


if __name__ == '__main__':
    main()
