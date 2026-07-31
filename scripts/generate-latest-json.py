#!/usr/bin/env python3
"""
生成 latest.json 更新清单

用于 GitHub Actions CI，根据 release-assets 目录中的文件生成 latest.json。
"""

import json
import hashlib
import os
import sys
from datetime import datetime, timezone
from pathlib import Path


def compute_sha256(filepath: Path) -> str:
    """计算文件的 SHA256 哈希值"""
    sha256_hash = hashlib.sha256()
    with open(filepath, "rb") as f:
        for byte_block in iter(lambda: f.read(4096), b""):
            sha256_hash.update(byte_block)
    return sha256_hash.hexdigest()


def get_file_size(filepath: Path) -> int:
    """获取文件大小（字节）"""
    return filepath.stat().st_size


def find_asset(assets_dir: Path, patterns: list[str]) -> Path | None:
    """在目录中查找匹配模式的文件"""
    for pattern in patterns:
        matches = list(assets_dir.glob(pattern))
        if matches:
            return matches[0]
    return None


def main():
    if len(sys.argv) < 3:
        print("Usage: generate-latest-json.py <version> <assets_dir> [output_file]")
        print("  version: Version string (e.g., v2.6.23)")
        print("  assets_dir: Directory containing release assets")
        print("  output_file: Output file path (default: latest.json)")
        sys.exit(1)

    version = sys.argv[1]
    assets_dir = Path(sys.argv[2])
    output_file = Path(sys.argv[3]) if len(sys.argv) > 3 else Path("latest.json")

    if not assets_dir.exists():
        print(f"Error: Assets directory not found: {assets_dir}")
        sys.exit(1)

    # 去除版本号前缀 v
    version_clean = version.lstrip("v")

    # GitHub Actions 会注入当前 owner/repo，本地生成时回退到当前发布仓库。
    repository = os.environ.get("GITHUB_REPOSITORY", "maqibg/code-switch-R").strip("/")
    repository_parts = repository.split("/")
    if len(repository_parts) != 2 or not all(repository_parts):
        print(f"Error: Invalid GITHUB_REPOSITORY: {repository}")
        sys.exit(1)
    base_url = f"https://github.com/{repository}/releases/download/v{version_clean}"

    # 应用只发布 Windows amd64 便携版。
    platform_configs = {
        "windows-x86_64": {
            "patterns": ["codeSwitchR.exe"],
            "filename": "codeSwitchR.exe",
        },
    }

    platforms = {}

    for platform_key, config in platform_configs.items():
        asset_file = find_asset(assets_dir, config["patterns"])

        if asset_file:
            sha256 = compute_sha256(asset_file)
            size = get_file_size(asset_file)

            # 首先检查是否有对应的 .sha256 文件
            sha256_file = asset_file.with_suffix(asset_file.suffix + ".sha256")
            if sha256_file.exists():
                # 从 .sha256 文件读取校验和（格式：hash  filename）
                content = sha256_file.read_text().strip()
                if content:
                    file_sha256 = content.split()[0]
                    # 验证计算的 SHA256 与文件中的一致
                    if file_sha256.lower() != sha256.lower():
                        print(f"Warning: SHA256 mismatch for {asset_file.name}")
                        print(f"  Computed: {sha256}")
                        print(f"  From file: {file_sha256}")

            platforms[platform_key] = {
                "url": f"{base_url}/{config['filename']}",
                "sha256": sha256,
                "size": size,
            }
            print(f"Found {platform_key}: {asset_file.name} ({size} bytes)")
        else:
            print(f"Error: Required asset not found for {platform_key}")
            sys.exit(1)

    # 读取 release notes（如果存在）
    notes = ""
    release_notes_file = Path("RELEASE_NOTES.md")
    if release_notes_file.exists():
        content = release_notes_file.read_text(encoding="utf-8")
        # 提取当前版本的 notes
        import re
        pattern = rf"# Code Switch v{version_clean}.*?(?=# Code Switch v|\Z)"
        match = re.search(pattern, content, re.DOTALL)
        if match:
            notes = match.group(0).strip()
            # 移除标题行
            lines = notes.split("\n")
            if lines and lines[0].startswith("# "):
                notes = "\n".join(lines[1:]).strip()

    # 构建 latest.json
    manifest = {
        "version": f"v{version_clean}",
        "pub_date": datetime.now(timezone.utc).isoformat(),
        "notes": notes,
        "platforms": platforms,
    }

    # 写入文件
    with open(output_file, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)

    print(f"\nGenerated {output_file}:")
    print(json.dumps(manifest, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
