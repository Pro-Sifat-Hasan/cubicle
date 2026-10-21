"""Sandboxed file tools.

Each assignment gets its own folder under <home>/workspace/<assignment id>/.
Employees can read, write and list files only inside that folder, so the
owner finds every deliverable in one place and nothing else on disk is reachable.
"""

from __future__ import annotations

import fnmatch
import re
from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Any

from cubicle_agents.tools.base import Tool, ToolError, require_str

MAX_WRITE_BYTES = 1_000_000
MAX_READ_CHARS = 100_000
MAX_FILES = 200
MAX_MATCHES = 200
MAX_GREP_FILE_BYTES = 1_000_000
MAX_PATTERN_CHARS = 300


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

    async def edit_file(self, args: dict[str, Any]) -> str:
        """Replace exact text in a file: the safe way to change part of a file without rewriting all of it."""
        path = self.resolve(require_str(args, "path", max_len=200))
        old, new = args.get("old_text"), args.get("new_text")
        if not isinstance(old, str) or not old:
            raise ToolError("'old_text' must be the exact text to replace (not empty).")
        if not isinstance(new, str):
            raise ToolError("'new_text' must be a string (it may be empty to delete the text).")
        if not path.is_file():
            raise ToolError("No such file. Use find_files to locate it, or write_file to create it.")
        text = path.read_text(encoding="utf-8")
        count = text.count(old)
        if count == 0:
            raise ToolError("'old_text' was not found. It must match exactly, including spaces and line breaks; "
                            "read the file again and copy the text from it.")
        replace_all = args.get("replace_all") is True
        if count > 1 and not replace_all:
            raise ToolError(f"'old_text' appears {count} times. Include more surrounding text so it is unique, "
                            "or set replace_all to true.")
        updated = text.replace(old, new) if replace_all else text.replace(old, new, 1)
        data = updated.encode("utf-8")
        if len(data) > MAX_WRITE_BYTES:
            raise ToolError(f"File too large (max {MAX_WRITE_BYTES // 1000} KB).")
        path.write_bytes(data)
        done = count if replace_all else 1
        name = path.relative_to(self.root.resolve()).as_posix()
        return f"Edited {name}: replaced {done} occurrence{'s' if done != 1 else ''}."

    async def find_files(self, args: dict[str, Any]) -> str:
        """Find files by name pattern, such as '*.py' or 'src/**/*.ts'."""
        pattern = require_str(args, "pattern", max_len=200).strip().replace("\\", "/")
        if pattern.startswith("/") or ".." in PurePosixPath(pattern).parts:
            raise ToolError("Use a relative pattern such as '*.py' or 'src/**/*.ts'.")
        files = self.files()
        # fnmatch's '*' also crosses '/', so a bare '*.py' finds files in every folder, like most people expect.
        hits = [f for f in files if fnmatch.fnmatch(f, pattern) or fnmatch.fnmatch(f.rsplit("/", 1)[-1], pattern)
                or fnmatch.fnmatch(f, pattern.replace("**/", ""))]
        if not hits:
            return "No files match."
        extra = f"\n[{len(hits) - MAX_MATCHES} more not shown]" if len(hits) > MAX_MATCHES else ""
        return "\n".join(hits[:MAX_MATCHES]) + extra

    async def grep_files(self, args: dict[str, Any]) -> str:
        """Search file contents with a regular expression; returns 'file:line: text'."""
        raw = require_str(args, "pattern", max_len=MAX_PATTERN_CHARS)
        try:
            regex = re.compile(raw, re.IGNORECASE if args.get("ignore_case") is True else 0)
        except re.error as exc:
            raise ToolError(f"Invalid regular expression: {exc}.") from exc
        scope = args.get("file_pattern")
        scope = scope.strip() if isinstance(scope, str) and scope.strip() else ""
        out: list[str] = []
        root = self.root.resolve()
        for rel in self.files():
            if scope and not (fnmatch.fnmatch(rel, scope) or fnmatch.fnmatch(rel.rsplit("/", 1)[-1], scope)):
                continue
            file = root / rel
            if file.stat().st_size > MAX_GREP_FILE_BYTES:
                continue
            try:
                lines = file.read_text(encoding="utf-8").splitlines()
            except (UnicodeDecodeError, OSError):
                continue  # binary or unreadable: not searchable text
            for number, line in enumerate(lines, 1):
                if regex.search(line):
                    out.append(f"{rel}:{number}: {line.strip()[:240]}")
                    if len(out) >= MAX_MATCHES:
                        return "\n".join(out) + f"\n[stopped at {MAX_MATCHES} matches; narrow the pattern]"
        return "\n".join(out) if out else "No matches."

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
                name="edit_file",
                description="Change part of an existing file by replacing exact text. Prefer this to write_file for "
                "small changes: read the file first, copy the text to replace exactly, and make it unique.",
                parameters={
                    "type": "object",
                    "properties": {
                        "path": path_prop,
                        "old_text": {"type": "string", "description": "The exact text to replace."},
                        "new_text": {"type": "string", "description": "What to put there (may be empty)."},
                        "replace_all": {"type": "boolean", "description": "Replace every occurrence. Default false."},
                    },
                    "required": ["path", "old_text", "new_text"],
                    "additionalProperties": False,
                },
                handler=self.edit_file,
            ),
            Tool(
                name="find_files",
                description="Find files in the workspace by name pattern, e.g. '*.py' or 'src/**/*.ts'.",
                parameters={
                    "type": "object",
                    "properties": {"pattern": {"type": "string"}},
                    "required": ["pattern"],
                    "additionalProperties": False,
                },
                handler=self.find_files,
            ),
            Tool(
                name="grep_files",
                description="Search the text of workspace files with a regular expression. Returns file:line: text. "
                "Use file_pattern (e.g. '*.py') to limit the search.",
                parameters={
                    "type": "object",
                    "properties": {
                        "pattern": {"type": "string", "description": "A regular expression."},
                        "file_pattern": {"type": "string", "description": "Optional file name pattern, e.g. '*.py'."},
                        "ignore_case": {"type": "boolean"},
                    },
                    "required": ["pattern"],
                    "additionalProperties": False,
                },
                handler=self.grep_files,
            ),
            Tool(
                name="list_files",
                description="List the files in this assignment's workspace.",
                parameters={"type": "object", "properties": {}, "additionalProperties": False},
                handler=self.list_files,
            ),
        ]
