# traefik starter pack

cube-idp's default gateway implementation (Gateway API, not classic
Ingress). Renders:

- `manifests/00-gateway-api-crds.yaml` — the Gateway API **standard**
  channel CRDs, vendored from
  [kubernetes-sigs/gateway-api v1.5.1](https://github.com/kubernetes-sigs/gateway-api/releases/tag/v1.5.1).
  Traefik v3.7.6 (this pack's app version) documents conformance against
  Gateway API Standard v1.5.1 specifically — v1.5.1 was pinned instead of
  the newer v1.6.0 GA release to match Traefik's own documented/tested
  compatibility rather than chasing latest. Note: the bundle's two
  `ValidatingAdmissionPolicy`/`Binding` objects carry a
  `gateway.networking.k8s.io/bundle-version: v1.5.0-dev` annotation while
  all eight CRDs say `v1.5.1` — that's how the upstream v1.5.1 release
  artifact ships (a release-tagging quirk in their VAP template), expected
  and harmless.
- `manifests/10-gateway.yaml` — a `Gateway` named `cube-idp` in the
  `traefik` namespace, one `web` listener on port 8000, `gatewayClassName:
  traefik` (the `GatewayClass` the traefik chart creates by default).
- `chart.yaml` — the `traefik/traefik` helm chart (pinned `41.0.2`, app
  `v3.7.6`; the task brief's `34.1.0` pin was stale — this pack tracks the
  current stable chart release as of 2026-07-13).

## Port wiring (host 8443 -> node 30080 -> traefik web)

Phase 1 serves plain HTTP behind the host port; TLS via `cube-idp trust` is
a Phase 2 concern (spec D6). To keep that simple, this pack exposes
traefik's `web` entrypoint as a **fixed NodePort 30080** rather than a
`LoadBalancer` Service (chart values: `ports.web.nodePort: 30080`,
`service.spec.type: NodePort`).

`internal/cluster/kindp/merge.go`'s `gatewayContainerPort` constant maps the
kind cluster's host port (`spec.gateway.port`, default 8443) to
**containerPort 30080** — i.e. kind's docker port-forward lands directly on
the node port traefik's Service listens on. The chain is:

```
host:8443 --(kind extraPortMapping)--> node:30080 --(NodePort Service)--> traefik pod:8000 (web entrypoint)
```

No `hostPort` on the traefik pod, no LoadBalancer controller (e.g.
cloud-provider-kind) required — a plain NodePort is enough because kind
already forwards the host port straight to the node's containerPort.

## What's disabled and why

- `gateway.enabled: false` — the chart can deploy its own default `Gateway`
  when `providers.kubernetesGateway.enabled` is on; disabled here so it
  doesn't create a second, conflicting `Gateway` alongside
  `manifests/10-gateway.yaml` (`cube-idp`).
- `gatewayClass.enabled` is left at its default (`true`) because our
  `Gateway` manifest references `gatewayClassName: traefik`, which only
  exists if the chart creates it.
- The chart's `ingressRoute.dashboard`/`ingressRoute.healthcheck` extras are
  already disabled by chart default and are left that way — classic
  Traefik `IngressRoute` CRDs are not part of this pack's Gateway API path.

## Verification method

Chart values were verified against `helm show values
traefik/traefik --version 41.0.2` and a real `helm template` render (not
guessed): confirmed `GatewayClass` name `traefik`, `Service.spec.type:
NodePort` with `nodePort: 30080` on the `web` port, no duplicate `Gateway`
object, and that a top-level `replicas` key is rejected by the chart's
`values.schema.json` (hence `pack.cue`'s `#Values` nests it under
`deployment.replicas`, matching the chart's real key).
