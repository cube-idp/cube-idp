# GitHub ADR-First Process + Subagent-Driven Development Rules — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install a binding, GitHub-native delivery process for the cube-idp org — ADR-first two-track intake, namespaced labels, milestones, issue forms, a CI process gate — plus committed rules and templates for subagent-driven development (SDD): a reusable dispatch prompt, a plan-ledger format, and a mandatory 10-minute visual status heartbeat.

**Architecture:** Decisions live as files in `docs/adr/` (source of truth, merged at acceptance); issues track delivery (epic + sub-issues); `CLAUDE.md` at repo root binds every agent session and absorbs the operational doctrine currently re-pasted into each dispatch prompt; `docs/process/` holds the three SDD templates the rules reference; one tiny GitHub Actions job converts the convention into a gate.

**Tech Stack:** GitHub Issues (sub-issues API), Labels, Milestones, Issue Forms (YAML), GitHub Actions, `gh` CLI, Markdown.

## Global Constraints

- `$ROOT` = `/Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/cube-idp/cube-idp` (primary checkout; main is clean except untracked `spokes-up.txt` — never add or commit it).
- **NEVER work in the main checkout.** All file changes happen in the isolated worktree `$ROOT/.claude/worktrees/process-0040-adr-first-sdd` on branch `process/0040-adr-first-sdd` (created at bootstrap — check for existence, reuse). Never commit to `main`. Pushing is limited to updating `process/0040-adr-first-sdd` to keep the tracking PR current; never push `main`, never push tags.
- **OUTWARD actions** (anything hitting github.com: label create/delete, milestone create, issue edit/create, PR open) are marked `[OUTWARD]` per task and require the dispatch to say `Outward actions authorized: yes`. Without it → report `NEEDS_CONTEXT`, do not improvise.
- Every commit message is the step's exact message and ends with the trailer:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Commit with explicit pathspecs only — never `git add -A` (known stray-staged-files gotcha on this machine).
- Next free ADR numbers: `0040` (process ADR) and `0041` (pilot). ADRs `0001–0039` live on branch `audit/phase-2-adrs` until T1 lands them on main.
- Verification is real commands with pasted output — never editor/LSP diagnostics.
- YAML validity gate used throughout: `python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1])); print('OK')" <file>`.

## Plan lifecycle (bootstrap → merge)

This plan is itself Track-A-shaped work and follows the process it installs:

1. **Bootstrap (done at plan creation):** worktree + branch `process/0040-adr-first-sdd`; this plan file committed there; tracking issue opened; PR opened referencing the issue (`Closes #<tracking>`). The PR stays open for the whole run — every task's commits update it.
2. **Execution:** tasks T1–T11 land as commits on the branch (worktree only), pushed to keep the PR current.
3. **Completion (T12):** all tasks DONE in the ledger → final verification → ADR-0040 flipped to `accepted` → owner merges the PR → the tracking issue closes automatically via the PR's `Closes` reference. Plan is complete only when the PR is merged AND the issue is closed.

Tracking issue: [#21](https://github.com/cube-idp/cube-idp/issues/21) · PR: [#22](https://github.com/cube-idp/cube-idp/pull/22).

## Phases (for the status heartbeat)

| Phase | Tasks | Deliverable |
| --- | --- | --- |
| 1 — Foundations | T1–T4 | ADRs on main, labels, milestone, issue forms |
| 2 — Decision & Rules | T5–T9 | ADR-0040, three SDD templates, CLAUDE.md |
| 3 — Enforcement | T10 | CI process gate |
| 4 — Pilot & Closeout | T11–T12 | Issue #7 through Track A, PR, owner checklist |

## Task Index & Ledger

Statuses: `UNCLAIMED` → `IN_PROGRESS(<session>, <UTC ts>)` → `DONE` / `DONE_WITH_CONCERNS` / `BLOCKED` / `NEEDS_CONTEXT`. Claim before code; close with evidence. (T8 formalizes this format for future plans; this plan eats its own dog food.)

| ID | Task | Depends | Outward? | STATUS |
| --- | --- | --- | --- | --- |
| T1 | Land `docs/adr/` 0001–0039 on main | — | no | UNCLAIMED · **OWNER-GATED** |
| T2 | Label taxonomy across org repos + relabel open issues | — | **yes** | UNCLAIMED |
| T3 | Milestone `v0.2.0` + assignments | T2 | **yes** | UNCLAIMED |
| T4 | Issue forms | T2 | no | UNCLAIMED |
| T5 | ADR-0040: the process ADR | T1 | no | UNCLAIMED |
| T6 | SDD dispatch prompt template | — | no | UNCLAIMED |
| T7 | SDD status heartbeat template | — | no | UNCLAIMED |
| T8 | SDD plan-ledger template | — | no | UNCLAIMED |
| T9 | `CLAUDE.md` + `AGENTS.md` (binding agent rules) | T5,T6,T7,T8 | no | UNCLAIMED |
| T10 | CI process gate workflow | — | no | UNCLAIMED |
| T11 | Pilot: issue #7 → ADR-0041 Track A | T2,T5,T9 | **yes** | UNCLAIMED |
| T12 | Open the PR + owner closeout checklist | all | **yes** | UNCLAIMED · **OWNER-GATED** (push) |

Per-task Outcome blocks live at the bottom of this file under "Ledger Outcomes".

---

### Task T1: Land `docs/adr/` (0001–0039) on main — OWNER-GATED

The 39 reconstructed ADRs sit on unmerged `audit/phase-2-adrs`. Unmerged decision records are invisible to every agent session on main — this is the process's hard dependency. **Owner gate:** the audit workstream (branches `audit/phase-1-oracle`, `audit/phase-2-adrs`, `audit/phase-3-comments`; the last is checked out in a worktree) may still be in flight. Claim this task only if the dispatch explicitly authorizes the merge; otherwise set `BLOCKED(owner-gate)` and continue with T2/T4/T6/T7/T8/T10, which do not depend on it.

**Files:**
- Merge into `main` (then branch `process/0040-adr-first-sdd` from the result): `docs/adr/0001-*.md` … `docs/adr/0039-*.md`, `docs/adr/README.md`

- [ ] **Step 1: Verify the audit branch is self-consistent**

```bash
cd $ROOT
git log --oneline -5 audit/phase-2-adrs
git ls-tree -r --name-only audit/phase-2-adrs -- docs/adr | wc -l   # expect 40 (39 ADRs + README.md)
git diff main...audit/phase-2-adrs --stat | tail -3
```
Expected: 40 files under `docs/adr/`; the diff also archives `docs/superpowers/` to `docs/archive/superpowers/` — that is part of the same audit commit set and merges with it.

- [ ] **Step 2: Check the not-yet-merged later audit phase does not conflict**

```bash
git merge-base --is-ancestor audit/phase-2-adrs audit/phase-3-comments && echo "phase-3 builds on phase-2 — safe to merge phase-2 first"
```
Expected: the echo line. If it fails, STOP → `BLOCKED`, report the branch topology; the owner decides merge order.

- [ ] **Step 3: Merge (owner-authorized only)**

```bash
git checkout main && git status --porcelain   # only ?? spokes-up.txt allowed
git merge --no-ff audit/phase-2-adrs -m "merge: docs audit phase 2 — ADRs 0001-0039 on main

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
ls docs/adr/ | head -5
```
Expected: merge commit created; `docs/adr/0001-adopt-architecture-decision-records.md` listed.

- [ ] **Step 4: Refresh the working branch** (worktree + branch already exist from bootstrap)

```bash
git -C $ROOT/.claude/worktrees/process-0040-adr-first-sdd merge --no-ff main \
  -m "merge: main (ADRs 0001-0039) into process branch

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
ls $ROOT/.claude/worktrees/process-0040-adr-first-sdd/docs/adr/ | head -3
```
Expected: `docs/adr/0001-…` visible in the worktree.

If T1 is BLOCKED on the owner gate, skip this step — T5 then targets `docs/adr/0040-…` as the directory's first file on the branch and notes the pending 0001–0039 merge in the ADR's index step.

---

### Task T2: Label taxonomy — org repos + relabel the 16 open issues `[OUTWARD]`

Replace GitHub default labels with a namespaced taxonomy (`type:`, `area:`, `status:`), applied to all five org repos, then migrate the open issues in `cube-idp/cube-idp`.

**Interfaces:**
- Produces: label names used verbatim by T4 (issue forms `labels:` keys), T9 (CLAUDE.md rules), T11 (pilot commands).

- [ ] **Step 1: Create the taxonomy in all five repos**

```bash
set -e
for R in cube-idp/cube-idp cube-idp/cube-idp-web cube-idp/packs cube-idp/plugins cube-idp/go-getter; do
  # type: —— what kind of work
  gh label create "type:bug"      -R $R --color d73a4a --description "Defect: shipped behavior is wrong" --force
  gh label create "type:feature"  -R $R --color a2eeef --description "New capability or enhancement" --force
  gh label create "type:chore"    -R $R --color ededed --description "Build, CI, tooling, refactor — no user-facing change" --force
  gh label create "type:docs"     -R $R --color 0075ca --description "Documentation only" --force
  gh label create "type:adr"      -R $R --color 5319e7 --description "Epic tracking an ADR from proposal to delivered" --force
  gh label create "type:spike"    -R $R --color fbca04 --description "Timeboxed exploration — must end in an ADR PR or close-with-reason" --force
  gh label create "type:question" -R $R --color d876e3 --description "Decision or information requested" --force
  # status: —— minimal; assignment+milestone models in-progress, not labels
  gh label create "status:triage"    -R $R --color f9d0c4 --description "Untriaged — needs type/area/milestone" --force
  gh label create "status:blocked"   -R $R --color b60205 --description "Cannot proceed — blocker named in body" --force
  gh label create "status:needs-adr" -R $R --color c5def5 --description "Waits on an architectural decision (Track A) before work starts" --force
done
# area: —— mirrors the ADR domains; main repo only (others inherit later if needed)
R=cube-idp/cube-idp
gh label create "area:cluster"     -R $R --color 1d76db --description "Providers (kind/k3d/existing), provider config, nodes/ports/mounts" --force
gh label create "area:packs"       -R $R --color 1d76db --description "Pack format, refs, fetching, deps, catalog, distribution" --force
gh label create "area:engine"      -R $R --color 1d76db --description "GitOps engines (flux/argocd), engine seam, engine-as-pack" --force
gh label create "area:registry"    -R $R --color 1d76db --description "In-cluster zot registry, artifact transport" --force
gh label create "area:gateway"     -R $R --color 1d76db --description "Gateway, routing, TLS, CA, DNS" --force
gh label create "area:tui-output"  -R $R --color 1d76db --description "Renderers, progress, TUI, machine-readable output" --force
gh label create "area:diagnostics" -R $R --color 1d76db --description "CUBE-xxxx diagnostics, doctor, error surfaces" --force
gh label create "area:trust"       -R $R --color 1d76db --description "Plugin trust, provenance, integrity, air-gap" --force
gh label create "area:ci"          -R $R --color 1d76db --description "GitHub Actions, e2e harness, release pipeline" --force
```
Expected: each line prints `✓ Label "…" created` (or updated, with `--force`).

- [ ] **Step 2: Relabel the 16 open issues (mapping table is normative)**

| Issue | Add | Remove |
| --- | --- | --- |
| #5 | `type:bug,area:cluster` | `bug` |
| #6 | `type:bug,area:cluster` | `bug` |
| #7 | `type:adr,area:cluster,status:needs-adr` | — |
| #8 | `type:feature,area:packs` | `enhancement` |
| #9 | `type:feature,area:gateway` | `enhancement` |
| #10 | `type:feature,area:registry` | `enhancement` |
| #11 | `type:docs` | `documentation` |
| #12 | `type:feature,area:packs` | `enhancement` |
| #13 | `type:chore,area:ci` | `enhancement` |
| #14 | `type:chore,area:ci` | `enhancement` |
| #15 | `type:bug,area:cluster` | — |
| #16 | `type:chore,status:needs-adr` | — |
| #17 | `type:question,area:packs,status:needs-adr` | — |
| #18 | `type:question,area:packs,status:needs-adr` | — |
| #19 | `type:question,area:packs,status:needs-adr` | — |
| #20 | `type:question,status:needs-adr` | — |
| #21 (this plan's tracking issue) | `type:adr,area:ci` | — |

```bash
R=cube-idp/cube-idp
gh issue edit 5  -R $R --add-label "type:bug,area:cluster" --remove-label "bug"
gh issue edit 6  -R $R --add-label "type:bug,area:cluster" --remove-label "bug"
gh issue edit 7  -R $R --add-label "type:adr,area:cluster,status:needs-adr"
gh issue edit 8  -R $R --add-label "type:feature,area:packs" --remove-label "enhancement"
gh issue edit 9  -R $R --add-label "type:feature,area:gateway" --remove-label "enhancement"
gh issue edit 10 -R $R --add-label "type:feature,area:registry" --remove-label "enhancement"
gh issue edit 11 -R $R --add-label "type:docs" --remove-label "documentation"
gh issue edit 12 -R $R --add-label "type:feature,area:packs" --remove-label "enhancement"
gh issue edit 13 -R $R --add-label "type:chore,area:ci" --remove-label "enhancement"
gh issue edit 14 -R $R --add-label "type:chore,area:ci" --remove-label "enhancement"
gh issue edit 15 -R $R --add-label "type:bug,area:cluster"
gh issue edit 16 -R $R --add-label "type:chore,status:needs-adr"
gh issue edit 17 -R $R --add-label "type:question,area:packs,status:needs-adr"
gh issue edit 18 -R $R --add-label "type:question,area:packs,status:needs-adr"
gh issue edit 19 -R $R --add-label "type:question,area:packs,status:needs-adr"
gh issue edit 20 -R $R --add-label "type:question,status:needs-adr"
```

- [ ] **Step 3: Retire replaced defaults in the main repo** (`duplicate`/`wontfix`/`invalid` go too — GitHub close-reasons replaced them; keep `good first issue` and `help wanted`, GitHub UI understands those)

```bash
for L in bug enhancement documentation question triage duplicate wontfix invalid; do
  gh label delete "$L" -R cube-idp/cube-idp --yes
done
```

- [ ] **Step 4: Verify**

```bash
gh label list -R cube-idp/cube-idp --limit 50 --json name -q '.[].name' | sort
gh issue list -R cube-idp/cube-idp --limit 30 --json number,labels -q '.[] | select((.labels|length)==0) | .number'
```
Expected: only `type:*` (7), `area:*` (9), `status:*` (3), `good first issue`, `help wanted`; second command prints nothing (no unlabeled open issues).

No commit — this task is GitHub-state only. Record label list output in the ledger Outcome as evidence.

---

### Task T3: Milestone `v0.2.0` + assignments `[OUTWARD]`

`v0.1.0` is tagged; the next deliverable batch gets a milestone. Unassigned = backlog by convention (no "backlog" milestone).

- [ ] **Step 1: Create the milestone**

```bash
gh api repos/cube-idp/cube-idp/milestones -f title="v0.2.0" \
  -f description="First post-0.1.0 batch: correctness fixes surfaced by the docs audit, CI hygiene, docs sweep." \
  --jq '.number'
```
Expected: prints the milestone number (likely `1`).

- [ ] **Step 2: Assign the starter set** (bugs + audit follow-ups + CI hygiene; feature issues stay backlog until an epic pulls them in)

```bash
for N in 5 6 15 11 14 16; do gh issue edit $N -R cube-idp/cube-idp --milestone "v0.2.0"; done
```

- [ ] **Step 3: Verify**

```bash
gh issue list -R cube-idp/cube-idp --milestone "v0.2.0" --json number -q '[.[].number] | sort | @csv'
```
Expected: `5,6,11,14,15,16`

---

### Task T4: Issue forms

**Files:**
- Create: `.github/ISSUE_TEMPLATE/config.yml`
- Create: `.github/ISSUE_TEMPLATE/bug.yml`
- Create: `.github/ISSUE_TEMPLATE/feature.yml`
- Create: `.github/ISSUE_TEMPLATE/epic.yml`
- Create: `.github/ISSUE_TEMPLATE/spike.yml`

**Interfaces:**
- Consumes: T2 label names verbatim.
- Note: forms gate the web UI only; `gh issue create` bypasses them — T9's CLAUDE.md §3 makes the same fields mandatory for agents.

- [ ] **Step 1: Write `config.yml`** (blank issues off — every issue picks a track)

```yaml
blank_issues_enabled: false
```

- [ ] **Step 2: Write `bug.yml`**

```yaml
name: Bug report
description: Shipped behavior is wrong
title: "bug: "
labels: ["type:bug", "status:triage"]
body:
  - type: textarea
    id: repro
    attributes:
      label: Reproduction
      description: Exact commands and cube.yaml (or minimal fragment). Paste output, not paraphrase.
      placeholder: |
        $ cube-idp up ...
        <actual output>
    validations:
      required: true
  - type: textarea
    id: expected
    attributes:
      label: Expected vs actual
    validations:
      required: true
  - type: input
    id: version
    attributes:
      label: Version
      description: "`cube-idp version` output or commit SHA"
    validations:
      required: true
  - type: dropdown
    id: area
    attributes:
      label: Area
      options: [cluster, packs, engine, registry, gateway, tui-output, diagnostics, trust, ci, unknown]
    validations:
      required: true
```

- [ ] **Step 3: Write `feature.yml`**

```yaml
name: Feature request
description: New capability or enhancement (Track B — or flags itself into Track A)
title: "feat: "
labels: ["type:feature", "status:triage"]
body:
  - type: textarea
    id: problem
    attributes:
      label: Problem
      description: What can't you do today? Why does it matter?
    validations:
      required: true
  - type: textarea
    id: proposal
    attributes:
      label: Proposal
      description: Sketch of the change. Closed scope — what is explicitly OUT.
    validations:
      required: true
  - type: dropdown
    id: needs-adr
    attributes:
      label: Does this need an ADR?
      description: New dependency, new architectural pattern, hard to reverse, or real competing alternatives → yes (Track A).
      options: ["no — routine work within existing decisions", "yes — architectural (an epic + ADR PR must precede code)", "unsure — triage decides"]
    validations:
      required: true
  - type: dropdown
    id: area
    attributes:
      label: Area
      options: [cluster, packs, engine, registry, gateway, tui-output, diagnostics, trust, ci, unknown]
    validations:
      required: true
```

- [ ] **Step 4: Write `epic.yml`** (Track A tracker)

```yaml
name: "Epic: ADR-tracked feature"
description: Track A — a decision plus its delivery, as one epic with sub-issues
title: "[ADR-NNNN] "
labels: ["type:adr"]
body:
  - type: input
    id: adr
    attributes:
      label: ADR
      description: Path once the ADR PR exists, e.g. docs/adr/0041-multinode-mounts-ports.md (PR link until merged)
    validations:
      required: true
  - type: textarea
    id: scope
    attributes:
      label: Scope
      description: One paragraph. What ships when this epic closes; what is explicitly out.
    validations:
      required: true
  - type: textarea
    id: subissues
    attributes:
      label: Delivery plan
      description: One line per intended sub-issue (converted to real sub-issues once the ADR is accepted).
      placeholder: |
        - [ ] mounts apply to all nodes by default
        - [ ] extraPorts semantics per provider
        - [ ] e2e coverage
    validations:
      required: true
  - type: input
    id: milestone
    attributes:
      label: Target milestone
    validations:
      required: true
```

- [ ] **Step 5: Write `spike.yml`**

```yaml
name: Spike (timeboxed exploration)
description: Allowed — but it must terminate in an ADR PR or close-with-reason
title: "spike: "
labels: ["type:spike"]
body:
  - type: textarea
    id: question
    attributes:
      label: Question to answer
    validations:
      required: true
  - type: input
    id: timebox
    attributes:
      label: Timebox
      description: e.g. "1 day", "4 hours". When it expires, the spike closes with its verdict.
    validations:
      required: true
  - type: dropdown
    id: exit
    attributes:
      label: Committed exit
      description: A spike may not end "open". Pick the exit now.
      options: ["ADR PR (Track A) or close-with-reason", "close-with-reason only (no decision expected)"]
    validations:
      required: true
```

- [ ] **Step 6: Validate and commit**

```bash
for F in .github/ISSUE_TEMPLATE/*.yml; do python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1])); print('OK', sys.argv[1])" "$F"; done
git add .github/ISSUE_TEMPLATE/
git commit -m "chore: issue forms — bug/feature/epic/spike, blank issues off

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
Expected: 5× `OK`; one commit.

---

### Task T5: ADR-0040 — the process ADR

The process itself is the first exercise of the process: recorded as an ADR, accepted when the PR merges.

**Files:**
- Create: `docs/adr/0040-adr-first-two-track-delivery-process.md`
- Modify: `docs/adr/README.md` (append index row; if T1 was blocked, create a minimal README with the 0040 row and a note that 0001–0039 land with the audit merge)

- [ ] **Step 1: Write the ADR** (full text; status `proposed` — flipped to `accepted` in T12 when the PR merges)

```markdown
# 0040 — ADR-First Two-Track Delivery Process on GitHub

Status: proposed
Date: 2026-07-20

## Context

The 2026-07-20 documentation audit validated the planning corpus (31 documents,
~236k words) against the code: 113 fully-specified decisions were never built,
69 were unverifiable, and 39 ADRs had to be reconstructed after the fact
(#16–#20). Design text lived on branches and in dated files main never saw;
nothing linked decisions to delivery. With AI-assisted coding the divergence
rate compounds: agents generate specs faster than humans notice they were
abandoned. The org stays on GitHub only — process, not new software.

## Decision

**Track A — decision-first** (features, architecture, anything hard to reverse):
1. Open an **epic issue** via the Epic form, titled `[ADR-NNNN] <name>`,
   labeled `type:adr`.
2. Open a **small PR adding `docs/adr/NNNN-<slug>.md`** (status `proposed`)
   with an Implementation Plan section (affected paths, patterns, tests,
   verification checkboxes). **PR review is the decision gate; merge =
   accepted.** The spec reaches main at acceptance time — before
   implementation, never on a long-lived side branch.
3. Create **sub-issues** under the epic, one per deliverable from the
   Implementation Plan. Each closes via a PR whose body carries
   `Closes #N` and `Implements ADR-NNNN`.
4. The epic closes when all sub-issues close and the ADR's verification
   checkboxes pass.

**Track B — routine** (bug/chore/docs): plain issue → PR with `Closes #N`.
Escalation guard: hitting a real architectural choice mid-implementation
stops the work and proposes an ADR (Track B → Track A).

**Spikes** are timeboxed and terminate in exactly one of: an ADR PR, or
closed-as-not-planned *with the reason in the closing comment*. Silent
abandonment is the failure mode this process exists to kill; a reasoned
"not doing X because Y" close is a first-class outcome.

**Taxonomy:** namespaced labels — `type:` (bug/feature/chore/docs/adr/spike/
question), `area:` (mirrors ADR domains), `status:` (triage/blocked/
needs-adr). Milestones are per-repo release buckets; unassigned = backlog.

**WIP rule:** before opening a new Track-A epic, list open epics in the
current milestone; an unfinished one must be justified as non-blocking in
the new epic's Scope field.

**Enforcement:** `CLAUDE.md` binds agent sessions (consult `docs/adr/`
before implementing in a governed area; propose an ADR on triggers; every
PR references an issue or ADR; no new design docs outside `docs/adr/`).
CI job `process-gate` rejects PRs whose body references neither `#N` nor
`ADR-NNNN`. Subagent-driven execution follows `docs/process/` templates.

## Non-Goals

- No org-level GitHub Project boards yet — revisit when cross-repo epics hurt.
- No retroactive re-issueing of shipped work; ADRs 0001–0039 already record it.
- Issue forms gate the web UI only; agent-side enforcement is CLAUDE.md's job.

## Consequences

- Every feature has a falsifiable paper trail: ADR → epic → sub-issues → PRs.
- Ceremony is bounded: Track B stays one-issue-one-PR light.
- `docs/superpowers/` is frozen as an archive; new plans attach to ADRs/epics.
- Follow-ups: #17–#20 must each get a Track-A revive or a reasoned close.

## Implementation Plan

- **Affected paths:** `.github/ISSUE_TEMPLATE/`, `.github/workflows/process-gate.yaml`,
  `CLAUDE.md`, `AGENTS.md`, `docs/process/`, `docs/adr/README.md`.
- **Installed by:** `docs/superpowers/plans/2026-07-20-github-process-and-sdd.md`
  (tasks T2–T11), which is also the first SDD run using the new templates.
- **Pattern for future Track-A work:** pilot ADR-0041 (issue #7).

## Verification

- [ ] `gh label list` shows only the namespaced taxonomy (+ community labels)
- [ ] Every open issue carries a `type:` label
- [ ] `process-gate` fails a PR with no `#N`/`ADR-NNNN` reference, passes one with
- [ ] Issue #7 retitled `[ADR-0041] …` with an accepted ADR and sub-issues
- [ ] `CLAUDE.md` present at repo root; agent session confirms it loads
```

- [ ] **Step 2: Append the index row to `docs/adr/README.md`**

```markdown
| 0040 | [ADR-First Two-Track Delivery Process on GitHub](0040-adr-first-two-track-delivery-process.md) | — |
```

- [ ] **Step 3: Commit**

```bash
git add docs/adr/0040-adr-first-two-track-delivery-process.md docs/adr/README.md
git commit -m "docs(adr): 0040 — ADR-first two-track delivery process (proposed)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T6: SDD dispatch prompt template

Generalizes the battle-tested `2026-07-19-engine-as-pack-agent-prompt.md` / `phase5-agent-prompt-v2.md` into a fill-in template. Repo-invariant doctrine moves OUT of the prompt into CLAUDE.md (T9); the template carries only per-plan facts. `{{DOUBLE_BRACES}}` = fill before dispatch; a filled prompt with unresolved braces is invalid.

**Files:**
- Create: `docs/process/sdd-dispatch-template.md`

- [ ] **Step 1: Write the template**

````markdown
# SDD dispatch prompt — {{PLAN_NAME}}

How to use: copy everything below the line into a fresh agent session to
execute exactly ONE task; re-paste for each next task. Fill every
{{PLACEHOLDER}}; delete optional sections that don't apply. Keep the
numbered structure — agents follow it in order. Authorization lines at the
bottom are per-dispatch and default to "no".

---

You are executing exactly ONE task of {{PLAN_NAME}}, then stopping. The
plan is NORMATIVE: you make no changes it does not specify. You do not
refactor, redesign, rename, "improve", or add scope. Where reality
contradicts the plan (an API name, a stale Expected line), use the plan's
escape hatch — verify against the real API/system, apply the minimal
correction, record it as a FINDINGS entry — never your own judgment beyond
that. On any unresolvable mismatch: STATUS: BLOCKED and stop.

Repos (absolute):
{{REPO_VARS e.g. $ROOT = /abs/path · $PACKS = /abs/path}}

0. RULES: $ROOT/CLAUDE.md binds this session — read it first. Its §SDD and
   §Operational-doctrine sections apply to every step below.

1. READ, in this order (this binds every step you take):
   - {{SPEC_PATH — mark RATIFIED sections binding}}
   - {{PLAN_PATH}} — Global Constraints, YOUR task's section, the Task
     Index & Ledger. {{BRANCH_NOTE if plan lives off-main}}
   - The ledger HANDOFF blocks of DONE tasks yours depends on — consume
     discovered values, never re-discover.

2. CURRENT STATE (verify, don't trust): {{STATE_SUMMARY — done/remaining}}.
   Cross-check ledger STATUS lines AND `git log --oneline -15` on the
   feature branch before claiming: if work already exists, do NOT redo it —
   close the ledger from the evidence. Default selection: first UNCLAIMED
   task whose dependencies are all DONE/DONE_WITH_CONCERNS
   {{SELECTION_ORDER if not simple task order}}. A Task id at the bottom
   overrides. {{GATED_TASKS — list OWNER-GATED / OUTWARD tasks}}.

3. WORKTREES/BRANCHES (create once, reuse — check for existence first):
   {{WORKTREE_CMDS one per repo, exact `git worktree add` with base branch}}
   NEVER work in a main checkout — every file you touch, code AND ledger,
   is edited inside the worktree on the task's branch. ALL commits land on
   the feature branch of their repo. Never commit to main. Never push ANY
   ref{{PUSH_EXCEPTIONS e.g. "except the plan's tracking branch to keep
   its PR current"}}.

4. CLAIM before any code: set ONLY your task's ledger STATUS to
   IN_PROGRESS(<session id>, <UTC ts>); commit with explicit pathspec:
   `git commit -m "docs: {{PLAN_SHORT}} — claim T<N>" -- {{PLAN_PATH}}`.
   Re-read the ledger immediately before editing; verify HEAD afterward.

5. EXECUTE the task's steps IN ORDER, TDD as written; every commit uses the
   step's exact message + the CLAUDE.md commit trailer.
   {{TASK_SPECIFIC_DOCTRINE — anything hard-won for THIS plan that
   CLAUDE.md §doctrine doesn't already cover; delete if none}}

6. STATUS HEARTBEAT: emit the docs/process/sdd-status-template.md block at
   claim, at every task-state change, at least every 10 minutes of
   wall-clock (chunk long foreground runs so a heartbeat lands between
   chunks), immediately on BLOCKED, and at final report.

7. On any Expected-mismatch beyond the §5 escape hatch, or any STOP
   condition: stop immediately, STATUS: BLOCKED, BLOCKERS = exact command +
   actual output + diagnosis, commit the ledger, LEAVE worktree and branch
   in place, report. No workarounds. Never close a red task.

8. GATE before closing — in the worktree:
   {{GATE_CMDS e.g. `go build ./... && go vet ./... && go test ./... -count=1`}}
   all green, with output pasted as evidence.
   {{MERGE_PROTOCOL if tasks merge to an integration branch; else delete}}

9. CLOSE the ledger: tick YOUR task's checkboxes; complete EVERY Outcome
   field — STATUS · BRANCH · COMMITS (hashes + messages) · FINDINGS (every
   deviation; "none" over dashes) · BLOCKERS · HANDOFF (discovered values,
   evidence the next task needs) — with pasted command OUTPUT, not
   paraphrase. Commit `docs: {{PLAN_SHORT}} — T<N> complete` (explicit
   pathspec).

10. REPORT and STOP (do not claim another task in this session):
    STATUS / Task / Branch + repo / Commits / Evidence (key commands +
    actual output lines) / Handoff. Statuses: DONE ·
    DONE_WITH_CONCERNS (state the concerns) · NEEDS_CONTEXT (state the
    missing context) · BLOCKED (per §7).

Task id (optional override): ____
Outward actions authorized: no ({{OUTWARD_SCOPE when yes}})
Owner gates authorized: no ({{OWNER_GATE_SCOPE when yes}})
````

- [ ] **Step 2: Commit**

```bash
git add docs/process/sdd-dispatch-template.md
git commit -m "docs(process): SDD dispatch prompt template (from p5/p7 prompts)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T7: SDD status heartbeat template

Formalizes the 10-minute visual update. Format spec + a filled example, so agents copy structure rather than invent.

**Files:**
- Create: `docs/process/sdd-status-template.md`

- [ ] **Step 1: Write the template**

````markdown
# SDD status heartbeat

## When to emit (all mandatory)

1. At task claim (baseline render).
2. On every task state change (DONE, BLOCKED, review verdict, fix dispatched).
3. At least every **10 minutes of wall-clock** while work is in flight.
   Long foreground runs are chunked into bounded calls (CLAUDE.md doctrine)
   — render a heartbeat between chunks.
4. Immediately on BLOCKED / NEEDS_CONTEXT / owner-gate hit.
5. As the final report's header.

## Format (blocks in this order; omit a block only if empty)

```
Overall: <D> of <T> tasks complete (<pct>%) · <n> in flight · <n> blocked
Time <HH:MM TZ> · started <HH:MM> · ETA ~<HH:MM>

Phase <K>  <bar>  <a>/<b> <unit>

  T<id>  <name> [<executor>]  → <STATE>  <detail>
         → <sub-item>            IN FLIGHT (<note, e.g. largest: …>)
         · <sub-item>            queued
         ✓ <sub-item>            done
         ⛔ <sub-item>           BLOCKED (<one-line reason>)

Lane <name> — <scope>   <bar>  <a>/<b>   <state / next>

<pacing: mode · measured rate · outlier caveat>
Discovered values (handoff): <k=v · k=v — only values later tasks consume>
Integrity: <main untouched?> · <pushed?> · <n> commits · <dirty files or "worktrees clean">
```

## Rules

- **Bar:** 10–16 cells, `█` filled = floor(done/total × cells), `░` rest.
- **States:** `✓ DONE` · `→ IN FLIGHT` · `· queued` · `⛔ BLOCKED` ·
  `⏸ OWNER-GATED` · `✗ FAILED (being fixed)`.
- **Executor tag:** what is doing the work — `[WORKFLOW wf_…]`, `[$REPO]`
  lane, `[subagent]`, `[inline]`.
- **ETA is measured, never invented:** after ≥1 completed unit,
  `ETA = now + remaining × measured-rate`; always `~`-prefixed; the pacing
  line states the basis (`~200s/doc measured`) and the biggest outlier
  (`README is biggest so likely slower`). Before any unit completes:
  `ETA: measuring`.
- **Integrity line is never omitted.** It answers: is main untouched, was
  anything pushed, how many commits exist, what is currently dirty.
- **Blocked items float up:** any ⛔ appears in Overall AND its phase block.
- **Discovered values** appear the heartbeat after discovery and persist
  until consumed (they mirror the ledger HANDOFF).
- **No prose padding.** The heartbeat is a render, not a narrative;
  anything needing sentences goes in the report or the ledger.

## Example (multi-lane, mid-run)

```
Overall: 17 of 20 tasks complete (85%) · 1 in flight · 0 blocked
Time 17:23 UTC+3 · started 17:21 · ETA ~17:45

Phase 4  ██░░░░░░░░░░░░░░  0/8 docs committed

  T15  doc fixes [WORKFLOW wf_6e796348-22a]  → IN FLIGHT
         → README.md            IN FLIGHT (largest: 51 residue + 9 findings)
         · pack-contract-v1     queued
         · cube-yaml-reference  queued
         · machine-readable     queued
         · kind-config-ref      queued
         · outstanding-todos    queued
         · tests/e2e/PACKS.md   queued
         · CHANGELOG.md         queued

Phase 7  ███░░░░░░░░░░░░░  2/15

Lane $PACKS — engine packs   ██████████  2/2   COMPLETE (T1 flux, T2 argocd)
Lane $ROOT  — engine seam    ░░░░░░░░░░  0/12  T3 next (fences)
Lane owner  — publish        ░░░░░░░░░░  0/1   T15 OWNER-GATED (not authorized this dispatch)

Sequential (shared tree) · ~200s/doc measured · README is biggest so likely slower
Discovered values (handoff): flux chart 2.19.0 (v1.9.2 controllers) ·
  REPLICA_KNOB = kustomizeController.resources.requests.cpu · argocd chart 10.1.4
Integrity: main untouched · nothing pushed · 25 commits · README.md currently modified
```
````

- [ ] **Step 2: Commit**

```bash
git add docs/process/sdd-status-template.md
git commit -m "docs(process): SDD status heartbeat — 10-minute visual update format

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T8: SDD plan-ledger template

The claim/close ledger format your p5/p7 plans carry, extracted so every future plan embeds it identically.

**Files:**
- Create: `docs/process/sdd-ledger-template.md`

- [ ] **Step 1: Write the template**

````markdown
# SDD plan ledger

Every SDD plan embeds two things: a **Task Index** table and a per-task
**Outcome block**. The ledger lives IN the plan file, edited only via the
claim/close protocol. It is the recovery map after compaction or session
loss: trust it and `git log` over memory.

## Task Index

| ID | Task | Depends | Outward? | STATUS |
| --- | --- | --- | --- | --- |
| T1 | <name> | — | no | UNCLAIMED |
| T2 | <name> | T1 | **yes** | UNCLAIMED |

STATUS values: `UNCLAIMED` · `IN_PROGRESS(<session id>, <UTC ts>)` ·
`DONE` · `DONE_WITH_CONCERNS` · `BLOCKED(<one word>)` · `NEEDS_CONTEXT`.
Suffix markers: `OWNER-GATED` (claimable only with explicit per-dispatch
authorization) · `[OUTWARD]` (touches github.com or any external system).

## Claim protocol

1. Re-read the ledger immediately before editing (another session may have
   claimed since your last read).
2. Set ONLY your task's STATUS to `IN_PROGRESS(<session>, <UTC ts>)`.
3. Commit the plan file alone, explicit pathspec:
   `git commit -m "docs: <plan-short> — claim T<N>" -- <plan-path>`
4. Verify HEAD contains your claim.

## Outcome block (one per task, filled at close)

```
#### T<N> Outcome
- STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
- BRANCH: <branch> (merged: yes|no) in <repo>
- COMMITS: <hash> <message> (one line each)
- FINDINGS: every deviation from the plan, with the evidence that forced
  it. "none" — never dashes, never blank.
- REVIEW: <task-review verdict, or "pending final review">
- BLOCKERS: exact command + actual output + diagnosis ("none" when DONE)
- HANDOFF: discovered values and evidence later tasks consume
  (versions, keys, ports, decisions). Never make a later task re-discover.
```

Evidence is pasted command OUTPUT, not paraphrase.

## Close protocol

1. Gate passes (plan's verification commands, all green, output captured).
2. Tick YOUR task's checkboxes in the plan body. Never touch another
   task's boxes or Outcome.
3. Fill EVERY Outcome field.
4. Commit: `git commit -m "docs: <plan-short> — T<N> complete" -- <plan-path>`
5. Append one line to `.superpowers/sdd/progress.md` if present:
   `Task N: complete (commits <base7>..<head7>, review <verdict>)`.

## Red lines

- Never re-claim or redo a task the ledger marks DONE — after compaction,
  re-verify via `git log`, then trust the ledger.
- Never close a red task. Never soften BLOCKED into DONE_WITH_CONCERNS.
- Ledger edits are separate `docs:` commits — never mixed into code commits.
````

- [ ] **Step 2: Commit**

```bash
git add docs/process/sdd-ledger-template.md
git commit -m "docs(process): SDD plan-ledger template (claim/close protocol)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T9: `CLAUDE.md` + `AGENTS.md` — the binding agent rules

The constitution. Absorbs the operational doctrine currently re-pasted into every dispatch prompt (§6 of the p7 prompt, §5–6 of phase-5 v2) so prompts shrink and the doctrine is versioned and reviewed like code.

**Files:**
- Create: `CLAUDE.md`
- Create: `AGENTS.md` (pointer)

**Interfaces:**
- Consumes: T5 ADR path, T6/T7/T8 template paths, T2 label names.

- [ ] **Step 1: Write `CLAUDE.md`**

````markdown
# cube-idp — Agent Rules (binding)

This file binds every AI agent session in this repository. Deviation
requires an explicit human instruction in the current session; note the
instruction in the work's FINDINGS/PR body. Process authority: ADR-0040
(`docs/adr/0040-adr-first-two-track-delivery-process.md`).

## 1. Decisions live in `docs/adr/`

- Before implementing in any governed area, read the relevant accepted
  ADRs — start at `docs/adr/README.md`; `area:*` labels mirror ADR domains.
- Never contradict an accepted ADR silently. Conflict → stop, flag, and
  propose a superseding ADR.
- Propose an ADR (stop and ask) when you are about to: add a dependency,
  create a new architectural pattern others must follow, choose between
  real alternatives with non-obvious tradeoffs, or contradict an ADR.
- Reference decisions in code as `ADR-NNNN` comments at the entry point;
  reference them in PR bodies as `Implements ADR-NNNN`.

## 2. Two-track intake (ADR-0040)

- **Track A** (features, architecture, hard-to-reverse): epic issue
  `[ADR-NNNN] <name>` (`type:adr`) → PR adding the ADR (status `proposed`,
  with Implementation Plan) → merge = accepted → sub-issues per
  deliverable → PRs close sub-issues.
- **Track B** (bug/chore/docs): plain issue → PR with `Closes #N`.
  Hitting an architectural choice mid-task escalates to Track A.
- **Spikes** are timeboxed and end in an ADR PR or close-with-reason.
  Closing "not doing X because Y" is a valid, valuable outcome.
- **WIP rule:** before opening a new Track-A epic, check open `type:adr`
  issues in the current milestone; justify non-blocking in the new Scope.

## 3. Issues & PRs

- Every PR body references an issue (`Closes #N`) or an ADR
  (`Implements ADR-NNNN`). CI (`process-gate`) enforces this.
- Issues created by agents carry the same required fields as the issue
  forms (`.github/ISSUE_TEMPLATE/`): type + area labels, repro/scope,
  version. `gh issue create` bypassing the forms does not bypass the fields.
- Labels are namespaced: exactly one `type:*`, `area:*` where known,
  `status:*` sparingly. No new labels without updating ADR-0040.
- New design/planning documents go ONLY into `docs/adr/` (via Track A).
  `docs/superpowers/` is a frozen archive — never add to it.

## 4. Branches, worktrees, commits

- Branch names: `adr-NNNN-<slug>` (Track A), `issue-N-<slug>` (Track B),
  `process/<slug>` (meta). Never work on `main`.
- **Never work in a main checkout.** All work — code, docs, plan ledgers —
  happens in an isolated worktree under `.claude/worktrees/` on the task's
  branch (create once, reuse; check for existence first).
- Explicit pathspecs always — never `git add -A` (stray-staged-files
  gotcha on this machine). Never commit `spokes-up.txt` or other sessions'
  untracked drafts.
- Every commit ends with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`

## 5. Subagent-driven development (SDD)

Plans are executed one-task-per-fresh-agent, per
`docs/process/sdd-dispatch-template.md`. Non-negotiables:

- The plan is NORMATIVE. No refactoring, renaming, scope-adding beyond it.
  Reality-vs-plan mismatch → minimal correction + FINDINGS entry, or BLOCKED.
- Claim before code; close with evidence — protocol and Outcome fields per
  `docs/process/sdd-ledger-template.md`.
- One task per dispatch, then STOP. Never claim a second task in-session.
- Fresh subagent per task; task review (spec compliance + code quality)
  after each; broad whole-branch review at the end. Fixes re-review.
- Dispatch prompts carry the task brief, interfaces, and constraints —
  never the session's accumulated history.
- Model selection: cheapest model that fits (transcription → cheap;
  integration → standard; design/final review → most capable). State the
  model explicitly in every dispatch.

## 6. Status heartbeat (mandatory during SDD)

Emit the visual status block per `docs/process/sdd-status-template.md`:
at claim, on every task state change, at least every 10 minutes of
wall-clock, immediately on BLOCKED, and as the final report header.
The Integrity line (main untouched · pushed? · commit count · dirty
files) is never omitted.

## 7. Outward actions & owner gates

- Outward = anything leaving this machine: pushing refs, tags, creating/
  editing GitHub issues/labels/milestones/releases, publishing packages.
- Outward actions require explicit per-dispatch authorization
  (`Outward actions authorized: yes` + scope). Absent that → NEEDS_CONTEXT.
- HARD LIMITS regardless of authorization: never push branches of this
  repo without the dispatch naming them; never force-push; never delete
  remote refs except a failed tag you yourself pushed this session.

## 8. Operational doctrine (hard-won; violating these wedged real runs)

a. **Foreground only.** Never background a long run and wait for a
   notification — it deadlocks the session. Run e2e/conformance/CI-watch
   as foreground Bash, chunked into bounded calls (each under the
   ~10-minute call timeout; loop sleep+check inside one call). Render a
   status heartbeat between chunks.
b. **Live legs are exclusive.** docker + host port 18443: before any
   kind/e2e run, `kind get clusters` must show no conf-*/e2e cluster and
   18443 must be free; poll until true. `CUBE_IDP_E2E_GATEWAY_PORT=18443`.
   One live leg at a time.
c. **Copy, never symlink,** any pack dir you stage (the hasher rejects
   symlinks, CUBE-4001).
d. **Verify with real commands,** never LSP/editor diagnostics (stale-
   diagnostics gotcha). Go gate: `go build ./... && go vet ./... &&
   go test ./... -count=1` in the worktree, all green.
e. **Tags:** exactly ONE tag per `git push` — >3 tags in one push emits
   ZERO GitHub events (CI silently skips).
f. **ghcr:** only tag-triggered CI can write packages (local token
   cannot). A new package may be created private — verify via
   `gh api "orgs/cube-idp/packages/container/<name>"`, record for the
   owner, do NOT flip it, do NOT treat as failure.
g. **go.mod** gains no new module unless the plan's task explicitly says so.

## 9. Repo map

- `docs/adr/` — decisions (source of truth) · `docs/process/` — SDD
  templates · `docs/archive/` — frozen history · `.github/ISSUE_TEMPLATE/`
  — intake forms · `internal/`, `cmd/` — Go code · `tests/` — suites.
````

- [ ] **Step 2: Write `AGENTS.md`**

```markdown
# Agent rules

All agent rules for this repository live in [CLAUDE.md](CLAUDE.md). They
bind every AI agent session regardless of harness.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md AGENTS.md
git commit -m "docs: CLAUDE.md — binding agent rules (process, SDD, doctrine)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T10: CI process gate

**Files:**
- Create: `.github/workflows/process-gate.yaml`

- [ ] **Step 1: Write the workflow** (PR body via env var — never interpolate untrusted body into the script)

```yaml
name: process-gate
on:
  pull_request:
    types: [opened, edited, reopened, synchronize]
permissions:
  contents: read
jobs:
  linked-work-item:
    name: PR references an issue or ADR
    runs-on: ubuntu-latest
    steps:
      - name: Check PR body for '#N' or 'ADR-NNNN'
        env:
          BODY: ${{ github.event.pull_request.body }}
          HEAD_REF: ${{ github.head_ref }}
        run: |
          if printf '%s' "$BODY" | grep -qE '(#[0-9]+|ADR-[0-9]{4})'; then
            echo "ok: work-item reference found"; exit 0
          fi
          case "$HEAD_REF" in
            release/*) echo "ok: release branch exempt"; exit 0 ;;
          esac
          echo "::error::PR body must reference an issue (#N) or an ADR (ADR-NNNN). See CLAUDE.md §3 / ADR-0040."
          exit 1
```

- [ ] **Step 2: Validate and commit**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/process-gate.yaml')); print('OK')"
command -v actionlint >/dev/null && actionlint .github/workflows/process-gate.yaml || echo "actionlint not installed — YAML parse gate only"
git add .github/workflows/process-gate.yaml
git commit -m "ci: process-gate — PRs must reference an issue or ADR

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T11: Pilot — issue #7 through Track A as ADR-0041 `[OUTWARD]`

#7 ("cluster mounts and extraPorts semantics for multi-node") already says "deliverable: a short design doc deciding the semantics, then implementation" — it is the perfect first Track-A exercise. The agent scaffolds; the OWNER decides in PR review.

**Files:**
- Create: `docs/adr/0041-multinode-mounts-and-extraports.md`
- Modify: `docs/adr/README.md` (index row)

- [ ] **Step 1: Convert #7 into the epic**

```bash
R=cube-idp/cube-idp
gh issue edit 7 -R $R --title "[ADR-0041] Cluster mounts and extraPorts semantics for multi-node clusters"
gh issue edit 7 -R $R --milestone "v0.2.0"
```

- [ ] **Step 2: Scaffold the ADR** — status `proposed`, options taken verbatim from #7's four questions, one recommendation per question marked `RECOMMENDED (agent) — owner adjudicates in PR review`:

```markdown
# 0041 — Cluster mounts and extraPorts Semantics for Multi-Node Clusters

Status: proposed
Date: 2026-07-20
Epic: cube-idp/cube-idp#7

## Context

`spec.cluster.extraPorts` / `spec.cluster.mounts` apply to the control-plane
node only (kind: `internal/cluster/kindp/merge.go:143,:163`; k3d:
`internal/cluster/k3dp/merge.go:153,:173`). `providerConfigRef` /
`forProvider` now allow multi-node topologies, where worker-scheduled pods
silently miss hostPath data and hostPort routing becomes provider-dependent.

## Options (from #7)

1. **Mounts scope** — (a) all nodes by default + optional per-role selector
   [RECOMMENDED (agent): least surprising for hostPath data] · (b) keep
   control-plane-only, documented.
2. **extraPorts semantics** — (a) control-plane only, documented ·
   (b) all nodes (host port conflicts!) · (c) provider-native LB answer
   (k3d serverlb) vs kind port-mapping [RECOMMENDED (agent): (a) now,
   (c) as follow-up — smallest correct step].
3. **Interaction with per-node conflict checks**
   (`internal/cluster/kindp/merge.go:147-156`) — decision follows 1&2.
4. **k3d specifics** (servers vs agents vs serverlb) — decision follows 2.

## Decision

_Pending PR review — the merge of this PR is the acceptance._

## Implementation Plan

- **Affected paths:** `internal/cluster/kindp/merge.go`,
  `internal/cluster/k3dp/merge.go`, provider contract tests.
- **Sub-issues (created at acceptance):** one per decided option group +
  e2e coverage on a multi-node topology.

## Verification

- [ ] Multi-node e2e: hostPath mount visible from a worker-scheduled pod
- [ ] Port semantics asserted per provider in the contract suite
- [ ] `spec.cluster.*` docs updated
```

- [ ] **Step 3: Index row + commit**

```bash
git add docs/adr/0041-multinode-mounts-and-extraports.md docs/adr/README.md
git commit -m "docs(adr): 0041 scaffold — multi-node mounts/extraPorts (proposed, decision pending review)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Record the post-acceptance sub-issue recipe in the ledger HANDOFF** (executed by the owner or a later dispatch AFTER the ADR PR merges — not now):

```bash
# For each Implementation Plan deliverable:
NEW=$(gh issue create -R cube-idp/cube-idp --title "<deliverable>" \
  --label "type:feature,area:cluster" --milestone "v0.2.0" \
  --body "Sub-issue of #7. Implements ADR-0041." | grep -oE '[0-9]+$')
ID=$(gh api repos/cube-idp/cube-idp/issues/$NEW --jq .id)
gh api repos/cube-idp/cube-idp/issues/7/sub_issues -X POST -F sub_issue_id=$ID
```

---

### Task T12: Finish the branch — merge PR, close issue — OWNER-GATED (merge)

The tracking PR has existed since bootstrap; every task pushed into it. T12 verifies, flips the ADR, and hands the merge to the owner. **Definition of done for the whole plan: PR merged AND tracking issue closed** (the close happens automatically via the PR body's `Closes #<tracking>`).

- [ ] **Step 1: Verify the branch end-to-end** (in the worktree)

```bash
git log --oneline main..process/0040-adr-first-sdd    # every T-commit present
git diff main..process/0040-adr-first-sdd --stat
for F in .github/ISSUE_TEMPLATE/*.yml .github/workflows/process-gate.yaml; do
  python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1])); print('OK', sys.argv[1])" "$F"; done
```
Expected: commits from T4,T5,T6,T7,T8,T9,T10,T11; all YAML `OK`.

- [ ] **Step 2: Confirm the ledger** — Task Index shows T2–T11 DONE/DONE_WITH_CONCERNS (T1 DONE or BLOCKED(owner-gate) with the fallback noted); every Outcome block filled with evidence.

- [ ] **Step 3: Flip ADR-0040 to `accepted`** (owner has approved the PR): edit `Status: proposed` → `Status: accepted` in `docs/adr/0040-…`, commit in the worktree:

```bash
git commit -m "docs(adr): 0040 accepted

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- docs/adr/0040-adr-first-two-track-delivery-process.md
git push origin process/0040-adr-first-sdd
```

- [ ] **Step 4 (owner-authorized): merge the PR** — closes the tracking issue automatically:

```bash
gh pr merge process/0040-adr-first-sdd -R cube-idp/cube-idp --merge --delete-branch=false
gh issue view <tracking> -R cube-idp/cube-idp --json state -q .state   # expect CLOSED
git -C $ROOT worktree remove $ROOT/.claude/worktrees/process-0040-adr-first-sdd
```

- [ ] **Step 5: Owner checklist (recorded here; not agent work)**

- [ ] Decide #17–#20: revive (→ Track A epic) or close-with-reason each
- [ ] Approve/adjust ADR-0041's recommendations in its PR review
- [ ] Optional: org-level Issue Types (Bug/Feature/Task/Epic) via org settings — labels already cover this; adopt only if the web UI view matters
- [ ] Optional: repeat T2's `area:` labels in packs/plugins when their issue volume warrants
- [ ] Delete merged audit branches after the audit workstream completes
- [ ] Announce: new issues go through the forms; agents obey CLAUDE.md

---

## Ledger Outcomes

#### T1 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T2 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T3 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T4 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T5 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T6 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T7 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T8 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T9 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T10 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T11 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T12 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:
