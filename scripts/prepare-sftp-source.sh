#!/usr/bin/env bash
set -euo pipefail
version="${1:?version required}"
source_dir="${2:?source directory required}"
patch_root="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid release tag' >&2; exit 1; }
git clone --branch "$version" --depth=1 https://github.com/siyuan-note/siyuan.git "$source_dir"
cd "$source_dir"
# Keep the historical patch set for v3.8.3 and v3.8.4. Try the current patch set
# on every newer release; git apply failures stop preflight before builds.
if [[ "$version" == v3.8.3 ]]; then
 for patch in disable-update default-config mock-vip-user; do
  git apply "$patch_root/patches/siyuan/$patch.patch"
 done
 git apply "$patch_root/patches/siyuan/account-v3.8.3.patch"
 git apply "$patch_root/patches/sftp/provider.patch"
elif [[ "$version" == v3.8.4 ]]; then
 git apply "$patch_root/patches/sftp/source-v3.8.4.patch"
else
 git apply "$patch_root/patches/sftp/source-v3.8.5.patch"
fi
if [[ "${3:-desktop}" == docker ]]; then
 # The entrypoint already invokes /opt/siyuan/kernel; CMD contains only its args.
 # Accept upstream incorporating this exact fix in a later stable tag.
 if ! git apply --reverse --check "$patch_root/patches/sftp/docker-command.patch" 2>/dev/null; then
  git apply "$patch_root/patches/sftp/docker-command.patch"
 fi
fi
cp -R "$patch_root/overlays/kernel/sftpcloud" kernel/
git diff --check
