#!/usr/bin/env bash
set -euo pipefail
version="${1:?version required}"
source_dir="${2:?source directory required}"
patch_root="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid release tag' >&2; exit 1; }
git clone --branch "$version" --depth=1 https://github.com/siyuan-note/siyuan.git "$source_dir"
cd "$source_dir"
if [[ -n "${EXPECTED_SOURCE_SHA:-}" && "$(git rev-parse HEAD)" != "$EXPECTED_SOURCE_SHA" ]]; then
 echo 'Upstream tag moved after detection; retry with its new commit' >&2
 exit 1
fi
apply_patch() {
 echo "Applying ${1##*/}"
 if git apply --check "$1"; then
  git apply "$1"
 else
  result=$?
  if [[ "$result" == 1 ]]; then
   echo "PATCH_CONFLICT: ${1##*/}" >&2
   exit 42
  fi
  exit "$result"
 fi
}
# Historical releases keep their original implementation. All newer releases
# share overlays and structural hooks selected by API shape, never version.
if [[ "$version" == v3.8.3 ]]; then
 for patch in disable-update default-config mock-vip-user; do
  apply_patch "$patch_root/patches/siyuan/$patch.patch"
 done
 apply_patch "$patch_root/patches/siyuan/account-v3.8.3.patch"
 apply_patch "$patch_root/patches/sftp/provider.patch"
elif [[ "$version" == v3.8.4 ]]; then
 apply_patch "$patch_root/patches/sftp/source-v3.8.4.patch"
else
 # Compile for the host even when the caller has set cross-build variables.
 transform_dir=$(mktemp -d)
 trap 'rm -rf "$transform_dir"' EXIT
 GO111MODULE=off GOOS= GOARCH= CGO_ENABLED=0 GOTOOLCHAIN=local go build \
  -o "$transform_dir/apply-source.exe" "$patch_root/patches/sftp/transforms/account.go" "$patch_root/patches/sftp/transforms/source.go" "$patch_root/patches/sftp/transforms/dejavu.go"
 "$transform_dir/apply-source.exe" .
 node "$patch_root/patches/sftp/transforms/frontend.mjs" .
 python3 "$patch_root/patches/sftp/transforms/dependencies.py" . "$transform_dir/apply-source.exe"
 for component in api apicontract conf model; do
  cp "$patch_root/overlays/kernel/$component/"*.go "kernel/$component/"
 done
 cp "$patch_root/overlays/app/src/config/tabs/sftp.ts" app/src/config/tabs/
fi
if [[ "${3:-desktop}" == docker ]]; then
 if [[ "$version" != v3.8.3 && "$version" != v3.8.4 ]]; then
  python3 "$patch_root/patches/sftp/transforms/docker.py" .
 fi
 # The entrypoint already invokes /opt/siyuan/kernel; CMD contains only its args.
 # Accept upstream incorporating this exact fix in a later stable tag.
 if ! git apply --reverse --check "$patch_root/patches/sftp/docker-command.patch" 2>/dev/null; then
  apply_patch "$patch_root/patches/sftp/docker-command.patch"
 fi
fi
cp -R "$patch_root/overlays/kernel/sftpcloud" kernel/
git diff --check
