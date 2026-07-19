#!/usr/bin/env bash
# hack/docaudit/inventory.sh [repo-root]
# Emits one JSON object per planning-pattern hit in source comments and
# scripts, excluding allowlisted paths. Read-only; used by the audit, the
# classification fan-out, and (report-only) the recurrence guard.
set -euo pipefail
root="${1:-.}"
here="$(cd "$(dirname "$0")" && pwd)"
cd "$root"

pat="$(grep -v '^#' "$here/patterns.txt" | grep -v '^$' | paste -sd'|' -)"
allow="$(grep -v '^#' "$here/allowlist-paths.txt" | grep -v '^$' | paste -sd'|' -)"

git ls-files -- '*.go' '*.sh' '*.yaml' '*.yml' '*.mjs' '*.astro' '*.ts' '*.md' 'Makefile' \
  | grep -Ev "$allow" \
  | while IFS= read -r f; do
      # `|| true`: under `set -e -o pipefail` a no-match grep (exit 1) would
      # otherwise abort the whole scan at the first clean file.
      { grep -nE "$pat" "$f" 2>/dev/null || true; } | while IFS=: read -r ln text; do
        matched="$(printf '%s' "$text" | grep -oE "$pat" | head -1 || true)"
        python3 - "$f" "$ln" "$text" "$matched" <<'PY'
import json,sys
print(json.dumps({"file":sys.argv[1],"line":int(sys.argv[2]),
                  "text":sys.argv[3][:400],"pattern":sys.argv[4]}))
PY
      done
    done
