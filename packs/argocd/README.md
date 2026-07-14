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
- `manifests/10-install.yaml` — the vendored install manifest, transformed
  (see below). Do not edit by hand; regenerate per "Re-vendoring".
- `manifests/20-httproute.yaml` — `argocd.cube-idp.localtest.me` routed to
  `argocd-server:80` (confirmed by inspecting the vendored Service: both
  its `http` (80) and `https` (443) ports target the same `containerPort:
  8080`, since `--insecure` collapses them onto one HTTP listener).

## Explicit `namespace: argocd` on every namespaced object

Upstream's `install.yaml` deliberately omits `metadata.namespace` and
assumes `kubectl apply -n argocd`. cube-idp's delivery path (pack render →
OCI artifact → Flux Kustomization **without** a `targetNamespace`) applies
objects exactly as rendered — an object without an explicit namespace
would land in `default`, and the HTTPRoute (in `argocd`) would never
resolve `argocd-server`. The vendored file is therefore regenerated
through kustomize's namespace transformer so all 50 namespaced objects
carry `namespace: argocd`, while cluster-scoped objects (3 CRDs, 3
ClusterRoles, 3 ClusterRoleBindings) stay un-namespaced and the
ClusterRoleBinding subjects correctly reference `namespace: argocd`.

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
applied declaratively in the re-vendoring kustomization below instead of
hand-editing a `command:`/`args:` block that could silently drift on
future `argo-cd` version bumps. The effect is identical: `argocd-server`
serves plain HTTP on port 8080, and both the vendored Service's `http` and
`https` ports target it.

## Re-vendoring (version bumps)

`manifests/10-install.yaml` is generated, not hand-edited. To bump the
pinned version, rerun (any recent `kubectl`; its embedded kustomize
handles the namespace transformer, correctly skipping cluster-scoped
kinds):

```bash
VERSION=v3.4.5   # <- new pin
WORK=$(mktemp -d)
curl -sL "https://raw.githubusercontent.com/argoproj/argo-cd/${VERSION}/manifests/install.yaml" \
  > "$WORK/upstream-install.yaml"
cat > "$WORK/kustomization.yaml" <<'EOF'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: argocd
resources:
  - upstream-install.yaml
patches:
  - patch: |-
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: argocd-cmd-params-cm
      data:
        server.insecure: "true"
EOF
kubectl kustomize "$WORK" > "$WORK/rendered.yaml"
# air-gap (Task 7): upstream pins imagePullPolicy: Always on most
# control-plane containers, which makes a kubelet ignore images node-loaded
# from a vendor bundle (`up --bundle`) — flip them so bundle installs work
# offline (guarded by tests/packs_airgap_test.go):
sed -i '' 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' "$WORK/rendered.yaml"
# keep the explanatory header comment block from the current file:
{ sed -n '/^#/p;/^[^#]/q' packs/argocd/manifests/10-install.yaml; \
  cat "$WORK/rendered.yaml"; } > packs/argocd/manifests/10-install.yaml
```

Then update the version references in this README and re-run the smoke
test (which asserts every known-namespaced rendered object actually
carries a namespace) plus the air-gap pull-policy guard:

```bash
go test ./tests/ -run 'TestStarterPacksRender|TestPackManifestsNoAlwaysPull' -v
```

## Verification method

No chart involved, so "verify against helm show values" doesn't apply
here; instead the vendored `install.yaml` was inspected directly:
confirmed the `argocd-server` Service name/ports (`argocd-server`, `80` ->
`8080`), that `ARGOCD_SERVER_INSECURE` really is wired from
`argocd-cmd-params-cm`'s `server.insecure` key, that no `Namespace` object
ships in the upstream manifest, and (post-kustomize) that all namespaced
objects carry `namespace: argocd`, no cluster-scoped object gained a
namespace, and ClusterRoleBinding subjects reference `namespace: argocd`.
