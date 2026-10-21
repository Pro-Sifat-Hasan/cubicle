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
MAX_READ_CHARS = 30_000
MAX_FILES = 200
MAX_MATCHES = 200
MAX_GREP_FILE_BYTES = 1_000_000
MAX_PATTERN_CHARS = 300
MAX_EDITS = 20
MAX_DIFF_LINES = 60


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
        start, count = args.get("start_line"), args.get("max_lines")
        if isinstance(start, int) or isinstance(count, int):
            lines = text.splitlines()
            first = max(start, 1) if isinstance(start, int) else 1
            last = min(first - 1 + (max(count, 1) if isinstance(count, int) else len(lines)), len(lines))
            text = "\n".join(f"{n}: {line}" for n, line in enumerate(lines[first - 1:last], first))
            text += f"\n[lines {first}-{last} of {len(lines)}]"
        if len(text) > MAX_READ_CHARS:
            return text[:MAX_READ_CHARS] + "\n[truncated: read the rest with start_line and max_lines]"
        return text

    async def list_files(self, _: dict[str, Any]) -> str:
        files = self.files()
        return "\n".join(files) if files else "The workspace is empty."

    async def edit_file(self, args: dict[str, Any]) -> str:
        """Change a file in place: one or several exact-text replacements, applied together or not at all."""
        path = self.resolve(require_str(args, "path", max_len=200))
        edits = args.get("edits")
        if edits is None:  # the single-edit form
            edits = [{"old_text": args.get("old_text"), "new_text": args.get("new_text"),
                      "replace_all": args.get("replace_all")}]
        if not isinstance(edits, list) or not 1 <= len(edits) <= MAX_EDITS:
            raise ToolError(f"'edits' must be a list of 1 to {MAX_EDITS} changes, each with old_text and new_text.")
        if not path.is_file():
            raise ToolError("No such file. Use find_files to locate it, or write_file to create it.")
        text, crlf = self._read_text(path)
        updated, notes = text, []
        for number, edit in enumerate(edits, 1):
            if not isinstance(edit, dict):
                raise ToolError(f"Edit {number}: each change needs old_text and new_text.")
            updated, note = self._apply_edit(updated, edit, number, len(edits))
            notes.append(note)
        return self._save_edit(path, text, updated, crlf, "; ".join(n for n in notes if n))

    async def insert_lines(self, args: dict[str, Any]) -> str:
        """Add new lines after a given line number (0 = at the top), without touching the rest of the file."""
        path = self.resolve(require_str(args, "path", max_len=200))
        after = args.get("after_line")
        new = args.get("text")
        if not isinstance(after, int) or isinstance(after, bool) or after < 0:
            raise ToolError("'after_line' must be a line number, or 0 to insert at the top.")
        if not isinstance(new, str) or not new:
            raise ToolError("'text' must be the lines to insert (not empty).")
        if not path.is_file():
            raise ToolError("No such file. Use write_file to create it.")
        text, crlf = self._read_text(path)
        lines = text.split("\n")
        total = len(lines) - 1 if lines and lines[-1] == "" else len(lines)
        if after > total:
            raise ToolError(f"The file has only {total} lines; 'after_line' cannot be {after}.")
        block = new.replace("\r\n", "\n").rstrip("\n").split("\n")
        lines[after:after] = block
        plural = "s" if len(block) != 1 else ""
        return self._save_edit(path, text, "\n".join(lines), crlf, f"inserted {len(block)} line{plural} after line {after}")

    # ---- editing helpers ----

    @staticmethod
    def _read_text(path: Path) -> tuple[str, bool]:
        """The file's text with \n line breaks, and whether it used \r\n (so the edit keeps the file's style)."""
        try:
            raw = path.read_bytes().decode("utf-8")
        except UnicodeDecodeError as exc:
            raise ToolError("That file is not UTF-8 text, so it cannot be edited here.") from exc
        return raw.replace("\r\n", "\n"), "\r\n" in raw

    def _apply_edit(self, text: str, edit: dict[str, Any], number: int, total: int) -> tuple[str, str]:
        label = f"Edit {number}: " if total > 1 else ""
        old, new = edit.get("old_text"), edit.get("new_text")
        if not isinstance(old, str) or not old.strip("\n"):
            raise ToolError(f"{label}'old_text' must be the exact text to replace (not empty).")
        if not isinstance(new, str):
            raise ToolError(f"{label}'new_text' must be a string (it may be empty to delete the text).")
        old, new = old.replace("\r\n", "\n"), new.replace("\r\n", "\n")
        if old == new:
            raise ToolError(f"{label}'old_text' and 'new_text' are identical, so nothing would change.")
        replace_all = edit.get("replace_all") is True
        count = text.count(old)
        if count == 1 or (count > 1 and replace_all):
            return text.replace(old, new), f"{label}replaced {count} occurrence{'s' if count != 1 else ''}"
        if count > 1:
            where = ", ".join(str(n) for n in self._line_numbers(text, old)[:6])
            raise ToolError(f"{label}'old_text' appears {count} times (at lines {where}). Include more surrounding "
                            "lines so it is unique, or set replace_all to true to change every one.")
        # Not found as written: the model often gets the indentation or trailing spaces slightly wrong.
        matched = self._match_ignoring_indentation(text, old, new)
        if matched is not None:
            return matched, f"{label}replaced 1 occurrence (matched ignoring indentation, which the file keeps)"
        raise ToolError(f"{label}'old_text' was not found. {self._closest(text, old)} Read the file again and copy "
                        "the lines exactly, including spaces and line breaks.")

    @staticmethod
    def _line_numbers(text: str, needle: str) -> list[int]:
        out, start = [], 0
        while (i := text.find(needle, start)) != -1:
            out.append(text.count("\n", 0, i) + 1)
            start = i + max(len(needle), 1)
        return out

    @staticmethod
    def _match_ignoring_indentation(text: str, old: str, new: str) -> str | None:
        """Replace a block that matches line for line apart from leading and trailing whitespace, if it is the only one."""
        wanted = [line.strip() for line in old.strip("\n").split("\n")]
        lines = text.split("\n")
        hits = [i for i in range(len(lines) - len(wanted) + 1)
                if all(lines[i + k].strip() == wanted[k] for k in range(len(wanted)))]
        if len(hits) != 1:
            return None
        first = hits[0]
        file_indent = lines[first][:len(lines[first]) - len(lines[first].lstrip())]
        old_first = old.strip("\n").split("\n")[0]
        old_indent = old_first[:len(old_first) - len(old_first.lstrip())]
        block = []
        for line in new.strip("\n").split("\n"):
            block.append(file_indent + line[len(old_indent):] if old_indent and line.startswith(old_indent)
                         else (file_indent + line if not old_indent and line.strip() else line))
        lines[first:first + len(wanted)] = block
        return "\n".join(lines)

    @staticmethod
    def _closest(text: str, old: str) -> str:
        """Point at the most similar line, so the next attempt can be exact."""
        import difflib

        first = next((line.strip() for line in old.split("\n") if line.strip()), "")
        lines = text.split("\n")
        close = difflib.get_close_matches(first, [line.strip() for line in lines], n=1, cutoff=0.6)
        if not close:
            return "No similar line exists in the file."
        number = next(i for i, line in enumerate(lines, 1) if line.strip() == close[0])
        return f"The most similar line is {number}: {close[0][:160]!r}."

    def _save_edit(self, path: Path, before: str, after: str, crlf: bool, summary: str) -> str:
        if after == before:
            raise ToolError("The change leaves the file exactly as it was.")
        data = (after.replace("\n", "\r\n") if crlf else after).encode("utf-8")
        if len(data) > MAX_WRITE_BYTES:
            raise ToolError(f"File too large (max {MAX_WRITE_BYTES // 1000} KB).")
        path.write_bytes(data)
        name = path.relative_to(self.root.resolve()).as_posix()
        out = [f"Edited {name}: {summary}.", self._diff(before, after)]
        if warning := self._syntax_warning(path, after):
            out.append(warning)
        return "\n".join(part for part in out if part)

    @staticmethod
    def _diff(before: str, after: str) -> str:
        """The change as a short unified diff with line numbers."""
        import difflib

        diff = list(difflib.unified_diff(before.split("\n"), after.split("\n"), "before", "after", n=2, lineterm=""))[2:]
        if len(diff) > MAX_DIFF_LINES:
            diff = diff[:MAX_DIFF_LINES] + [f"… {len(diff) - MAX_DIFF_LINES} more diff lines"]
        return "\n".join(diff)

    @staticmethod
    def _syntax_warning(path: Path, text: str) -> str:
        """After editing code or data, say at once if it no longer parses, instead of leaving a broken file behind."""
        suffix = path.suffix.lower()
        try:
            if suffix == ".py":
                import ast

                ast.parse(text)
            elif suffix == ".json":
                import json

                json.loads(text)
        except SyntaxError as exc:
            return f"WARNING: the file no longer parses as Python (line {exc.lineno}: {exc.msg}). Fix it before you go on."
        except ValueError as exc:
            return f"WARNING: the file is no longer valid JSON ({exc}). Fix it before you go on."
        return ""

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
                description="Read a text file from this assignment's workspace, for example a teammate's draft. Long "
                "files are cut; read a part with start_line and max_lines (lines are numbered).",
                parameters={
                    "type": "object",
                    "properties": {"path": path_prop, "start_line": {"type": "integer"}, "max_lines": {"type": "integer"}},
                    "required": ["path"],
                    "additionalProperties": False,
                },
                handler=self.read_file,
            ),
            Tool(
                name="edit_file",
                description="Change an existing file in place: the right way to modify code or text without "
                "rewriting it all. Read the file first, then give the exact text to replace (old_text) and what "
                "should replace it (new_text). Make old_text long enough to be unique, or set replace_all. To make "
                "several changes in one file, pass them all in 'edits': they are applied together or not at all. "
                "The result shows the diff; Python and JSON files are checked so you know at once if you broke one.",
                parameters={
                    "type": "object",
                    "properties": {
                        "path": path_prop,
                        "old_text": {"type": "string", "description": "The exact text to replace."},
                        "new_text": {"type": "string", "description": "What to put there (may be empty to delete)."},
                        "replace_all": {"type": "boolean", "description": "Replace every occurrence. Default false."},
                        "edits": {
                            "type": "array",
                            "description": f"Several changes to the same file (up to {MAX_EDITS}), applied in order. "
                            "Use this instead of old_text and new_text.",
                            "items": {
                                "type": "object",
                                "properties": {"old_text": {"type": "string"}, "new_text": {"type": "string"},
                                               "replace_all": {"type": "boolean"}},
                                "required": ["old_text", "new_text"],
                                "additionalProperties": False,
                            },
                        },
                    },
                    "required": ["path"],
                    "additionalProperties": False,
                },
                handler=self.edit_file,
            ),
            Tool(
                name="insert_lines",
                description="Add new lines to an existing file after a given line number (0 = at the top) without "
                "touching anything else. Use read_file with start_line to find the line number first.",
                parameters={
                    "type": "object",
                    "properties": {
                        "path": path_prop,
                        "after_line": {"type": "integer", "description": "Insert after this line; 0 for the top."},
                        "text": {"type": "string", "description": "The lines to insert."},
                    },
                    "required": ["path", "after_line", "text"],
                    "additionalProperties": False,
                },
                handler=self.insert_lines,
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
