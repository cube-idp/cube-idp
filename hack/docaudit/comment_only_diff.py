#!/usr/bin/env python3
"""Verify a git diff only changes Go comments.

Rules per changed .go file (unless --allow-file):
  - added/removed lines that are blank or whole-line comments: OK
  - paired +/- lines whose content BEFORE the first `//` is identical: OK
    (trailing-comment edits, e.g. codes.go constants)
  - anything else: violation.
Approximation is deliberate: `//` inside string literals is rare in this
codebase's comments and a false *positive* here just asks for human review.
"""
import argparse, re, subprocess, sys

def code_part(line: str) -> str:
    idx = line.find("//")
    return (line if idx < 0 else line[:idx]).rstrip()

def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--allow-file", action="append", default=[])
    ap.add_argument("base")
    a = ap.parse_args()
    diff = subprocess.run(["git", "diff", "-U0", a.base, "--", "*.go"],
                          capture_output=True, text=True, check=True).stdout
    violations, current, removed, added = [], None, [], []

    def flush():
        n = max(len(removed), len(added))
        for i in range(n):
            r = removed[i] if i < len(removed) else ""
            d = added[i] if i < len(added) else ""
            for ln in (r, d):
                s = ln.strip()
                if s == "" or s.startswith("//") or s.startswith("*") \
                   or s.startswith("/*") or s.endswith("*/"):
                    continue
                if code_part(r) == code_part(d) and code_part(d) != "":
                    continue  # trailing-comment edit, code identical
                violations.append(f"{current}: -{r!r} +{d!r}")
        removed.clear(); added.clear()

    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            flush(); current = line[6:]
        elif line.startswith("@@"):
            flush()
        elif current in a.allow_file:
            continue
        elif line.startswith("-") and not line.startswith("---"):
            removed.append(line[1:])
        elif line.startswith("+") and not line.startswith("+++"):
            added.append(line[1:])
    flush()
    for v in violations[:40]:
        print("NON-COMMENT CHANGE:", v)
    return 1 if violations else 0

if __name__ == "__main__":
    sys.exit(main())
