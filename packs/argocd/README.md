# argocd starter pack

Argo CD, vendored as the upstream non-HA `install.yaml` (no chart —
Argo CD doesn't ship an official helm chart for the core install; this pack
is data-only per Task 12's constraints). Pinned to
[`v3.4.5`](https://github.com/argoproj/argo-cd/releases/tag/v3.4.5), the
current stable release as of 2026-07-13 (the brief's `v2.13.3` pin
predates the v3 major and is stale).

Contents:

- `manifests/00-namespace.yaml` — the vendored `install.yaml` does not
  create its own `Namespace` (upstream expects the caller to), so this pack
  provides one explicitly.
- `manifests/10-install.yaml` — the vendored install manifest, patched (see
  below).
- `manifests/20-httproute.yaml` — `argocd.cube-idp.localtest.me` routed to
  `argocd-server:80` (confirmed via `helm template`-equivalent inspection
  of the vendored Service: both its `http` (80) and `https` (443) ports
  target the same `containerPort: 8080`, since `--insecure` collapses them
  onto one HTTP listener).

## `--insecure` — HTTP behind the gateway, and how it's wired

Phase 1 serves plain HTTP behind cube-idp's gateway; TLS/`cube-idp trust`
is a Phase 2 concern (D6). argocd-server normally redirects HTTP to HTTPS
and serves its own self-signed cert, which would loop behind a
Gateway-API HTTPRoute that only listens on HTTP.

The brief's literal instruction was to patch the `argocd-server`
Deployment's container **args** directly. Instead this pack patches the
`argocd-cmd-params-cm` ConfigMap (`data: {server.insecure: "true"}`) —
inspecting the vendored manifest shows the `argocd-server` container
already sources an `ARGOCD_SERVER_INSECURE` env var from that exact
ConfigMap key (`optional: true`, i.e. absent = disabled). That's Argo CD's
own documented mechanism for `--insecure` (see
[`argocd-cmd-params-cm` docs](https://argo-cd.readthedocs.io/en/stable/operator-manual/argocd-cmd-params-cm.yaml/)),
and it's a 6-line diff against the vendored file instead of hand-editing a
`command:`/`args:` block that could silently drift on future `argo-cd`
version bumps. The effect is identical: `argocd-server` serves plain HTTP
on port 8080, and both the vendored Service's `http` and `https` ports
target it.

## Verification method

No chart involved, so "verify against helm show values" doesn't apply
here; instead the vendored `install.yaml` was inspected directly
(`grep`/`Read`) to confirm: the `argocd-server` Service name/ports
(`argocd-server`, `80` -> `8080`), that `ARGOCD_SERVER_INSECURE` really is
wired from `argocd-cmd-params-cm`'s `server.insecure` key, and that no
`Namespace` object ships in the upstream manifest.
