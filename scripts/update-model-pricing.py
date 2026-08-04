#!/usr/bin/env python3
"""Download and validate the LiteLLM model pricing catalog for release builds."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import tempfile
import urllib.request
from pathlib import Path
from typing import Any


SOURCE_URL = (
    "https://raw.githubusercontent.com/BerriAI/litellm/main/"
    "model_prices_and_context_window.json"
)
MAX_BYTES = 16 * 1024 * 1024
DOWNLOAD_TIMEOUT_SECONDS = 120
MINIMUM_MODELS = 1000
MINIMUM_RETAIN_RATIO = 0.70
RESERVED_KEYS = {"sample_spec", "fallback_generalizations"}


class PricingValidationError(ValueError):
    """Raised when the downloaded catalog is not safe to embed."""


def reject_nonstandard_number(value: str) -> Any:
    raise PricingValidationError(f"non-finite JSON number is not allowed: {value}")


def reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise PricingValidationError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def load_catalog(data: bytes) -> dict[str, Any]:
    try:
        value = json.loads(
            data,
            object_pairs_hook=reject_duplicate_keys,
            parse_constant=reject_nonstandard_number,
        )
    except (json.JSONDecodeError, PricingValidationError) as error:
        raise PricingValidationError(f"invalid pricing JSON: {error}") from error
    if not isinstance(value, dict):
        raise PricingValidationError("pricing catalog root must be a JSON object")
    return value


def validate_cost_values(value: Any, path: str) -> None:
    pending: list[tuple[Any, str]] = [(value, path)]
    while pending:
        current, current_path = pending.pop()
        if isinstance(current, dict):
            for key, child in current.items():
                child_path = f"{current_path}.{key}"
                if "cost" in key.lower() and isinstance(child, (int, float)) and child < 0:
                    raise PricingValidationError(f"negative cost at {child_path}")
                if isinstance(child, (dict, list)):
                    pending.append((child, child_path))
        elif isinstance(current, list):
            for index, child in enumerate(current):
                if isinstance(child, (dict, list)):
                    pending.append((child, f"{current_path}[{index}]"))


def validate_case_duplicates(catalog: dict[str, Any]) -> int:
    """Validate model keys that differ only by letter case.

    LiteLLM has historically published an occasional case-only duplicate. Go's
    normalized lookup handles equal payloads deterministically, while conflicting
    payloads must fail rather than silently select a price.
    """

    groups: dict[str, list[str]] = {}
    for key in catalog:
        if key in RESERVED_KEYS:
            continue
        groups.setdefault(key.casefold(), []).append(key)

    duplicate_groups = 0
    for keys in groups.values():
        if len(keys) < 2:
            continue
        first_payload = catalog[keys[0]]
        if any(catalog[key] != first_payload for key in keys[1:]):
            raise PricingValidationError(
                "case-insensitive duplicate model keys have different payloads: "
                + ", ".join(sorted(keys))
            )
        duplicate_groups += 1
    return duplicate_groups


def validate_catalog(catalog: dict[str, Any]) -> int:
    model_count = 0
    for model, payload in catalog.items():
        if model in RESERVED_KEYS:
            continue
        if not model.strip():
            raise PricingValidationError("model key cannot be empty")
        if not isinstance(payload, dict):
            raise PricingValidationError(f"model {model!r} must contain a JSON object")
        validate_cost_values(payload, model)
        model_count += 1
    if model_count < MINIMUM_MODELS:
        raise PricingValidationError(
            f"pricing catalog contains only {model_count} models; minimum is {MINIMUM_MODELS}"
        )
    return model_count


def validate_overlay(output_path: Path, catalog: dict[str, Any]) -> None:
    overlay_path = output_path.with_name("model_prices_overlay.json")
    if not overlay_path.exists():
        return
    overlay = load_catalog(overlay_path.read_bytes())
    aliases = overlay.get("aliases")
    if not isinstance(aliases, dict):
        raise PricingValidationError("pricing overlay must contain an aliases object")
    for alias, target in aliases.items():
        if not isinstance(target, str) or target not in catalog:
            raise PricingValidationError(f"overlay alias {alias!r} points to missing model {target!r}")


def download_catalog(url: str) -> bytes:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/json",
            "User-Agent": "code-switch-R/model-pricing-release",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=DOWNLOAD_TIMEOUT_SECONDS) as response:
            status = getattr(response, "status", 200)
            if status not in (None, 200):
                raise PricingValidationError(f"pricing download returned HTTP {status}")
            chunks: list[bytes] = []
            size = 0
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_BYTES:
                    raise PricingValidationError(
                        f"pricing download exceeds {MAX_BYTES // (1024 * 1024)} MiB"
                    )
                chunks.append(chunk)
            return b"".join(chunks)
    except PricingValidationError:
        raise
    except Exception as error:
        raise PricingValidationError(f"pricing download failed: {error}") from error


def atomic_write(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary_path, path)
    except Exception:
        try:
            temporary_path.unlink()
        except FileNotFoundError:
            pass
        raise


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path, help="embedded pricing JSON path")
    parser.add_argument("--source-url", default=SOURCE_URL)
    arguments = parser.parse_args()

    downloaded = download_catalog(arguments.source_url)
    print(f"Downloaded pricing source: {len(downloaded)} bytes")
    catalog = load_catalog(downloaded)
    print(f"Parsed pricing source: {len(catalog)} top-level entries")
    duplicate_groups = validate_case_duplicates(catalog)
    if duplicate_groups:
        print(f"Found {duplicate_groups} equivalent case-insensitive model-key group(s)")
    model_count = validate_catalog(catalog)
    print(f"Validated pricing catalog: {model_count} models", flush=True)
    validate_overlay(arguments.output, catalog)

    if arguments.output.exists():
        previous_count = validate_catalog(load_catalog(arguments.output.read_bytes()))
        if previous_count > 0 and model_count < previous_count * MINIMUM_RETAIN_RATIO:
            raise PricingValidationError(
                f"pricing catalog shrank from {previous_count} to {model_count} models"
            )
        print(f"Compared with embedded catalog: {previous_count} models")

    atomic_write(arguments.output, downloaded)

    digest = hashlib.sha256(downloaded).hexdigest()
    print(f"Updated {arguments.output}: {model_count} models, sha256={digest}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
