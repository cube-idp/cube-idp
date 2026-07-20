---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 13. Spoke Clusters: Declaration, Bootstrap, and Registration-Only Support

## Context and Problem Statement

A cube manages one hub cluster running a GitOps engine (flux or argocd). Real platforms
need more than one cluster — a staging cluster, a workload cluster, a cluster someone else
already runs. The question is how far cube-idp should go on those secondary clusters.

Two extremes were available. Deliver packs to every cluster, which would make cube-idp a
multi-cluster delivery system duplicating what the GitOps engine already does. Or ignore
secondary clusters entirely, leaving operators to hand-craft cluster credentials and hub
registration secrets for whichever engine they picked. The first bloats scope and creates
two competing delivery paths; the second leaves the hardest, most error-prone step — RBAC,
token minting, and the engine's undocumented registration Secret shape — as manual work.

Secondary clusters are called *spokes*. This record fixes what a spoke is, how it is
declared, what cube-idp does to it, and where the boundary with the engine sits.

## Decision

Spokes are declarative first-class `cube.yaml` content under `spec.spokes[]` with `name`
and `cluster` fields. Names match `^[a-z0-9][a-z0-9-]{0,30}$` and are unique within a cube;
a spoke with provider `existing` must set `cluster.context`. Violations are the typed error
CUBE-8001.

Spokes support only the `kind` and `existing` providers — a k3d spoke is rejected with
CUBE-8001, though a k3d hub remains fully supported. A cube-created kind spoke is named
`<cube-name>-spoke-<spoke-name>`, maps no host ports (the hub owns host port mappings), and
takes its hub-facing API URL from kind's internal kubeconfig
(`https://<cluster>-control-plane:6443` on the shared kind docker network). For `existing`
spokes the registered URL is the connection's own server URL, and reachability is the
operator's responsibility.

**Spoke support is registration-only.** Bootstrap idempotently server-side-applies
namespace `cube-idp-system`, ServiceAccount `cube-idp-<engine>`, and ClusterRoleBinding
`cube-idp-<engine>-admin` bound to `cluster-admin`; mints a long-lived TokenRequest token;
and registers the spoke with the hub engine as a single Secret named
`cube-idp-spoke-<name>` in that engine's own native form — argocd: in the `argocd`
namespace, labelled `argocd.argoproj.io/secret-type: cluster`; flux: in `flux-system` with
key `value` holding a kubeconfig, referenced by `Kustomization.spec.kubeConfig.secretRef`.
Then it exits. No controller, CRD or daemon is installed on a spoke and no packs are
delivered to it; workload delivery to spokes is user-authored engine content.

`spoke add` and `spoke remove` only declare and undeclare. Bootstrap, registration and
registration-secret pruning happen on the next `up`; a single spoke failure aborts `up`
loudly and re-running `up` is the retry path. Registered spokes surface in `status`,
`spoke list`, inventory and doctor, with reachability probed in parallel on a 2-second
timeout and unreachability reported as the warning CUBE-8006, never an error.

Engine parity for spoke registration is proven in CI by an end-to-end matrix crossing local
providers with both engines; that same matrix is the parity evidence required when engine
chart or manifest pins are bumped.

## Consequences

* Good, because the boundary is sharp and testable: cube-idp owns credentials and
  registration, the engine owns delivery. There is exactly one delivery path in the system.
* Good, because the engine-native Secret shape means a registered spoke is indistinguishable
  from one an operator registered by hand — no cube-idp-specific runtime on the spoke, and
  nothing to uninstall beyond a namespace and a ClusterRoleBinding.
* Good, because everything is declarative: spokes round-trip through `cube.yaml`, and `up`
  is idempotent and re-entrant, so retry is always "run `up` again".
* Bad, because `cluster-admin` is a blunt grant — the engine service account has full rights
  on every spoke.
* Bad, because a ~10-year TokenRequest token is long-lived credential material sitting in a
  hub Secret; the mitigation is that every `up` re-issues it.
* Bad, because kind spokes register a docker-network-internal URL, so a spoke the hub can
  reach may still probe unreachable from the operator's machine — hence reachability is a
  warning, not an error.
* Bad, because k3d spokes are unsupported, so a k3d-hub user must mix providers to get a
  spoke.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `spec.spokes` is a list of `{name, cluster}` entries; names match `^[a-z0-9][a-z0-9-]{0,30}$`, are unique within a cube, and provider `existing` requires `cluster.context` — violations are CUBE-8001. | `internal/config/load.go:252-275`, `internal/config/schema.cue:41-43` |
| Spokes support only providers `kind` and `existing`; a k3d spoke is rejected with CUBE-8001 while a k3d hub stays supported. | `internal/config/schema.cue:45`, `internal/config/load.go:252-275` |
| Spokes are declarative `cube.yaml` content; `spoke add` only declares and `spoke remove` only undeclares, with bootstrap, registration and pruning deferred to the next `up`. | `cmd/spoke.go:44-52` |
| Spoke bootstrap creates namespace `cube-idp-system`, ServiceAccount `cube-idp-<engine>`, and ClusterRoleBinding `cube-idp-<engine>-admin` bound to `cluster-admin`. | `internal/spoke/bootstrap.go:23-67` |
| Spoke bootstrap is idempotent via server-side apply; bootstrap failures are CUBE-8002 and token-issuance failures are CUBE-8003. | `internal/spoke/bootstrap.go:66-92` |
| Spoke service-account tokens are minted via TokenRequest with a 10-year lifetime; cube-created spoke clusters are named `<cube>-spoke-<name>`. | `internal/spoke/bootstrap.go:26,84-85` |
| Spoke support is registration-only: apply RBAC, mint a token, hand the credential to the hub engine, then exit — no packs, no controller, no CRD, no daemon on the spoke. | `internal/up/up.go:694-729` |
| Spokes support both flux and argocd engines from day one, each registered as a single hub Secret. | `internal/spoke/register.go:36-73` |
| The hub-side registration Secret is named `cube-idp-spoke-<name>`, in namespace `argocd` labelled `argocd.argoproj.io/secret-type: cluster` for argocd, or `flux-system` with key `value` holding a kubeconfig for flux. | `internal/spoke/register.go:37` |
| Argocd registration carries server URL, bearer token and CA; flux registration is a kubeconfig Secret referenced by `Kustomization.spec.kubeConfig.secretRef`. | `internal/spoke/register.go:36-74` |
| A kind spoke's hub-facing API URL comes from kind's internal kubeconfig (`https://<cluster>-control-plane:6443`). | `internal/up/up.go:748-772` |
| For `existing` spokes the endpoint is the connection's own server URL and reachability is the operator's responsibility. | `internal/up/up.go:759-773` |
| Spoke cluster rendering maps no host ports — a zero gateway spec emits no `extraPortMappings`. | `internal/cluster/kindp/merge.go:103-139` |
| Failure of any single spoke aborts `up` loudly; re-running `up` is the retry path and re-issues tokens. | `internal/up/up.go:555-562` |
| Hub registration secrets are applied through the hub applier and recorded in inventory, so undeclaring a spoke plus `up` prunes them and `down` cascades. | `internal/up/up.go:718-726` |
| `spoke remove` leaves a kind spoke's cluster running unless `--delete-cluster` is passed, which requires confirmation or `--yes` and refuses non-interactively with CUBE-0010. | `cmd/spoke.go:170-207` |
| `down` deletes cube-created spoke clusters best-effort after hub teardown and merely deregisters `existing` spokes, naming the manual RBAC removal commands. | `cmd/down.go:134-152` |
| `cube-idp status` gains an additive top-level `spokes` array whose rows carry Registered and Reachable states as paired glyph+word cells. | `cmd/status.go:479-496` |
| A registered spoke appears in `status`, `spoke list`, the inventory record driving the `down` cascade, and doctor's checks. | `cmd/status.go:239-259,320-327` |
| Spoke reachability is probed in parallel on a 2-second per-spoke timeout using the hub secret's own credential; any probe error yields `Reachable=false` rather than failing the command. | `internal/doctor/doctor.go:274-318` |
| An unreachable spoke is the warning CUBE-8006 from doctor's `spoke-reachability` check, never an error. | `internal/doctor/doctor.go:328-350` |
| `spoke list` degrades gracefully when the hub is unreachable, printing the declared-config-only table with a trailing note instead of erroring. | `cmd/spoke.go:82-100` |
| CI proves engine parity for spoke registration with an end-to-end provider x engine matrix, which is also the parity evidence for engine pin bumps. | `.github/workflows/ci.yaml:27` |

### Verification

- [ ] `internal/config/schema.cue:45` pins the spoke provider enum to `*"kind" | "existing"` and excludes k3d.
- [ ] `internal/config/load.go:252-275` returns `diag.CodeSpokeProviderUnsupported` (CUBE-8001) for a duplicate spoke name, an `existing` spoke without `cluster.context`, and any provider outside kind/existing.
- [ ] `internal/spoke/bootstrap.go:43-63` builds exactly three objects — Namespace `cube-idp-system`, ServiceAccount `cube-idp-<engine>`, ClusterRoleBinding `cube-idp-<engine>-admin` with `roleRef` ClusterRole `cluster-admin` — and nothing else.
- [ ] `internal/spoke/register.go:36-74` returns exactly one Secret named `cube-idp-spoke-<name>` per engine and a CUBE-8005 error for any other engine type.
- [ ] `internal/up/up.go:694-729` (`ensureSpoke`) applies only to the hub applier; no apply call targets the spoke beyond `spoke.Bootstrap`.
- [ ] `internal/cluster/kindp/merge.go:107` guards all `extraPortMappings` injection behind `gw.Port > 0`, and `internal/up/up.go:698` passes a zero `config.GatewaySpec{}`.
- [ ] `internal/doctor/doctor.go:224` sets `spokeProbeTimeout = 2 * time.Second`, and `doctor.go:274-318` launches one goroutine per registered spoke.
- [ ] `internal/doctor/doctor.go:336,344` are the only `CodeSpokeUnreachable` emission sites and both use `diag.SeverityWarning`.
- [ ] `internal/diag/codes.go:173-178` defines CUBE-8001 through CUBE-8006 for the spoke range.
- [ ] `.github/workflows/ci.yaml:27-29` defines a provider x engine end-to-end matrix covering both flux and argocd.

## History

Two earlier formulations no longer hold.

k3d spokes were originally required to join a shared docker network, expressed as a
cluster-shape field with a recreate caveat. k3d spoke support was deferred entirely and a
k3d spoke is now rejected with CUBE-8001; only the kind server-URL rewrite
(`https://<name>-control-plane:6443` on the shared kind docker network) and the
`existing`-spoke doctor-probe halves of that decision survive.

The CI matrix was originally specified as `{kind} x {flux, argocd} x {up, add, diff,
upgrade, down}` plus an `existing`-provider smoke test against a k3s container, with
sub-minute `up` tracked as a CI metric. It has since been replaced by the `{kind, k3d} x
{flux, argocd}` matrix in `.github/workflows/ci.yaml`; there is no `existing`-provider k3s
smoke test and no sub-minute `up` metric. The engine axis, and its role as parity evidence
for engine pin bumps, is the part that survived.

A long-lived `kubernetes.io/service-account-token` Secret was originally specified as the
spoke credential; the implementation mints a TokenRequest token instead, re-issued on every
`up`.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:827` — registration-only scope boundary
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:447` — `spec.spokes[]` schema and CUBE-8001
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:162` — spoke bootstrap RBAC objects
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:263` — per-engine hub registration
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:175` — CI engine parity matrix (superseded form)
