#!/bin/bash
set -e

# 移除自启动配置（如果存在）
AUTOSTART="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/code-switch-R.desktop"
rm -f "$AUTOSTART" 2>/dev/null || true

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

echo "code-switch-R 已卸载，程序目录下的 .code-switch-R 数据会保留"
