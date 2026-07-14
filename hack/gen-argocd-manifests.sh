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
#
# --check: regenerates into a temp file and diffs it against the committed
# $OUT instead of overwriting it; exits non-zero (and prints the diff) if
# the committed manifest has drifted from what a fresh regen would produce.
# CI-runnable: needs only curl/awk and network egress to raw.githubusercontent.com.
set -euo pipefail
cd "$(dirname "$0")/.."
ARGOCD_VERSION="${ARGOCD_VERSION:-v3.4.5}"
OUT=internal/engine/argocd/manifests/install.yaml
AWK_SCRIPT="$(dirname "$0")/inject-argocd-cmd-params.awk"

generate() {
  # The Namespace object argocd's install.yaml doesn't ship (it assumes the
  # namespace already exists) is prepended here; the reposerver.oci.layer
  # .media.types data key on argocd-cmd-params-cm (a cube-idp addition, see
  # inject-argocd-cmd-params.awk for the full rationale) is injected by the
  # awk filter so no hand-edit is needed after a regen.
  {
    printf 'apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argocd\n---\n'
    curl -fsSL "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
  } | awk -f "$AWK_SCRIPT"
}

if [[ "${1:-}" == "--check" ]]; then
  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' EXIT
  generate > "$tmp"
  if diff -u "$OUT" "$tmp"; then
    echo "$OUT is up to date with argo-cd ${ARGOCD_VERSION}"
    exit 0
  fi
  echo "$OUT is stale — run hack/gen-argocd-manifests.sh to regenerate" >&2
  exit 1
fi

generate > "$OUT"
echo "wrote $OUT (argo-cd ${ARGOCD_VERSION})"
