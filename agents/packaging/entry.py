"""PyInstaller entry point for the bundled `cubicle-agents.exe` (see scripts/build.ps1)."""

from cubicle_agents.cli import main

if __name__ == "__main__":
    raise SystemExit(main())
