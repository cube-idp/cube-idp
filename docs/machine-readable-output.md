# Machine-readable output

> **EXPERIMENTAL** — both schemas on this page (the event stream and the
> document mode) are experimental until the v1 `cube.yaml` config freeze
> (spec D5). Until then, fields may be added, renamed, or removed without
> notice. After the freeze they receive a Terraform-style compatibility
> promise: within a major schema version (`"v": 1`), existing fields keep
> their name and meaning; new fields may be added; consumers must ignore
> fields they do not recognize.

cube-idp has two machine-readable surfaces, selected independently:

| Surface | Commands | How to select | Shape |
|---|---|---|---|
| **Event stream** | long-running commands: `up`, `down`, `vendor`; short static commands: `sync` (one-shot), `repo create`, `plugin list`\|`trust`\|`install`, `pack push` | `--progress=json` or `CUBE_IDP_PROGRESS=json` | JSON lines: one event per line, written to the same channel as it's produced — the short commands just produce very few lines in quick succession, not a separate batching behavior |
| **Documents** | request/response commands: `status`, `doctor`, `get secrets` | `--output json` (or `-o json`); `--progress=json` selects the same document on these commands | one pretty-printed JSON object per invocation, emitted once at the end |

Everything else about a run is unchanged in both modes: exit codes, the
`--file` flag, and the human-readable `✗ CUBE-…` diagnosis block, which is
printed to **stderr** — stdout carries only JSON, so `cube-idp up
--progress=json | jq .` is always safe.

Commands with no meaningful JSON form (`version`, `trust`, `config`, …) keep
their plain text output under `--progress=json`; plain *is* their machine
contract.

---

## 1. The event stream (`--progress=json`)

Long-running commands emit one canonical stream of typed events. In JSON
mode each event is one JSON object on one line on **stdout** — never
batched, never pretty-printed, exactly one event per object.

### Envelope

Every line carries `v` and `type`; every line except `encode_error` (the
envelope-level marshal-failure fallback, documented at the end of the
Event types section below) also carries `ts`:

| Field | Type | Meaning |
|---|---|---|
| `v` | number | Schema version. Currently `1`. |
| `ts` | string | Event timestamp, RFC3339Nano. **Absent on `encode_error`.** |
| `type` | string | The event type; one of the types below. |

### Ordering guarantees

1. `run_started` is first — when emitted at all. It is skipped when config
   loading fails; consumers must tolerate a stream that is only
   `run_done` + `diagnosis`.
2. Every `step_started` is resolved by the next `step_done` or
   `step_failed` for the same stage, or implicitly by `run_done`.
3. Success termination: `… → epilogue? → access? → run_done{ok:true}`.
   Nothing follows `run_done` on success.
4. Failure termination: `… → step_failed? → run_done{ok:false} →
   diagnosis`. **`diagnosis` is always the final event on a failed run** —
   machine consumers (CI annotators, wrappers) may treat it as the
   terminal record, the Terraform `diagnostic` precedent.

`stage` is an open string, not an enum: `up` currently emits `config`,
`ca`, `cluster`, `registry`, `packs-crd`, `engine`, `tls`, `pack`, `lock`,
`dns`, `health`, `packs`; `down` emits `engine`, `dns`, `cascade`,
`cluster`, `trust`; `vendor` emits a single stage, `vendor`, once per pack
and once per image plus the final bundle-written step (`cmd`/`cube` on
`run_started` is `"vendor"`/`""` — vendor is a pure `cube.lock` consumer with
no `cube.yaml`, so `cube` is always empty); `sync` (one-shot only — `--watch`
keeps its own plain loop, out of scope for the event stream) emits a single
stage, `sync`, three times: rendered, pushed, delivered. `repo create` emits
no stages at all — its whole access block (created confirmation, clone URL,
push command, and the deploy line when `--deploy` was passed) is a sequence
of `note` events instead. `plugin list`/`trust`/`install` and `pack push`
are the same shape: `plugin list` emits either one `warn` (nothing
discovered) or one `note` (the NAME/PATH/TRUSTED table, embedded newlines
and all); `plugin trust`/`plugin install` each emit one `note`; `pack push`
emits a single `step_done` on stage `pack`. None of the five short static
commands ever pop the live step-tree — `--progress=live` still forces it,
but a real terminal under the default auto-styled mode gets the
content-identical styled-static projection instead (design doc §5.2; the
live tree is reserved for `up`/`down`/`vendor`). Packs and future commands
may add stages without a schema version bump.

### Event types

#### `run_started`

Opens a run, after `cube.yaml` loaded and validated.

```json
{"v":1,"ts":"…","type":"run_started","cmd":"up","cube":"dev"}
```

| Field | Type | Meaning |
|---|---|---|
| `cmd` | string | The command producing the stream (`up`, `down`). |
| `cube` | string | `metadata.name` from `cube.yaml`. |

#### `step_started`

A stage is now in-flight (the spinner line in a terminal run).

```json
{"v":1,"ts":"…","type":"step_started","stage":"cluster","msg":"creating kind cluster"}
```

| Field | Type | Meaning |
|---|---|---|
| `stage` | string | Stage tag (see the stage list above). |
| `msg` | string | Human-readable in-flight message. |
| `idx` | number | 1-based n-of-m position for enumerated repeats (the `pack` delivery loop emits `"idx":3,"of":7`). **Omitted** when the step is not enumerated. |
| `of` | number | The enumeration total. **Omitted** with `idx`. |

#### `step_done`

A stage completed successfully.

```json
{"v":1,"ts":"…","type":"step_done","stage":"cluster","msg":"kind cluster ready (context kind-dev)","dur_ms":72340}
```

| Field | Type | Meaning |
|---|---|---|
| `stage` | string | Stage tag. |
| `msg` | string | Completion message. |
| `dur_ms` | number | Elapsed milliseconds. **Omitted** when 0 (instantaneous steps). |
| `idx` | number | 1-based n-of-m position for enumerated repeats. **Omitted** when not enumerated. |
| `of` | number | The enumeration total. **Omitted** with `idx`. |

#### `step_failed`

The in-flight stage failed. The authoritative error arrives later as
`diagnosis`; this event marks *which* stage was open, what it was doing,
and how long it ran.

```json
{"v":1,"ts":"…","type":"step_failed","stage":"engine","msg":"installing flux","dur_ms":4000}
```

| Field | Type | Meaning |
|---|---|---|
| `stage` | string | The failed stage. |
| `msg` | string | The step's in-flight message. **Omitted** when empty (older producers). |
| `dur_ms` | number | Elapsed milliseconds. **Omitted** when 0. |

#### `health_tick`

One poll of engine component health during the `health` stage. Emitted on
the first poll and thereafter only when any component's `ready`/`message`
changed — the stream never repeats identical lines every poll interval.

```json
{"v":1,"ts":"…","type":"health_tick","components":[{"name":"cube-idp-traefik","ready":false,"message":"reconciling"}]}
```

| Field | Type | Meaning |
|---|---|---|
| `components` | array | One entry per component: `name` (string), `ready` (bool), `message` (string). |

#### `note`

A neutral passthrough line (e.g. `up`'s final success block, `down`'s
trust-revert messages). `msg` may contain embedded newlines.

```json
{"v":1,"ts":"…","type":"note","msg":"…"}
```

#### `warn`

An advisory (e.g. a deprecation note).

```json
{"v":1,"ts":"…","type":"warn","msg":"…"}
```

#### `epilogue`

The post-success "what you actually need" block, as data (the `✔ cube …
is up` headline plus key–value rows in a terminal run). The `✔` glyph is
presentation — renderers add it; the event never carries it (ratified R2).

```json
{"v":1,"ts":"…","type":"epilogue","cube":"dev","gateway_url":"https://cube-idp.localtest.me:8443","context":"kind-dev","registry":"zot.cube-idp-system:5000","hint":"credentials: cube-idp get secrets"}
```

| Field | Type | Meaning |
|---|---|---|
| `cube` | string | `metadata.name` from `cube.yaml`. |
| `gateway_url` | string | The gateway URL the cube serves. |
| `context` | string | The kubeconfig context. **Omitted** when the producer does not know it. |
| `registry` | string | The in-cluster registry address. **Omitted** when unknown. |
| `hint` | string | The `next:`/credentials hint line. |

#### `access`

The post-`up` "here's what you just got" summary — the delivered packs'
URLs and the credentials hint, as data.

```json
{"v":1,"ts":"…","type":"access","packs":[{"name":"gitea","urls":["https://gitea.cube.local:8443"]}],"hint":"credentials: cube-idp get secrets"}
```

| Field | Type | Meaning |
|---|---|---|
| `packs` | array | One entry per pack: `name` (string), `urls` (array of strings). |
| `hint` | string | The closing hint line. |

#### `run_done`

Closes a run. On failure it is emitted immediately **before** `diagnosis`.

```json
{"v":1,"ts":"…","type":"run_done","ok":false,"dur_ms":123456}
```

| Field | Type | Meaning |
|---|---|---|
| `ok` | bool | Whether the run succeeded. |
| `dur_ms` | number | Total run duration in milliseconds. |

#### `diagnosis`

Always the **last** event on a failed run — the machine-readable form of
the `✗ CUBE-…` block (which is still printed, human-readable, to stderr).

```json
{"v":1,"ts":"…","type":"diagnosis","code":"CUBE-3004","summary":"engine components not ready","cause":"context deadline exceeded","remediation":"re-run `cube-idp up`; inspect the components with kubectl","raw":"CUBE-3004: engine components not ready: context deadline exceeded"}
```

| Field | Type | Meaning |
|---|---|---|
| `code` | string | The typed `CUBE-xxxx` error code. **Omitted** for untyped errors. |
| `summary` | string | One-line summary. Omitted for untyped errors. |
| `cause` | string | The underlying cause. **Omitted** when the error has no distinct cause. |
| `remediation` | string | Copy-pasteable fix. Omitted for untyped errors. |
| `raw` | string | The full `error.Error()` text. **Always present** — the fallback for untyped errors. |

#### `encode_error`

Not a run event — an envelope-level escape hatch. Every other line above is
built as a typed Go struct and marshaled; if that marshal itself ever fails
(a bug in this package, since every field is plain data), the renderer
still owes the stream *something* rather than silently dropping the event
and leaving a consumer's step-tree stuck open forever. `encode_error`
is that fallback: written directly with `fmt.Fprintf`, bypassing
`json.Marshal` entirely, so it cannot itself fail to encode.

```json
{"v":1,"type":"encode_error","error":"…"}
```

| Field | Type | Meaning |
|---|---|---|
| `error` | string | The marshal error's `Error()` text. |

**No `ts` field** — unlike every event above, `encode_error` is NOT built
from `jsonHead{v, ts, type}`; it is a literal `Fprintf` format string
(`internal/ui/render/json.go`) carrying only `v` and `type`. Consumers
must not assume every line on the stream carries `ts`.

---

## 2. Documents (`--output json`)

Request/response commands answer once, so they emit a single pretty-printed
JSON document on stdout (the `gh` convention) instead of a stream. Every
document carries `"v": 1` (same versioning policy as the stream). Exit-code
behavior is identical to text mode.

### `cube-idp status --output json`

```json
{
  "v": 1,
  "cube": "dev",
  "components": [
    {"name": "cube-idp-traefik", "ready": true, "message": ""}
  ],
  "spokes": [
    {"name": "staging", "provider": "kind", "registered": true, "reachable": false}
  ],
  "inventory": {
    "count": 42,
    "objects": [
      {"kind": "ConfigMap", "namespace": "default", "name": "app-config"}
    ]
  },
  "ready": true
}
```

| Field | Type | Meaning |
|---|---|---|
| `cube` | string | `metadata.name` from `cube.yaml`. |
| `components` | array | Engine-reported health: `name`, `ready`, `message`. |
| `spokes` | array | One row per declared spoke (Phase 5): `name`, `provider`, `registered` (the hub registration secret exists), `reachable` (the spoke API server answered `/readyz` using that secret's payload, probed from the CLI's machine — kind spokes carry a docker-network-internal URL, so the hub engine may reach them when this probe cannot). **Additive; only present when `spec.spokes` is non-empty.** |
| `inventory.count` | number | Objects tracked in the cube's inventory. |
| `inventory.objects` | array | `kind`/`namespace`/`name` rows, sorted. **Only present with `--details`.** |
| `ready` | bool | Overall verdict; `false` also makes the command exit 1 (CUBE-3004). |

### `cube-idp doctor --output json`

```json
{
  "v": 1,
  "findings": [
    {
      "code": "CUBE-0102",
      "severity": "error",
      "message": "port 8443 is already in use",
      "remediation": "if this is not cube-idp's gateway, stop whatever binds port 8443 or change spec.gateway.port"
    }
  ],
  "errors": true
}
```

| Field | Type | Meaning |
|---|---|---|
| `findings` | array | Every finding: `code` (`CUBE-xxxx`), `severity` (`error` \| `warning` \| `info`), `message`, `remediation`. `[]` (never `null`) when clean. |
| `errors` | bool | Whether any finding is an error; `true` also makes the command exit 1. |

The findings array is designed for CI annotation: each entry carries the
typed code and severity a PR annotator needs.

### `cube-idp get secrets --output json`

```json
{
  "v": 1,
  "secrets": [
    {
      "pack": "gitea",
      "namespace": "gitea",
      "name": "gitea-admin-cube-idp",
      "fields": {"username": "gitea_admin", "password": "…"}
    },
    {
      "pack": "argocd",
      "namespace": "argocd",
      "name": "argocd-initial-admin-secret",
      "placeholder": "<secret argocd/argocd-initial-admin-secret not found>"
    }
  ],
  "notes": ["note: …legacy label deprecation…"]
}
```

| Field | Type | Meaning |
|---|---|---|
| `secrets` | array | One row per surfaced credential: `pack`, `namespace`, `name`, and either `fields` (flattened key→value map) or `placeholder` (set when the pack's `authSecretRef` points at a Secret that doesn't exist yet). |
| `notes` | array | Legacy-label deprecation notes, as data. **Omitted** when empty. |
