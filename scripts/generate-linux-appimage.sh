#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "Usage: $0 <binary> <icon> <desktop-file> <output-dir> <build-dir>" >&2
  exit 2
fi

binary="$1"
icon="$2"
desktop_file="$3"
output_dir="$4"
build_dir="$5"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
wails_version="$(awk '$1 == "github.com/wailsapp/wails/v3" { print $2; exit }' "${repo_root}/go.mod")"

if [ -z "$wails_version" ]; then
  echo "Unable to read github.com/wailsapp/wails/v3 version from go.mod" >&2
  exit 1
fi

module_dir="$(go env GOMODCACHE)/github.com/wailsapp/wails/v3@${wails_version}"
gtk_plugin_source="${module_dir}/internal/commands/linuxdeploy-plugin-gtk.sh"

if [ ! -f "$gtk_plugin_source" ]; then
  echo "Unable to locate Wails GTK plugin: $gtk_plugin_source" >&2
  exit 1
fi

go_arch="${ARCH:-$(go env GOARCH)}"
case "$go_arch" in
  amd64|x86_64) appimage_arch="x86_64" ;;
  arm64|aarch64) appimage_arch="aarch64" ;;
  *)
    echo "Unsupported AppImage architecture: $go_arch" >&2
    exit 1
    ;;
esac

binary_name="$(basename "$binary")"
normalised_name="$(printf '%s' "$binary_name" | tr '[:upper:] ' '[:lower:]-')"
app_dir="${build_dir}/${normalised_name}-${appimage_arch}.AppDir"
linuxdeploy="${build_dir}/linuxdeploy-${appimage_arch}.AppImage"
app_run="${app_dir}/AppRun"
gtk_plugin="${build_dir}/linuxdeploy-plugin-gtk.sh"

download() {
  local url="$1"
  local target="$2"

  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --retry 3 --output "$target" "$url"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -q -O "$target" "$url"
    return
  fi

  echo "Neither curl nor wget is available to download $url" >&2
  exit 1
}

copy_abs_to_appdir() {
  local source="$1"
  local target="${app_dir}/${source#/}"

  mkdir -p "$(dirname "$target")"
  cp -a "$source" "$target"
}

copy_linked_webkit_helpers() {
  local ldd_output
  local webkit_dir

  ldd_output="$(ldd "$binary")"
  case "$ldd_output" in
    *libwebkit2gtk-4.1.so*) webkit_dir="/usr/lib/webkit2gtk-4.1" ;;
    *libwebkit2gtk-4.0.so*) webkit_dir="/usr/lib/webkit2gtk-4.0" ;;
    *libwebkitgtk-6.0.so*) webkit_dir="/usr/lib/webkitgtk-6.0" ;;
    *)
      echo "Unable to determine linked WebKitGTK runtime from $binary" >&2
      exit 1
      ;;
  esac

  for file in \
    "${webkit_dir}/WebKitWebProcess" \
    "${webkit_dir}/WebKitNetworkProcess" \
    "${webkit_dir}/WebKitGPUProcess" \
    "${webkit_dir}/injected-bundle/libwebkit2gtkinjectedbundle.so" \
    "${webkit_dir}/injected-bundle/libwebkitgtkinjectedbundle.so" \
    "${webkit_dir}/injected-bundle/libwebkitgtk-web-process-extension.so"; do
    if [ -f "$file" ]; then
      copy_abs_to_appdir "$file"
    fi
  done
}

patch_gtk_plugin() {
  local patched_plugin

  patched_plugin="$(mktemp "${gtk_plugin}.XXXXXX")"
  cp "$gtk_plugin_source" "$patched_plugin"
  chmod u+w "$patched_plugin"
  python3 - "$patched_plugin" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()

copy_tree = '''copy_lib_tree() {
    # The source lib directory could be /usr/lib, /usr/lib64, or /usr/lib/x86_64-linux-gnu
    # Therefore, when copying lib directories, we need to transform that target path
    # to a consistent /usr/lib
    local src=("${@:1:$#-1}")
    local dst="${*:$#}"

    for elem in "${src[@]}"; do
        mkdir -p "${dst::-1}${elem/$LD_GTK_LIBRARY_PATH//usr/lib}"
        pushd "$LD_GTK_LIBRARY_PATH"
        cp "$(realpath --relative-to="$LD_GTK_LIBRARY_PATH" "$elem")" --archive --parents --target-directory="$dst/usr/lib" $verbose
        popd
    done
}
'''

copy_tree_patched = copy_tree + '''
copy_gtk_tree() {
    local src=("${@:1:$#-1}")
    local dst="${*:$#}"

    for elem in "${src[@]}"; do
        case "$elem" in
            "$LD_GTK_LIBRARY_PATH"/*)
                copy_lib_tree "$elem" "$dst"
                ;;
            *)
                copy_tree "$elem" "$dst"
                ;;
        esac
    done
}
'''

if copy_tree not in text:
    raise SystemExit("Unable to patch linuxdeploy-plugin-gtk.sh: copy_lib_tree block not found")

text = text.replace(copy_tree, copy_tree_patched)
for variable in ("gi_typelibsdir", "gtk3_libdir", "gtk4_libdir", "gdk_pixbuf_binarydir"):
    text = text.replace(
        f'copy_lib_tree "${variable}" "$APPDIR/"',
        f'copy_gtk_tree "${variable}" "$APPDIR/"',
    )

text = text.replace(
    'find "$directory" \\( -type l -o -type f \\) -name "$library" -print0',
    'find "$directory" -maxdepth 1 \\( -type l -o -type f \\) -name "$library" -print0',
)
text = text.replace(
    '''for directory in "${PATCH_ARRAY[@]}"; do
    while IFS= read -r -d '' file; do
        ln $verbose -sf "${file/$LD_GTK_LIBRARY_PATH\\//}" "$APPDIR/usr/lib"
    done < <(find "$directory" -name '*.so' -print0)
done''',
    '''for directory in "${PATCH_ARRAY[@]}"; do
    if [ ! -d "$directory" ]; then
        continue
    fi

    while IFS= read -r -d '' file; do
        ln $verbose -sf "${file/$LD_GTK_LIBRARY_PATH\\//}" "$APPDIR/usr/lib"
    done < <(find "$directory" -name '*.so' -print0)
done''',
)

path.write_text(text)
PY
  mv -f "$patched_plugin" "$gtk_plugin"
  chmod +x "$gtk_plugin"
}

has_relr_dyn_sections() {
  local lib
  for lib in \
    /usr/lib/libgtk-4.so.1 \
    /usr/lib64/libgtk-4.so.1 \
    /usr/lib/x86_64-linux-gnu/libgtk-4.so.1 \
    /usr/lib/aarch64-linux-gnu/libgtk-4.so.1 \
    /usr/lib/libgtk-3.so.0 \
    /usr/lib64/libgtk-3.so.0 \
    /usr/lib/x86_64-linux-gnu/libgtk-3.so.0 \
    /usr/lib/aarch64-linux-gnu/libgtk-3.so.0; do
    if [ -f "$lib" ] && readelf -S "$lib" 2>/dev/null | grep -q ".relr.dyn"; then
      return 0
    fi
  done

  return 1
}

remove_existing_app_dir() {
  case "$app_dir" in
    ""|"/"|"."|"..")
      echo "Refusing to remove unsafe AppDir path: $app_dir" >&2
      exit 1
      ;;
  esac

  if [ -e "$app_dir" ]; then
    find "$app_dir" -mindepth 1 -delete
    rmdir "$app_dir"
  fi
}

remove_existing_app_dir
mkdir -p "${app_dir}/usr/bin" "$build_dir" "$output_dir"

cp "$binary" "${app_dir}/usr/bin/${binary_name}"
chmod +x "${app_dir}/usr/bin/${binary_name}"
cp "$icon" "${app_dir}/.DirIcon"
ln -sf ".DirIcon" "${app_dir}/$(basename "$icon")"
cp "$desktop_file" "$app_dir/"

if [ ! -f "$linuxdeploy" ]; then
  download "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-${appimage_arch}.AppImage" "$linuxdeploy"
fi
chmod +x "$linuxdeploy"

if [ ! -f "$app_run" ]; then
  download "https://github.com/AppImage/AppImageKit/releases/download/continuous/AppRun-${appimage_arch}" "$app_run"
fi
chmod +x "$app_run"

copy_linked_webkit_helpers
patch_gtk_plugin

case "$(ldd "$binary")" in
  *libgtk-3.so*) deploy_gtk_version="3" ;;
  *libgtk-4.so*) deploy_gtk_version="4" ;;
  *libgtk-x11-2.0.so*) deploy_gtk_version="2" ;;
  *)
    echo "Unable to determine linked GTK runtime from $binary" >&2
    exit 1
    ;;
esac

(
  cd "$build_dir"
  appimage_name="${normalised_name}-${appimage_arch}.AppImage"
  export DEPLOY_GTK_VERSION="$deploy_gtk_version"
  export OUTPUT="$appimage_name"
  if has_relr_dyn_sections; then
    export NO_STRIP=1
    echo "Detected modern toolchain (.relr.dyn sections), disabling linuxdeploy stripping."
  fi

  "$linuxdeploy" --appimage-extract-and-run --appdir "$app_dir" --output appimage --plugin gtk
)

generated="${build_dir}/${normalised_name}-${appimage_arch}.AppImage"
if [ ! -f "$generated" ]; then
  echo "Expected AppImage was not generated: $generated" >&2
  exit 1
fi

mv -f "$generated" "$output_dir/"
echo "AppImage created: ${output_dir}/${normalised_name}-${appimage_arch}.AppImage"
