# SPDX-License-Identifier: Apache-2.0

"""Smoke test: package imports cleanly."""

import af_stack


def test_version_is_defined() -> None:
    assert isinstance(af_stack.__version__, str)
    assert af_stack.__version__ != ""