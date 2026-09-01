# SPDX-License-Identifier: Apache-2.0

"""Cross-language parity: the BackAI client surface matches sdk-parity.json.

Validation contract:
* Every governed namespace in ``packages/sdk-parity.json`` exists on the
  explicit ``BackAI`` client.
* For each governed namespace the set of public callable methods equals the
  manifest's ``py`` names exactly — no missing methods, no extras.
* The set of governed namespaces on the client equals the manifest's set.

Because both SDKs' parity tests compare their own client against the SAME
manifest, a method added to only one language fails that language's test.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from af_stack import BackAI
from af_stack.client import GOVERNED_NAMESPACES

MANIFEST_PATH = Path(__file__).resolve().parents[2] / "sdk-parity.json"


def _load_manifest() -> dict:
    with MANIFEST_PATH.open(encoding="utf-8") as fh:
        return json.load(fh)


def _public_methods(namespace_obj: object) -> set[str]:
    return {
        name
        for name in vars(namespace_obj)
        if not name.startswith("_") and callable(getattr(namespace_obj, name))
    }


def test_manifest_exists_and_is_wellformed() -> None:
    manifest = _load_manifest()
    assert manifest["namespaces"], "manifest must declare namespaces"
    for ns, spec in manifest["namespaces"].items():
        assert spec["methods"], f"namespace {ns} has no methods"
        for method in spec["methods"]:
            assert {"name", "py", "ts"} <= set(method), method


def test_client_namespaces_match_manifest() -> None:
    manifest = _load_manifest()
    manifest_namespaces = set(manifest["namespaces"])
    assert set(GOVERNED_NAMESPACES) == manifest_namespaces


@pytest.mark.parametrize("client", [BackAI(check_runtime_version=False)])
def test_each_namespace_surface_matches_manifest(client: BackAI) -> None:
    manifest = _load_manifest()
    for ns, spec in manifest["namespaces"].items():
        namespace_obj = getattr(client, ns, None)
        assert namespace_obj is not None, f"client missing namespace {ns!r}"
        expected = {method["py"] for method in spec["methods"]}
        actual = _public_methods(namespace_obj)
        assert actual == expected, (
            f"namespace {ns!r} surface drift: missing={expected - actual} extra={actual - expected}"
        )


def test_bound_methods_preserve_signature() -> None:
    """A bound method keeps the underlying function's signature (introspection)."""
    import inspect

    from af_stack import agents

    client = BackAI(check_runtime_version=False)
    assert inspect.signature(client.agents.call) == inspect.signature(agents.call)
