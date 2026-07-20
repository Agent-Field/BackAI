# SPDX-License-Identifier: Apache-2.0

"""Runtime-version compatibility policy (pure function).

Validation contract:
* A version inside the supported major range returns no warning.
* A version with a different major returns a warning message.
* Unknown / unparseable / missing versions are tolerated (no warning).
"""

from __future__ import annotations

from af_stack import check_runtime_compat
from af_stack._http import SUPPORTED_RUNTIME_MAJOR


def test_matching_major_is_ok() -> None:
    assert check_runtime_compat(f"{SUPPORTED_RUNTIME_MAJOR}.5.2") is None
    assert check_runtime_compat(f"v{SUPPORTED_RUNTIME_MAJOR}.0.0") is None


def test_major_mismatch_warns() -> None:
    msg = check_runtime_compat(f"{SUPPORTED_RUNTIME_MAJOR + 1}.0.0")
    assert msg is not None
    assert "upgrade the SDK" in msg


def test_unknown_version_is_tolerated() -> None:
    assert check_runtime_compat(None) is None
    assert check_runtime_compat("") is None
    assert check_runtime_compat("not-a-version") is None
