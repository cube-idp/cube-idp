# Architecture — the living system map

This directory holds the living system map: **how cube-idp works NOW**, one
file per `area:*` label (`.github/labels.yml`). It is updated in the SAME PR
as the behavior change it describes (CLAUDE.md §3, ADR-0042 §Documentation
layout). ADRs record WHY (append-only); this map records HOW; the code is
the ground truth both point at.

Each area file starts empty — a MAP, not prose: markers plus an ADR index
and code entry points. Bodies are filled by the first behavior-changing PR
in that area. When you design new functionality, read the area file FIRST.

## Area markers

Every `docs/architecture/<area>.md` begins with a machine-readable header
comment; subsections may carry section markers:

    <!-- cube:doc area=packs code=internal/pack,internal/catalog adrs=0002,0003,0004,0005,0008 -->
    <!-- cube:section area=packs topic=fetching code=internal/pack/fetch adrs=0003 -->

Grammar: HTML comment · `cube:doc` | `cube:section` · space-separated
`key=value` pairs · comma-separated lists · `area` values must exist in
`.github/labels.yml`. CI validates header presence and area values; deep
content stays human-owned.

Keys:

- `area` — the `area:*` label this file/section maps to (required; must
  exist in `.github/labels.yml`).
- `code` — comma-separated entry points (package dirs / paths) implementing
  this area or topic; keep current as code moves.
- `adrs` — comma-separated ADR numbers governing this area or topic; keep
  current as decisions land or supersede.
- `topic` — (`cube:section` only) the subsection's subject.

## Navigation

An agent locates work by area, then follows the markers to code and
decisions:

    # find the area (or a specific topic within it)
    grep -rn 'cube:\(doc\|section\).*area=<area>' docs/architecture/

    # then follow the header's code= to entry points and adrs= to decisions
    grep -rn 'cube:section.*area=<area>' docs/architecture/

`code=` points at the implementing packages; `adrs=` points at the governing
decisions in `docs/adr/` (start at `docs/adr/README.md`). User-facing
contracts for an area live in `../reference/`.
