import sys
from pathlib import Path

import pytest

# Make tests/fakes.py importable as `fakes`.
sys.path.insert(0, str(Path(__file__).parent))


@pytest.fixture(autouse=True)
def _isolated_home(tmp_path, monkeypatch):
    """Never touch the real Cubicle data folder (MCP server logs go to <home>/logs)."""
    monkeypatch.setenv("CUBICLE_HOME", str(tmp_path / "home"))
