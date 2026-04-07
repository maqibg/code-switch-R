#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required. Install from https://cli.github.com/" >&2
  exit 1
fi

if [ $# -lt 1 ]; then
  echo "Usage: scripts/publish_release.sh <tag> [notes-file]" >&2
  exit 1
fi

TAG="$1"
NOTES="${2:-RELEASE_NOTES.md}"
VERSION="${TAG#v}"

if [ ! -f "$NOTES" ]; then
  echo "Release notes file '$NOTES' not found" >&2
  exit 1
fi

MAC_APP_PRIMARY="bin/code-switch-R.app"
MAC_APP_ALT="bin/code-switch-R.app"
MAC_ARCHS=("arm64" "amd64")
MAC_ZIPS=()

package_macos_arch() {
  local arch="$1"
  local staging_app="bin/code-switch-R-${arch}.app"
  local zip_path="bin/code-switch-R-v${VERSION}-macos-${arch}.zip"

  echo "==> Building macOS ${arch}"
  env ARCH="$arch" wails3 task package ${BUILD_OPTS:-}

  local bundle_path="$MAC_APP_PRIMARY"
  if [ ! -d "$bundle_path" ] && [ -d "$MAC_APP_ALT" ]; then
    bundle_path="$MAC_APP_ALT"
  fi

  if [ ! -d "$bundle_path" ]; then
    echo "Missing asset: $MAC_APP_PRIMARY (or $MAC_APP_ALT)" >&2
    exit 1
  fi

  rm -rf "$staging_app"
  mv "$bundle_path" "$staging_app"

  echo "==> Archiving macOS app bundle (${arch})"
  rm -f "$zip_path"
  ditto -c -k --sequesterRsrc --keepParent "$staging_app" "$zip_path"
  rm -rf "$staging_app"

  MAC_ZIPS+=("$zip_path")
}

perl -0pi -e "s/const\\s+AppVersion\\s*=\\s*\"[^\"]*\"/const AppVersion = \"$TAG\"/" version_service.go

wails3 task common:update:build-assets
for arch in "${MAC_ARCHS[@]}"; do
  package_macos_arch "$arch"
done

env ARCH=amd64 wails3 task windows:package ${BUILD_OPTS:-}

# 构建 updater.exe（静默更新辅助程序）
echo "==> Building updater.exe"
wails3 task windows:build:updater ${BUILD_OPTS:-}

if [ -f "bin/code-switch-R.exe" ]; then
  mv "bin/code-switch-R.exe" "bin/code-switch-R-v${VERSION}.exe"
fi

if [ -f "build/windows/nsis/code-switch-R-amd64-installer.exe" ]; then
  mv "build/windows/nsis/code-switch-R-amd64-installer.exe" "bin/code-switch-R-v${VERSION}-amd64-installer.exe"
elif [ -f "bin/code-switch-R-amd64-installer.exe" ]; then
  mv "bin/code-switch-R-amd64-installer.exe" "bin/code-switch-R-v${VERSION}-amd64-installer.exe"
fi

# 生成 SHA256 哈希文件
echo "==> Generating SHA256 checksums"
generate_sha256() {
  local file="$1"
  if [ -f "$file" ]; then
    local hash_file="${file}.sha256"
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$file" | awk '{print $1 "  " FILENAME}' FILENAME="$(basename "$file")" > "$hash_file"
    elif command -v shasum >/dev/null 2>&1; then
      shasum -a 256 "$file" | awk '{print $1 "  " FILENAME}' FILENAME="$(basename "$file")" > "$hash_file"
    else
      echo "Warning: no sha256sum or shasum available, skipping hash for $file" >&2
      return 1
    fi
    echo "  hash: $hash_file"
  fi
}

generate_sha256 "bin/code-switch-R-v${VERSION}.exe"
generate_sha256 "bin/updater.exe"

ASSETS=(
  "${MAC_ZIPS[@]}"
  "bin/code-switch-R-v${VERSION}-amd64-installer.exe"
  "bin/code-switch-R-v${VERSION}.exe"
  "bin/code-switch-R-v${VERSION}.exe.sha256"
  "bin/updater.exe"
  "bin/updater.exe.sha256"
)

for asset in "${ASSETS[@]}"; do
  [ -e "$asset" ] || { echo "Missing asset: $asset" >&2; exit 1; }
  echo "  asset: $asset"
done

gh release create "$TAG" "${ASSETS[@]}" \
  --title "$TAG" \
  --notes-file "$NOTES"
