#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
linux_taskfile="${repo_root}/build/linux/Taskfile.yml"
nfpm_config="${repo_root}/build/linux/nfpm/nfpm.yaml"

if [ ! -f "$linux_taskfile" ]; then
  echo "Missing Linux Taskfile: $linux_taskfile" >&2
  exit 1
fi

python3 - "$linux_taskfile" "$nfpm_config" <<'PY'
from pathlib import Path
import re
import sys

taskfile = Path(sys.argv[1])
nfpm = Path(sys.argv[2])

task_text = taskfile.read_text()
old_appimage_cmd = (
    "      - wails3 generate appimage -binary {{.APP_NAME}} -icon {{.ICON}} "
    "-desktopfile {{.DESKTOP_FILE}} -outputdir {{.OUTPUT_DIR}} "
    "-builddir {{.ROOT_DIR}}/build/linux/appimage/build"
)
legacy_patched_appimage_cmd = (
    "      - ../../../scripts/generate-linux-appimage.sh {{.APP_NAME}} {{.ICON}} "
    "{{.DESKTOP_FILE}} {{.OUTPUT_DIR}} {{.ROOT_DIR}}/build/linux/appimage/build"
)
legacy_sh_patched_appimage_cmd = (
    "      - sh ../../../scripts/generate-linux-appimage.sh {{.APP_NAME}} {{.ICON}} "
    "{{.DESKTOP_FILE}} {{.OUTPUT_DIR}} {{.ROOT_DIR}}/build/linux/appimage/build"
)
legacy_bash_patched_appimage_cmd = (
    "      - bash ../../../scripts/generate-linux-appimage.sh {{.APP_NAME}} {{.ICON}} "
    "{{.DESKTOP_FILE}} {{.OUTPUT_DIR}} {{.ROOT_DIR}}/build/linux/appimage/build"
)
new_appimage_cmd = (
    '      - bash "../../../scripts/generate-linux-appimage.sh" "{{.APP_NAME}}" "{{.ICON}}" '
    '"{{.DESKTOP_FILE}}" "{{.OUTPUT_DIR}}" "{{.ROOT_DIR}}/build/linux/appimage/build"'
)
old_build_flags = (
    "    vars:\n"
    "      BUILD_FLAGS: '{{if eq .PRODUCTION \"true\"}}-tags production -trimpath -buildvcs=false "
    "-ldflags=\"-w -s\"{{else}}-buildvcs=false -gcflags=all=\"-l\"{{end}}'"
)
gtk4_build_flags = (
    "    vars:\n"
    "      BUILD_FLAGS: '{{if eq .PRODUCTION \"true\"}}-tags production,gtk4 -trimpath -buildvcs=false "
    "-ldflags=\"-w -s\"{{else}}-tags gtk4 -buildvcs=false -gcflags=all=\"-l\"{{end}}'"
)

if old_appimage_cmd in task_text:
    task_text = task_text.replace(old_appimage_cmd, new_appimage_cmd)
elif legacy_patched_appimage_cmd in task_text:
    task_text = task_text.replace(legacy_patched_appimage_cmd, new_appimage_cmd)
elif legacy_sh_patched_appimage_cmd in task_text:
    task_text = task_text.replace(legacy_sh_patched_appimage_cmd, new_appimage_cmd)
elif legacy_bash_patched_appimage_cmd in task_text:
    task_text = task_text.replace(legacy_bash_patched_appimage_cmd, new_appimage_cmd)
elif new_appimage_cmd not in task_text:
    raise SystemExit("Unable to patch build/linux/Taskfile.yml: AppImage command not found")

if old_build_flags in task_text:
    task_text = task_text.replace(old_build_flags, gtk4_build_flags)
elif gtk4_build_flags not in task_text:
    raise SystemExit("Unable to patch build/linux/Taskfile.yml: Linux GTK4 build flags not found")

taskfile.write_text(task_text)

if not nfpm.exists():
    raise SystemExit(f"Missing nfpm config: {nfpm}")

nfpm_text = nfpm.read_text()
dependencies_block = """# Default dependencies for the GTK4 + WebKitGTK 6.0 stack (Ubuntu 24.04+ / Debian 13+)
depends:
  - libgtk-4-1
  - libwebkitgtk-6.0-4

# Distribution-specific overrides for different package formats
overrides:
  # RPM packages for Fedora / RHEL / AlmaLinux / Rocky Linux
  rpm:
    depends:
      - gtk4
      - webkitgtk6.0

  # Arch Linux packages
  archlinux:
    depends:
      - gtk4
      - webkitgtk-6.0

"""

pattern = re.compile(
    r"# Default dependencies.*?\n"
    r"depends:\n"
    r"(?:  - .*\n)+\n"
    r"# Distribution-specific overrides.*?\n"
    r"overrides:\n"
    r".*?\n(?=# scripts section)",
    re.DOTALL,
)

nfpm_text, count = pattern.subn(dependencies_block, nfpm_text, count=1)
if count != 1:
    raise SystemExit("Unable to patch build/linux/nfpm/nfpm.yaml: dependency block not found")

nfpm_text = re.sub(
    r"\n# If you build your app with -tags gtk3.*?\n(?=# replaces:)",
    "\n",
    nfpm_text,
    count=1,
    flags=re.DOTALL,
)

nfpm.write_text(nfpm_text)
PY
