#!/usr/bin/env bash
# Regenerates the embedded Flux install manifests (spec: pre-rendered at
# build time so the runtime needs no external binaries, works offline).
#
# Requires the flux CLI: https://fluxcd.io/flux/installation/
#   brew install fluxcd/tap/flux
#
# `go run github.com/fluxcd/flux2/v2/cmd/flux@latest install --export ...`
# does NOT work as a substitute: flux2's go.mod carries `replace` directives,
# and `go run`/`go install` on a module@version refuse to build a main
# package from a module whose go.mod has replace directives pointing
# outside itself. The flux CLI binary must be installed some other way
# (brew, a release tarball, or `git clone` + `go build` inside the module).
#
# Only source-controller (OCIRepository) and kustomize-controller
# (Kustomization apply) are required — helm rendering is client-side
# (pack.Render), so helm-controller stays out.
set -euo pipefail
cd "$(dirname "$0")/.."

command -v flux >/dev/null || {
  echo "flux CLI not found; install it first: brew install fluxcd/tap/flux" >&2
  exit 1
}

flux install --export \
  --components=source-controller,kustomize-controller \
  > internal/engine/flux/manifests/install.yaml

echo "wrote internal/engine/flux/manifests/install.yaml (flux $(flux version --client 2>/dev/null || true))"
