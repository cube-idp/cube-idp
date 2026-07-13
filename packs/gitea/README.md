# gitea starter pack

cube-idp's default in-cluster git server. Pinned to `gitea-charts/gitea`
`12.6.0` (app `1.26.1`), reachable at `gitea.cube-idp.localtest.me` via the
`traefik` pack's `cube-idp` Gateway (`manifests/20-httproute.yaml`, backend
`gitea-http:3000` — the chart's actual HTTP Service name/port, confirmed
with `helm template`).

## Fixed dev credentials (D9) — local dev only, do not reuse

`chart.yaml` sets `gitea.admin.username: gitea_admin` /
`gitea.admin.password: cube-idp-dev`, matching
`manifests/10-secret.yaml`'s `gitea-admin-cube-idp` Secret (labeled
`cube-idp.dev/cli-secret: "true"`, `cube-idp.dev/pack-name: gitea` so
`cube-idp get secrets` can surface it). This mirrors idpbuilder's
`--dev-password` posture: acceptable for a disposable local kind cluster,
**never** acceptable for anything reachable outside localhost. There is no
per-cluster credential randomization in Phase 1 — every `cube-idp up` with
this pack gets the same password.

## Lightweight single-pod database/cache, not the chart's HA default

The chart's default `values.yaml` enables `postgresql-ha` (multi-pod, needs
pgpool + several replicas) and a `valkey-cluster` (also multi-pod), both
with persistence — infrastructure meant for production HA, not a
single-node kind cluster. `chart.yaml` instead uses the chart's own
documented "minimal DEV installation" combination: `gitea.config.database.
DB_TYPE: sqlite3`, in-memory session/cache, `level` queue, all HA subcharts
and persistence disabled. Data does not survive `cube-idp down` — that's
expected for a local dev pack; a persistent variant can layer on top later
via user-supplied pack values if needed.
