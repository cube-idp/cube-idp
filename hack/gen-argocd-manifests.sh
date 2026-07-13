#!/usr/bin/env bash
# Regenerates the embedded Argo CD install manifests (pre-rendered at build
# time: no external binaries at runtime, works offline — same posture as
# hack/gen-flux-manifests.sh).
#
# ARGOCD_VERSION is pinned to the latest stable Argo CD 3.x release with
# first-class OCI repository support (native `spec.source.repoURL: oci://...`
# application sources for plain-manifest artifacts, not just Helm charts —
# see https://argo-cd.readthedocs.io/en/stable/user-guide/oci/, present since
# well before 3.4). v3.4.5 was the latest stable release (3.5 was still at
# -rc2) at the time this was pinned; re-run with a newer ARGOCD_VERSION and
# re-verify the repository-secret field names in
# manifests/repo-secret.yaml against
# https://github.com/argoproj/argo-cd/blob/<version>/docs/operator-manual/upgrading/
# before bumping across a minor version boundary (field semantics have
# changed there before, e.g. insecureOCIForceHttp's interaction with Helm
# v4 in the 3.4->3.5 upgrade notes).
set -euo pipefail
cd "$(dirname "$0")/.."
ARGOCD_VERSION="${ARGOCD_VERSION:-v3.4.5}"
OUT=internal/engine/argocd/manifests/install.yaml
{
  printf 'apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argocd\n---\n'
  curl -fsSL "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
} > "$OUT"
echo "wrote $OUT (argo-cd ${ARGOCD_VERSION})"
