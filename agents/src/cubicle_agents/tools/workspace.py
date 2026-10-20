"""Sandboxed file tools.

Each assignment gets its own folder under <home>/workspace/<assignment id>/.
Employees can read, write and list files only inside that folder, so the
owner finds every deliverable in one place and nothing else on disk is reachable.
"""

from __future__ import annotations

from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Any

from cubicle_agents.tools.base import Tool, ToolError, require_str

MAX_WRITE_BYTES = 1_000_000
MAX_READ_CHARS = 100_000
MAX_FILES = 200


class Workspace:
    def __init__(self, root: Path) -> None:
        self.root = root

    def resolve(self, relative: str) -> Path:
        """Map a model-supplied relative path into the workspace, rejecting escapes."""
        rel = relative.strip().replace("\\", "/")
        if not rel or PurePosixPath(rel).is_absolute() or PureWindowsPath(relative).drive:
            raise ToolError("Use a relative path inside the workspace, e.g. 'report.md' or 'drafts/email.txt'.")
        if any(part == ".." for part in PurePosixPath(rel).parts):
            raise ToolError("Paths may not contain '..'.")
        root = self.root.resolve()
        target = (root / rel).resolve()
        if not target.is_relative_to(root) or target == root:
            raise ToolError("That path is outside the workspace.")
        return target

    def files(self) -> list[str]:
        if not self.root.exists():
            return []
        root = self.root.resolve()
        return sorted(p.relative_to(root).as_posix() for p in root.rglob("*") if p.is_file())

    async def write_file(self, args: dict[str, Any]) -> str:
        path = self.resolve(require_str(args, "path", max_len=200))
        content = args.get("content")
        if not isinstance(content, str):
            raise ToolError("'content' must be a string.")
        data = content.encode("utf-8")
        if len(data) > MAX_WRITE_BYTES:
            raise ToolError(f"File too large (max {MAX_WRITE_BYTES // 1000} KB).")
        if not path.exists() and len(self.files()) >= MAX_FILES:
            raise ToolError(f"The workspace already holds {MAX_FILES} files.")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return f"Saved {path.relative_to(self.root.resolve()).as_posix()} ({len(data):,} bytes)."

    async def read_file(self, args: dict[str, Any]) -> str:
        path = self.resolve(require_str(args, "path", max_len=200))
        if not path.is_file():
            raise ToolError("No such file. Use list_files to see what exists.")
        text = path.read_text(encoding="utf-8", errors="replace")
        if len(text) > MAX_READ_CHARS:
            return text[:MAX_READ_CHARS] + "\n[truncated]"
        return text

    async def list_files(self, _: dict[str, Any]) -> str:
        files = self.files()
        return "\n".join(files) if files else "The workspace is empty."

    def tools(self) -> list[Tool]:
        path_prop = {"type": "string", "description": "Relative path inside the workspace, e.g. 'report.md'."}
        return [
            Tool(
                name="write_file",
                description="Save a text file (Markdown, CSV, code, ...) to this assignment's workspace, where the "
                "owner can open it. Overwrites an existing file with the same path.",
                parameters={
                    "type": "object",
                    "properties": {"path": path_prop, "content": {"type": "string"}},
                    "required": ["path", "content"],
                    "additionalProperties": False,
                },
                handler=self.write_file,
            ),
            Tool(
                name="read_file",
                description="Read a text file from this assignment's workspace, for example a teammate's draft.",
                parameters={
                    "type": "object",
                    "properties": {"path": path_prop},
                    "required": ["path"],
                    "additionalProperties": False,
                },
                handler=self.read_file,
            ),
            Tool(
                name="list_files",
                description="List the files in this assignment's workspace.",
                parameters={"type": "object", "properties": {}, "additionalProperties": False},
                handler=self.list_files,
            ),
        ]
