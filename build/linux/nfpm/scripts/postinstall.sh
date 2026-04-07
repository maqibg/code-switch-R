#!/bin/bash
set -e

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

# 更新图标缓存
if command -v gtk-update-icon-cache &> /dev/null; then
    gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
fi

# code-switch-R 已使用最终命令名，无需额外别名

echo "code-switch-R 安装完成"
