# GitHub ADR-First Process + Subagent-Driven Development Rules — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **AMENDMENT 2026-07-20 (issue #33 — applied before execution; no task had run yet):**
> 1. **ADR renumber:** main already carries `0040-diagnostic-code-identifier-stability.md` and `0041-changelog-content-policy.md` (audit merges #32). The process ADR is **ADR-0042**, the pilot is **ADR-0043**. The branch name `process/0040-adr-first-sdd` is historical and stays (renaming would break PR #22); the PR title/body should be updated to say ADR-0042 when next touched with outward authorization.
> 2. **T1 is OBSOLETE:** audit phases 2+3 are merged to main and merged into this branch — ADRs 0001–0041 are present. Skip T1.
> 3. **Plan relocated** from `docs/superpowers/plans/` to `docs/process/plans/` — the audit archived `docs/superpowers/` and T9's own rules freeze it.
> 4. **Projects v2 delivery board adopted** (issue #33 decision): board Status field owns pipeline state; `status:triage`/`status:needs-adr` labels are dropped (only `status:blocked` survives, as an orthogonal flag); board writes are automation-only. New tasks **T13** (board-sync workflow) and **T14** (board instantiation, owner-gated). T2/T4/T5/T9/T10 amended accordingly.
> 5. **Docs layout closed** (owner follow-up to #33): `docs/` top level becomes a CLOSED set (`adr/ architecture/ reference/ process/ archive/ vhs/`) declared in ADR-0042 §Documentation layout and enforced by the doc-consistency CI job. New task **T15** creates `docs/reference/` (moves the four contract docs) and a `docs/architecture/` skeleton — one file per `area:*` label, each carrying a machine-readable `<!-- cube:doc area=… code=… adrs=… -->` header so agents can grep their way to the right section and code entry points. Deliberately NO `docs/features/`.
> 6. **Isolated kubeconfig doctrine:** T9's CLAUDE.md §8 gains item (h) — every cluster-touching command carries an explicit per-command `KUBECONFIG=<worktree>/.kube/config`; never the user's default `~/.kube/config`.

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
- Next free ADR numbers: `0042` (process ADR) and `0043` (pilot). ADRs `0001–0041` are already on main (audit merges, PRs #31/#32) — T1 is OBSOLETE.
- Verification is real commands with pasted output — never editor/LSP diagnostics.
- YAML validity gate used throughout: `python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1])); print('OK')" <file>`.

## Plan lifecycle (bootstrap → merge)

This plan is itself Track-A-shaped work and follows the process it installs:

1. **Bootstrap (done at plan creation):** worktree + branch `process/0040-adr-first-sdd`; this plan file committed there; tracking issue opened; PR opened referencing the issue (`Closes #<tracking>`). The PR stays open for the whole run — every task's commits update it.
2. **Execution:** tasks T2–T11 and T13 land as commits on the branch (worktree only), pushed to keep the PR current. T14 (board instantiation) is owner-gated and may land after merge.
3. **Completion (T12):** all tasks DONE in the ledger → final verification → ADR-0042 flipped to `accepted` → owner merges the PR → the tracking issue closes automatically via the PR's `Closes` reference. Plan is complete only when the PR is merged AND the issue is closed.

Tracking issue: [#21](https://github.com/cube-idp/cube-idp/issues/21) · PR: [#22](https://github.com/cube-idp/cube-idp/pull/22).

## Phases (for the status heartbeat)

| Phase | Tasks | Deliverable |
| --- | --- | --- |
| 1 — Foundations | T2–T4 (T1 obsolete) | Labels, milestone, issue forms |
| 2 — Decision & Rules | T5–T9 | ADR-0042 (incl. board spec), three SDD templates, CLAUDE.md |
| 3 — Enforcement | T10, T13, T15 | CI gates, board-sync workflow, docs layout |
| 4 — Pilot & Closeout | T11–T12 | Issue #7 through Track A, PR, owner checklist |
| 5 — Board (owner) | T14 | Projects v2 board instantiated per ADR-0042 §Board |

## Task Index & Ledger

Statuses: `UNCLAIMED` → `IN_PROGRESS(<session>, <UTC ts>)` → `DONE` / `DONE_WITH_CONCERNS` / `BLOCKED` / `NEEDS_CONTEXT`. Claim before code; close with evidence. (T8 formalizes this format for future plans; this plan eats its own dog food.)

| ID | Task | Depends | Outward? | STATUS |
| --- | --- | --- | --- | --- |
| T1 | ~~Land `docs/adr/` 0001–0039 on main~~ | — | no | **OBSOLETE** (audit merged via #31/#32) |
| T2 | Label taxonomy across org repos + relabel open issues + `labels.yml` | — | **yes** | UNCLAIMED |
| T3 | Milestone `v0.2.0` + assignments | T2 | **yes** | UNCLAIMED |
| T4 | Issue forms | T2 | no | UNCLAIMED |
| T5 | ADR-0042: the process ADR (incl. §Board spec) | — | no | UNCLAIMED |
| T6 | SDD dispatch prompt template | — | no | UNCLAIMED |
| T7 | SDD status heartbeat template | — | no | UNCLAIMED |
| T8 | SDD plan-ledger template | — | no | UNCLAIMED |
| T9 | `CLAUDE.md` + `AGENTS.md` (binding agent rules) | T5,T6,T7,T8 | no | UNCLAIMED |
| T10 | CI process gate workflow (+ doc-consistency job) | T2 | no | UNCLAIMED |
| T11 | Pilot: issue #7 → ADR-0043 Track A | T2,T5,T9 | **yes** | UNCLAIMED |
| T12 | Finish the branch: verify, flip ADR, merge | all but T14 | **yes** | UNCLAIMED · **OWNER-GATED** (push) |
| T13 | `board-sync` workflow (status lifecycle automation) | T2,T5 | no | UNCLAIMED |
| T14 | Instantiate the Projects v2 board per ADR-0042 §Board | T5,T13 | **yes** | UNCLAIMED · **OWNER-GATED** |
| T15 | Docs layout: `reference/` move + `architecture/` skeleton (ADR-0042 §Docs) | T2,T5 | no | UNCLAIMED |

Per-task Outcome blocks live at the bottom of this file under "Ledger Outcomes".

---

### Task T1: Land `docs/adr/` (0001–0039) on main — OBSOLETE

> **OBSOLETE (amendment):** the audit workstream merged to main (PRs #31/#32) and main was merged into this branch — `docs/adr/0001–0041` and `docs/archive/superpowers/` are already present. Do not claim; do not execute any step below. Kept for the record only.

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
  # status: —— exactly ONE status label survives (amendment / ADR-0042 §Board):
  # pipeline position lives on the delivery board's Status field (automation-owned).
  # status:blocked stays a label because blocked-ness is orthogonal to pipeline
  # position (an In-progress item can be blocked) and must be readable without GraphQL.
  gh label create "status:blocked"   -R $R --color b60205 --description "Cannot proceed — blocker named in body (orthogonal to board Status)" --force
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
| #7 | `type:adr,area:cluster` | — |
| #8 | `type:feature,area:packs` | `enhancement` |
| #9 | `type:feature,area:gateway` | `enhancement` |
| #10 | `type:feature,area:registry` | `enhancement` |
| #11 | `type:docs` | `documentation` |
| #12 | `type:feature,area:packs` | `enhancement` |
| #13 | `type:chore,area:ci` | `enhancement` |
| #14 | `type:chore,area:ci` | `enhancement` |
| #15 | `type:bug,area:cluster` | — |
| #16 | `type:chore` | — |
| #17 | `type:question,area:packs` | — |
| #18 | `type:question,area:packs` | — |
| #19 | `type:question,area:packs` | — |
| #20 | `type:question` | — |
| #21 (this plan's tracking issue) | `type:adr,area:ci` | — |

```bash
R=cube-idp/cube-idp
gh issue edit 5  -R $R --add-label "type:bug,area:cluster" --remove-label "bug"
gh issue edit 6  -R $R --add-label "type:bug,area:cluster" --remove-label "bug"
gh issue edit 7  -R $R --add-label "type:adr,area:cluster"
gh issue edit 8  -R $R --add-label "type:feature,area:packs" --remove-label "enhancement"
gh issue edit 9  -R $R --add-label "type:feature,area:gateway" --remove-label "enhancement"
gh issue edit 10 -R $R --add-label "type:feature,area:registry" --remove-label "enhancement"
gh issue edit 11 -R $R --add-label "type:docs" --remove-label "documentation"
gh issue edit 12 -R $R --add-label "type:feature,area:packs" --remove-label "enhancement"
gh issue edit 13 -R $R --add-label "type:chore,area:ci" --remove-label "enhancement"
gh issue edit 14 -R $R --add-label "type:chore,area:ci" --remove-label "enhancement"
gh issue edit 15 -R $R --add-label "type:bug,area:cluster"
gh issue edit 16 -R $R --add-label "type:chore"
gh issue edit 17 -R $R --add-label "type:question,area:packs"
gh issue edit 18 -R $R --add-label "type:question,area:packs"
gh issue edit 19 -R $R --add-label "type:question,area:packs"
gh issue edit 20 -R $R --add-label "type:question"
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
Expected: only `type:*` (7), `area:*` (9), `status:blocked` (the single status label), `good first issue`, `help wanted`; second command prints nothing (no unlabeled open issues).

- [ ] **Step 5: Commit the taxonomy as `.github/labels.yml`** — single source of truth for label names (amendment / issue #33 G3-B). T4 forms, T9 CLAUDE.md, T10's doc-consistency job, and T13's board-sync all reference label names; this file is what they are checked against.

```yaml
# .github/labels.yml — normative label taxonomy (ADR-0042).
# CI (process-gate doc-consistency) asserts referenced labels exist here.
type:
  [bug, feature, chore, docs, adr, spike, question]
area:
  [cluster, packs, engine, registry, gateway, tui-output, diagnostics, trust, ci]
status:
  [blocked]   # pipeline position lives on the delivery board, not in labels
community:
  ["good first issue", "help wanted"]
```

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/labels.yml')); print('OK')"
git add .github/labels.yml
git commit -m "chore: labels.yml — normative label taxonomy (ADR-0042)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Record label list output in the ledger Outcome as evidence for steps 1–4.

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
labels: ["type:bug"]
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
labels: ["type:feature"]
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
      description: Path once the ADR PR exists, e.g. docs/adr/0043-multinode-mounts-ports.md (PR link until merged)
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

### Task T5: ADR-0042 — the process ADR (incl. the delivery board)

The process itself is the first exercise of the process: recorded as an ADR, accepted when the PR merges. **Amendment:** numbered 0042 (0040/0041 are taken on main); includes the Projects v2 delivery-board decision from issue #33.

**Files:**
- Create: `docs/adr/0042-adr-first-two-track-delivery-process.md`
- Modify: `docs/adr/README.md` (append index row)

- [ ] **Step 1: Write the ADR** (full text; status `proposed` — flipped to `accepted` in T12 when the PR merges)

```markdown
# 0042 — ADR-First Two-Track Delivery Process on GitHub

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
question), `area:` (mirrors ADR domains), `status:blocked` (the only status
label — an orthogonal impediment flag, readable without GraphQL). Pipeline
position lives exclusively on the delivery board's Status field (§Board).
Normative label list: `.github/labels.yml`. Milestones are per-repo release
buckets; unassigned = backlog.

## Delivery board (Projects v2)

One org-level Projects v2 board ("cube-idp delivery") is the **single owner
of workflow state**. Issues only — PRs are never board items (they are
transient, and the one PR-driven transition below keys off the *linked*
issue, which built-in workflows cannot do anyway).

**Division of labor:** labels are the machine-readable API (`type:`,
`area:`, `status:blocked` — cheap for agents/CI to read via REST); the
board Status is the pipeline view — machine-written, human-read. Agents
NEVER write board state; automation owns it.

**Fields (deliberately minimal — no duplication of what labels carry):**
- `Status` (single-select): `Backlog → Proposed → Accepted → In progress →
  In review → Done`.
- `Iteration`, `Estimate` — the only typed fields labels cannot express.
- NO `Area`/`Track`/`ADR` fields: area is a label, track is derivable
  (`type:adr`/`type:spike`), the ADR number is in the epic title
  `[ADR-NNNN] …` (that prefix is machine-parseable and load-bearing —
  it is the join key between an ADR PR and its epic).

**Status transitions (all automated; manual Status edits are a process
violation):**

| Transition | Trigger | Mechanism |
| --- | --- | --- |
| → Backlog | issue opened / added | built-in (auto-add + item-added workflow) |
| → Proposed | ADR PR opened carrying `ADR-NNNN` | `board-sync` workflow |
| → Accepted | that PR merged (adds `docs/adr/NNNN-*.md`) | `board-sync` workflow |
| → In progress | draft PR opened with `Closes #N` | `board-sync` workflow |
| → In review | PR ready for review with `Closes #N` | `board-sync` workflow |
| → Done | issue closed (incl. auto-close on merge) | built-in (item-closed workflow) |

**Credential:** org-installed GitHub App (org Projects: read/write) minted
per-run via `actions/create-github-app-token`; `GITHUB_TOKEN` cannot write
org projects. Org config: variable `BOARD_APP_ID`, secret
`BOARD_APP_PRIVATE_KEY`, variable `BOARD_PROJECT_NUMBER`.

**Instantiation:** the board cannot be created by committing a file. This
section is the board's source of truth; the board is its instantiation
(plan task T14 — scripted `gh project` commands plus a documented one-time
UI checklist for built-in workflows, which have no write API).

**What the board does NOT do:** milestones stay (release grouping); the
SDD status heartbeat stays the intra-run truth; decisions stay in ADRs —
the board tracks delivery, never decisions.

## Documentation layout

`docs/` top level is a CLOSED set. Adding a top-level directory or loose
file is an architectural act: it requires updating this ADR (Track A);
CI (`process-gate` doc-consistency) rejects unknown entries.

| Directory | Contents | Nature |
| --- | --- | --- |
| `docs/adr/` | numbered decision records | WHY — append-only |
| `docs/architecture/` | living system map, one file per `area:*` label | HOW it works NOW — updated in the same PR as the behavior change |
| `docs/reference/` | user-facing contracts: cube.yaml, kind config, machine-readable output, pack contract | WHAT users rely on |
| `docs/process/` | delivery machinery: SDD templates, `plans/`, model map | HOW we work |
| `docs/archive/` | frozen history | read-only — never added to |
| `docs/vhs/` | demo tapes | assets |

There is deliberately NO `docs/features/` and no per-feature design docs:
feature *decisions* are ADRs, feature *delivery* is issues + the board,
feature *current behavior* is `architecture/` + `reference/`. A features
folder is exactly the artifact class whose divergence the 2026-07-20 audit
measured (113 fully-specified never-built decisions).

**Area markers.** Every `docs/architecture/<area>.md` begins with a
machine-readable header comment; subsections may carry section markers:

    <!-- cube:doc area=packs code=internal/pack,internal/catalog adrs=0002,0003,0004,0005,0008 -->
    <!-- cube:section area=packs topic=fetching code=internal/pack/fetch adrs=0003 -->

Grammar: HTML comment · `cube:doc` | `cube:section` · space-separated
`key=value` pairs · comma-separated lists · `area` values must exist in
`.github/labels.yml`. Agents locate work by
`grep -rn 'cube:\(doc\|section\).*area=<area>' docs/architecture/`, then
follow `code=` to entry points and `adrs=` to the governing decisions.
CI validates header presence and area values; deep content stays human-owned.

**WIP rule:** before opening a new Track-A epic, list open epics in the
current milestone; an unfinished one must be justified as non-blocking in
the new epic's Scope field.

**Enforcement:** `CLAUDE.md` binds agent sessions (consult `docs/adr/`
before implementing in a governed area; propose an ADR on triggers; every
PR references an issue or ADR; no new design docs outside `docs/adr/`).
CI job `process-gate` rejects PRs whose body references neither `#N` nor
`ADR-NNNN` — the same guarantee makes `board-sync` deterministic (every PR
carries a join key). Subagent-driven execution follows `docs/process/`
templates.

## Non-Goals

- Board auto-add across all five org repos from day one — main repo only;
  extend when other repos' issue volume warrants (auto-add workflow count
  is plan-tier-limited; the `board-sync` script path has no such limit).
- No retroactive re-issueing of shipped work; ADRs 0001–0041 already record it.
- Issue forms gate the web UI only; agent-side enforcement is CLAUDE.md's job.

## Consequences

- Every feature has a falsifiable paper trail: ADR → epic → sub-issues → PRs.
- Ceremony is bounded: Track B stays one-issue-one-PR light.
- `docs/superpowers/` is frozen as an archive; new plans attach to ADRs/epics.
- Follow-ups: #17–#20 must each get a Track-A revive or a reasoned close.

## Implementation Plan

- **Affected paths:** `.github/ISSUE_TEMPLATE/`, `.github/workflows/process-gate.yaml`,
  `.github/workflows/board-sync.yaml`, `.github/labels.yml`, `CLAUDE.md`,
  `AGENTS.md`, `docs/process/`, `docs/adr/README.md`.
- **Installed by:** `docs/process/plans/2026-07-20-github-process-and-sdd.md`
  (tasks T2–T13; board instantiation T14), the first SDD run using the new
  templates.
- **Pattern for future Track-A work:** pilot ADR-0043 (issue #7).

## Verification

- [ ] `gh label list` shows only the namespaced taxonomy (+ community labels);
      the only `status:*` label is `status:blocked`
- [ ] Every open issue carries a `type:` label
- [ ] `process-gate` fails a PR with no `#N`/`ADR-NNNN` reference, passes one with
- [ ] Issue #7 retitled `[ADR-0043] …` with an accepted ADR and sub-issues
- [ ] `CLAUDE.md` present at repo root; agent session confirms it loads
- [ ] Board: a test issue lands in Backlog on open and moves to Done on close
      with zero manual Status edits; an ADR PR open/merge moves its epic to
      Proposed/Accepted (T14 verification)
```

- [ ] **Step 2: Append the index row to `docs/adr/README.md`**

```markdown
| 0042 | [ADR-First Two-Track Delivery Process on GitHub](0042-adr-first-two-track-delivery-process.md) | — |
```

- [ ] **Step 3: Commit**

```bash
git add docs/adr/0042-adr-first-two-track-delivery-process.md docs/adr/README.md
git commit -m "docs(adr): 0042 — ADR-first two-track delivery process + delivery board (proposed)

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
instruction in the work's FINDINGS/PR body. Process authority: ADR-0042
(`docs/adr/0042-adr-first-two-track-delivery-process.md`).

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

## 2. Two-track intake (ADR-0042)

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
  `status:blocked` only when genuinely blocked. The normative label list is
  `.github/labels.yml`; no new labels without updating it AND ADR-0042.
- **Workflow status lives on the delivery board (ADR-0042 §Board), and the
  board is automation-owned. NEVER set board Status manually and NEVER
  script board mutations — `board-sync` and built-in workflows are the only
  writers.** `status:*` labels other than `status:blocked` do not exist.
- New design/planning documents go ONLY into `docs/adr/` (via Track A).
  `docs/archive/` is frozen — never add to it.
- **`docs/` top level is a closed set (ADR-0042 §Documentation layout):**
  `adr/ architecture/ reference/ process/ archive/ vhs/`. Never create a
  new top-level docs directory or loose file — that requires updating
  ADR-0042 first; CI rejects unknown entries.
- **Changing behavior in a governed area updates
  `docs/architecture/<area>.md` in the SAME PR.** Find the section via its
  `cube:doc` / `cube:section` markers; keep the markers' `code=`/`adrs=`
  lists current. When designing new functionality, read that area file
  FIRST — it is the map of what exists.

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
  editing GitHub issues/labels/milestones/releases/project boards,
  publishing packages. (Board *Status* is never yours to set even with
  outward authorization — see §3.)
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
h. **Isolated kubeconfig, always.** Never read or write the user's default
   kubeconfig (`~/.kube/config`). Every cluster-touching command — kind,
   kubectl, helm, flux, `cube-idp` itself, e2e legs — carries an explicit
   per-command inline env var, one file per worktree/leg:
   `KUBECONFIG=<worktree>/.kube/config kind create cluster …`
   `KUBECONFIG=<worktree>/.kube/config go test ./tests/e2e/…`
   Inline on the command, never a session-wide export, never a shell-profile
   edit. kind/k3d honor `KUBECONFIG` for context writes, so contexts land in
   the isolated file; delete the file when the leg's cluster is deleted.
   (`kind get clusters` talks to docker and needs no kubeconfig.)

## 9. Repo map

- `docs/adr/` — decisions (why) · `docs/architecture/` — living system map,
  one file per `area:*`, `cube:doc` markers (how it works now) ·
  `docs/reference/` — user-facing contracts · `docs/process/` — SDD
  templates, plans · `docs/archive/` — frozen history ·
  `.github/ISSUE_TEMPLATE/` — intake forms · `internal/`, `cmd/` — Go code
  · `tests/` — suites.
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
          echo "::error::PR body must reference an issue (#N) or an ADR (ADR-NNNN). See CLAUDE.md §3 / ADR-0042."
          exit 1
  doc-consistency:
    name: Process docs are internally consistent
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: AGENTS.md points at CLAUDE.md and the target exists
        run: |
          test -f CLAUDE.md
          grep -q 'CLAUDE.md' AGENTS.md
      - name: No machine-specific absolute paths committed
        run: |
          ! grep -rEn '/Users/[a-z]' --include='*.md' CLAUDE.md AGENTS.md docs/process/ .github/ 2>/dev/null
      - name: Labels referenced by forms and rules exist in labels.yml
        run: |
          python3 - <<'EOF'
          import yaml, re, glob, sys
          tax = yaml.safe_load(open('.github/labels.yml'))
          known = {f"{ns}:{n}" for ns in ('type','area','status') for n in tax.get(ns, [])}
          referenced = set()
          for f in glob.glob('.github/ISSUE_TEMPLATE/*.yml'):
              for lbl in (yaml.safe_load(open(f)) or {}).get('labels', []):
                  referenced.add(lbl)
          referenced |= set(re.findall(r'`((?:type|area|status):[a-z-]+)`', open('CLAUDE.md').read()))
          missing = referenced - known
          if missing: sys.exit(f"labels referenced but not in labels.yml: {sorted(missing)}")
          print("OK", len(referenced), "label refs checked")
          EOF
      - name: docs/ top level is the closed set (ADR-0042 §Documentation layout)
        run: |
          # outstanding-todos.md is temporarily allowed until the owner retires
          # it into issues (T15 FINDINGS records the disposition) — remove it
          # from this list when that happens.
          ALLOWED="adr architecture reference process archive vhs outstanding-todos.md"
          rc=0
          for e in $(ls docs/); do
            case " $ALLOWED " in
              *" $e "*) ;;
              *) echo "::error::docs/$e is not in the ADR-0042 closed set — update the ADR first"; rc=1 ;;
            esac
          done
          exit $rc
      - name: architecture docs carry valid cube:doc area headers
        run: |
          python3 - <<'EOF'
          import re, glob, sys, yaml
          areas = set(yaml.safe_load(open('.github/labels.yml')).get('area', []))
          bad = []
          for f in sorted(glob.glob('docs/architecture/*.md')):
              if f.endswith('README.md'): continue
              m = re.match(r'<!-- cube:doc area=([a-z-]+)[ >]', open(f).readline())
              if not m or m.group(1) not in areas: bad.append(f)
          if bad: sys.exit(f"missing/invalid cube:doc header: {bad}")
          print("OK", "architecture headers valid")
          EOF
```

- [ ] **Step 2: Validate and commit**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/process-gate.yaml')); print('OK')"
command -v actionlint >/dev/null && actionlint .github/workflows/process-gate.yaml || echo "actionlint not installed — YAML parse gate only"
git add .github/workflows/process-gate.yaml
git commit -m "ci: process-gate — work-item reference + process-doc consistency

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T11: Pilot — issue #7 through Track A as ADR-0043 `[OUTWARD]`

#7 ("cluster mounts and extraPorts semantics for multi-node") already says "deliverable: a short design doc deciding the semantics, then implementation" — it is the perfect first Track-A exercise. The agent scaffolds; the OWNER decides in PR review.

**Files:**
- Create: `docs/adr/0043-multinode-mounts-and-extraports.md`
- Modify: `docs/adr/README.md` (index row)

- [ ] **Step 1: Convert #7 into the epic**

```bash
R=cube-idp/cube-idp
gh issue edit 7 -R $R --title "[ADR-0043] Cluster mounts and extraPorts semantics for multi-node clusters"
gh issue edit 7 -R $R --milestone "v0.2.0"
```

- [ ] **Step 2: Scaffold the ADR** — status `proposed`, options taken verbatim from #7's four questions, one recommendation per question marked `RECOMMENDED (agent) — owner adjudicates in PR review`:

```markdown
# 0043 — Cluster mounts and extraPorts Semantics for Multi-Node Clusters

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
git add docs/adr/0043-multinode-mounts-and-extraports.md docs/adr/README.md
git commit -m "docs(adr): 0043 scaffold — multi-node mounts/extraPorts (proposed, decision pending review)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Record the post-acceptance sub-issue recipe in the ledger HANDOFF** (executed by the owner or a later dispatch AFTER the ADR PR merges — not now):

```bash
# For each Implementation Plan deliverable:
NEW=$(gh issue create -R cube-idp/cube-idp --title "<deliverable>" \
  --label "type:feature,area:cluster" --milestone "v0.2.0" \
  --body "Sub-issue of #7. Implements ADR-0043." | grep -oE '[0-9]+$')
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
Expected: commits from T2(labels.yml),T4,T5,T6,T7,T8,T9,T10,T11,T13,T15; all YAML `OK`.

- [ ] **Step 2: Confirm the ledger** — Task Index shows T2–T11 DONE/DONE_WITH_CONCERNS (T1 DONE or BLOCKED(owner-gate) with the fallback noted); every Outcome block filled with evidence.

- [ ] **Step 3: Flip ADR-0042 to `accepted`** (owner has approved the PR): edit `Status: proposed` → `Status: accepted` in `docs/adr/0042-…`, commit in the worktree:

```bash
git commit -m "docs(adr): 0042 accepted

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" -- docs/adr/0042-adr-first-two-track-delivery-process.md
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
- [ ] Approve/adjust ADR-0043's recommendations in its PR review
- [ ] Execute T14 (board instantiation) — the only piece that cannot land by merging this PR
- [ ] Optional: org-level Issue Types (Bug/Feature/Task/Epic) via org settings — labels already cover this; adopt only if the web UI view matters
- [ ] Optional: repeat T2's `area:` labels in packs/plugins when their issue volume warrants
- [ ] Delete merged audit branches after the audit workstream completes
- [ ] Announce: new issues go through the forms; agents obey CLAUDE.md

---

### Task T13: `board-sync` workflow — automated status lifecycle

Implements ADR-0042 §Board's four custom transitions (Proposed / Accepted / In progress / In review). Backlog and Done are built-in board workflows (T14). First-party GraphQL via `gh api` — no third-party marketplace actions (`actions/add-to-project` is unmaintained; a marketplace dependency also sits badly with this org's trust posture).

Deterministic join keys, both guaranteed present by `process-gate`: `ADR-NNNN` in the PR body/title joins an ADR PR to its epic (titled `[ADR-NNNN] …`); `Closes #N` joins a delivery PR to its issue.

**Files:**
- Create: `.github/workflows/board-sync.yaml`

**Interfaces:**
- Consumes: org variable `BOARD_APP_ID`, org secret `BOARD_APP_PRIVATE_KEY`, org variable `BOARD_PROJECT_NUMBER` (all created in T14 — until T14 runs, the workflow exits cleanly when they are unset).

- [ ] **Step 1: Write the workflow**

```yaml
name: board-sync
on:
  pull_request:
    types: [opened, ready_for_review, converted_to_draft, closed]
permissions:
  contents: read
concurrency:
  group: board-sync-${{ github.event.pull_request.number }}
  cancel-in-progress: false
jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - name: Skip until the board exists (T14 not yet executed)
        id: gate
        env:
          N: ${{ vars.BOARD_PROJECT_NUMBER }}
        run: |
          if [ -z "$N" ]; then echo "skip=true" >> "$GITHUB_OUTPUT"; echo "board not instantiated — nothing to sync"; fi
      - name: Mint board token (org App)
        if: steps.gate.outputs.skip != 'true'
        id: app
        uses: actions/create-github-app-token@v1
        with:
          app-id: ${{ vars.BOARD_APP_ID }}
          private-key: ${{ secrets.BOARD_APP_PRIVATE_KEY }}
          owner: cube-idp
      - name: Resolve target issue and status
        if: steps.gate.outputs.skip != 'true'
        env:
          GH_TOKEN: ${{ steps.app.outputs.token }}
          BODY: ${{ github.event.pull_request.body }}
          TITLE: ${{ github.event.pull_request.title }}
          ACTION: ${{ github.event.action }}
          DRAFT: ${{ github.event.pull_request.draft }}
          MERGED: ${{ github.event.pull_request.merged }}
          PROJECT_NUMBER: ${{ vars.BOARD_PROJECT_NUMBER }}
        run: |
          set -euo pipefail
          ADR=$(printf '%s\n%s' "$TITLE" "$BODY" | grep -oE 'ADR-[0-9]{4}' | head -1 || true)
          CLOSES=$(printf '%s' "$BODY" | grep -oiE '(close[sd]?|fix(e[sd])?|resolve[sd]?) #[0-9]+' | grep -oE '[0-9]+' | head -1 || true)
          ISSUE="" STATUS=""
          if [ -n "$ADR" ]; then
            # ADR PR → move the epic titled "[ADR-NNNN] …"
            ISSUE=$(gh api graphql \
              -f query='query($q:String!){search(query:$q,type:ISSUE,first:1){nodes{... on Issue{number}}}}' \
              -f q="repo:$GITHUB_REPOSITORY is:issue in:title \"[$ADR]\"" \
              --jq '.data.search.nodes[0].number' 2>/dev/null || true)
            case "$ACTION" in
              opened|ready_for_review) STATUS="Proposed" ;;
              closed) if [ "$MERGED" = "true" ]; then STATUS="Accepted"; fi ;;
            esac
          elif [ -n "$CLOSES" ]; then
            ISSUE=$CLOSES
            case "$ACTION" in
              opened) if [ "$DRAFT" = "true" ]; then STATUS="In progress"; else STATUS="In review"; fi ;;
              ready_for_review) STATUS="In review" ;;
              converted_to_draft) STATUS="In progress" ;;
              closed) : ;;   # merge auto-closes the issue; built-in workflow moves it to Done
            esac
          fi
          if [ -z "$ISSUE" ] || [ -z "$STATUS" ]; then echo "nothing to sync"; exit 0; fi
          echo "sync: issue #$ISSUE -> $STATUS"

          PID=$(gh api graphql \
            -f query='query($org:String!,$n:Int!){organization(login:$org){projectV2(number:$n){id}}}' \
            -f org="${GITHUB_REPOSITORY_OWNER}" -F n="$PROJECT_NUMBER" \
            --jq '.data.organization.projectV2.id')
          FIELD=$(gh api graphql \
            -f query='query($p:ID!){node(id:$p){... on ProjectV2{field(name:"Status"){... on ProjectV2SingleSelectField{id options{id name}}}}}}' \
            -f p="$PID" --jq '.data.node.field')
          FID=$(echo "$FIELD" | jq -r '.id')
          OID=$(echo "$FIELD" | jq -r --arg s "$STATUS" '.options[] | select(.name==$s) | .id')
          CID=$(gh api graphql \
            -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){id}}}' \
            -f o="${GITHUB_REPOSITORY_OWNER}" -f r="${GITHUB_REPOSITORY#*/}" -F n="$ISSUE" \
            --jq '.data.repository.issue.id')
          ITEM=$(gh api graphql \
            -f query='mutation($p:ID!,$c:ID!){addProjectV2ItemById(input:{projectId:$p,contentId:$c}){item{id}}}' \
            -f p="$PID" -f c="$CID" --jq '.data.addProjectV2ItemById.item.id')   # idempotent: returns the existing item
          gh api graphql \
            -f query='mutation($p:ID!,$i:ID!,$f:ID!,$o:String!){updateProjectV2ItemFieldValue(input:{projectId:$p,itemId:$i,fieldId:$f,value:{singleSelectOptionId:$o}}){projectV2Item{id}}}' \
            -f p="$PID" -f i="$ITEM" -f f="$FID" -f o="$OID" --jq '.data.updateProjectV2ItemFieldValue.projectV2Item.id'
```

- [ ] **Step 2: Validate and commit**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/board-sync.yaml')); print('OK')"
command -v actionlint >/dev/null && actionlint .github/workflows/board-sync.yaml || echo "actionlint not installed — YAML parse gate only"
git add .github/workflows/board-sync.yaml
git commit -m "ci: board-sync — automated delivery-board status lifecycle (ADR-0042 §Board)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task T14: Instantiate the Projects v2 board — OWNER-GATED `[OUTWARD]`

The board cannot be created by committing a file: creation needs a `project`-scoped token (`gh auth refresh -s project`), and built-in workflow configuration has **no write API** (UI only). ADR-0042 §Board is the spec; this task is its instantiation. May run after PR #22 merges.

- [ ] **Step 1 (scripted): create the project and fields**

```bash
gh project create --owner cube-idp --title "cube-idp delivery"
N=$(gh project list --owner cube-idp --format json --jq '.projects[] | select(.title=="cube-idp delivery") | .number')
gh project field-create $N --owner cube-idp --name "Estimate" --data-type NUMBER
# Iteration fields cannot be created via CLI — create "Iteration" in the UI.
# Status options: edit the built-in Status field to exactly:
#   Backlog · Proposed · Accepted · In progress · In review · Done
# (UI, or GraphQL updateProjectV2Field with singleSelectOptions — names must
#  match board-sync's strings byte-for-byte.)
```

- [ ] **Step 2 (UI checklist): built-in workflows**

- Auto-add: repo `cube-idp/cube-idp`, filter `is:issue is:open` (issues only — PRs are never items). Auto-add workflow count is plan-tier-limited; other repos join via a `board-sync` extension, not more auto-add slots.
- "Item added to project" → Status: `Backlog`
- "Item closed" → Status: `Done`
- "Item reopened" → Status: `In progress`
- Disable any default PR-related built-ins (PRs are not items).

- [ ] **Step 3: credential for board-sync**

- Create an org GitHub App (`cube-idp-board-bot`): org permission **Projects: read & write**, repo permission **Issues: read**. Install on the org.
- Org **variable** `BOARD_APP_ID`, org **secret** `BOARD_APP_PRIVATE_KEY`, org **variable** `BOARD_PROJECT_NUMBER` = `$N`.

- [ ] **Step 4: end-to-end verification (per ADR-0042 Verification)**

```bash
# open a scratch issue → appears on board in Backlog with zero manual edits
gh issue create -R cube-idp/cube-idp --title "board smoke test" --label "type:chore" --body "Scratch — verifying ADR-0042 §Board automation."
# open a draft PR with "Closes #<that issue>" → In progress; mark ready → In review;
# merge → issue auto-closes → Done. Then check the epic path with an ADR PR (Proposed → Accepted).
gh project item-list $N --owner cube-idp --format json --jq '.items[] | {title: .content.title, status: .status}'
```
Expected: every transition happened without a human or agent touching Status.

---

### Task T15: Docs layout normalization — `reference/` move + `architecture/` skeleton

Implements ADR-0042 §Documentation layout. Mechanical moves plus navigable stubs — NO reference content is rewritten in this task; architecture files start as maps (markers + ADR index + code entry points) and get their prose filled by the first behavior-changing PR per area (CLAUDE.md §3 same-PR rule).

**Files:**
- Create `docs/reference/`; `git mv` into it: `cube-yaml-reference.md`, `kind-config-reference.md`, `machine-readable-output.md`, `pack-contract-v1.md`
- Create `docs/architecture/README.md` (marker grammar, navigation how-to) and one stub per `area:*` label: `cluster.md`, `packs.md`, `engine.md`, `registry.md`, `gateway.md`, `tui-output.md`, `diagnostics.md`, `trust.md`, `ci.md`
- Leave `docs/outstanding-todos.md` in place (temporarily allowed by doc-consistency) — its items belong in issues under the new process; record in FINDINGS that the owner must convert-then-archive it.

- [ ] **Step 1: Move the reference docs and fix every inbound link**

```bash
mkdir -p docs/reference
git mv docs/cube-yaml-reference.md docs/kind-config-reference.md docs/machine-readable-output.md docs/pack-contract-v1.md docs/reference/
for F in cube-yaml-reference kind-config-reference machine-readable-output pack-contract-v1; do
  grep -rln "docs/$F.md" README.md CHANGELOG.md docs/ internal/ cmd/ tests/ .github/ 2>/dev/null
done
# update every hit to docs/reference/<name>.md, then verify zero stale links:
grep -rn 'docs/\(cube-yaml-reference\|kind-config-reference\|machine-readable-output\|pack-contract-v1\)\.md' . --include='*.md' --include='*.go' | grep -v docs/reference && echo "STALE LINKS" || echo "OK links"
```

- [ ] **Step 2: Write the architecture skeleton** — each stub is a MAP, not prose. Shape (example `packs.md`; derive `adrs=` from `docs/adr/README.md`, `code=` from the package layout):

```markdown
<!-- cube:doc area=packs code=internal/pack,internal/catalog adrs=0002,0003,0004,0005,0008,0009 -->
# Architecture — packs

Governing decisions: ADR-0002 (format), ADR-0003 (refs/pinning),
ADR-0004 (values/extra manifests), ADR-0005 (deps/ordering),
ADR-0008 (distribution), ADR-0009 (air-gap/integrity).
User contract: ../reference/pack-contract-v1.md

<!-- cube:section area=packs topic=format code=internal/pack -->
## Format
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._
```

`docs/architecture/README.md` documents the grammar verbatim from ADR-0042 §Documentation layout and the navigation recipe (`grep -rn 'cube:section.*area=<area>' docs/architecture/`).

- [ ] **Step 3: Validate and commit**

```bash
python3 - <<'EOF'
import re, glob, yaml
areas = set(yaml.safe_load(open('.github/labels.yml'))['area'])
for f in sorted(glob.glob('docs/architecture/*.md')):
    if f.endswith('README.md'): continue
    m = re.match(r'<!-- cube:doc area=([a-z-]+)[ >]', open(f).readline())
    assert m and m.group(1) in areas, f
print('OK', 'headers valid')
EOF
git add docs/reference/ docs/architecture/ README.md
git commit -m "docs: closed layout — reference/ move + architecture/ area skeleton (ADR-0042)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Ledger Outcomes

#### T1 Outcome
- STATUS: OBSOLETE · BRANCH: n/a · COMMITS: n/a · FINDINGS: audit phases 2+3 merged to main (#31/#32) before execution started; ADRs 0001–0041 present on this branch via merge `ced665a` · REVIEW: n/a · BLOCKERS: none · HANDOFF: next free ADR numbers are 0042/0043

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

#### T13 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T14 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:

#### T15 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · REVIEW: · BLOCKERS: · HANDOFF:
