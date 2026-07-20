# How the e2e suite finds packs

The packs no longer live in this repository: they live in the public
monorepo [cube-idp/packs](https://github.com/cube-idp/packs) and publish to
`oci://ghcr.io/cube-idp/packs/<name>`. The e2e suite therefore no longer
resolves anything from a repo-relative `packs/` tree.

## Hermetic legs (default): a local packs checkout

Every `init --local`-driven leg (`TestUpStatusDown`, the provider/engine grid suite,
`TestSpokeKindRegistration`) resolves pack directories from a local
cube-idp/packs checkout via `packsCheckout` (e2e_test.go):

- `CUBE_IDP_E2E_PACKS_DIR` — points at the checkout's `packs/` directory
  (any path shape; the suite passes its parent to `init --local`).
- Unset, it defaults to the sibling checkout `../cube-idp-packs/packs`
  (relative to this repo's root).
- When the directory is missing the affected tests **skip** with this hint:

```sh
git clone https://github.com/cube-idp/packs ../cube-idp-packs
# or:
CUBE_IDP_E2E_PACKS_DIR=/path/to/packs-checkout/packs \
  CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 35m
```

CI checks out cube-idp/packs next to the workspace and sets
`CUBE_IDP_E2E_PACKS_DIR` explicitly (`.github/workflows/ci.yaml`).

The pack-content smoke tests in `tests/` (`packs_render_test.go`,
`packs_airgap_test.go`) share the same knob and default via `packsTree`,
and skip identically when no checkout is present — the authoritative
per-pack gate is the packs repo's own conformance harness.

Local note: a dev machine may already bind 8443 — export
`CUBE_IDP_E2E_GATEWAY_PORT=18443` for any local run.

## Online leg: digest-pinned published packs

`TestPublishedPacksByDigest` proves the standalone contract against the
REAL registry, pinned by digest, never by mutable tag. It is doubly gated:

1. `CUBE_IDP_E2E_ONLINE=1` — it pulls from ghcr.io (network + docker).
2. `tests/e2e/packs.lock` — a committed JSON map of digest-pinned refs,
   seeded by the owner after each publish; while absent the test skips.

```json
{
  "traefik": "oci://ghcr.io/cube-idp/packs/traefik@sha256:…",
  "gitea": "oci://ghcr.io/cube-idp/packs/gitea@sha256:…"
}
```

To (re)seed after a publish, take the digests from the publish workflow's
output (`published <name>:<version> @ sha256:…`), or resolve them from the
registry with the `cube-idp pack` toolchain:

```sh
cube-idp pack index build /path/to/packs-checkout/packs -o /tmp/index.json --from-registry
# each entry's "digest" field pairs with its "ref" minus the tag
```

Every ref MUST carry `@sha256:` — the test rejects tag-pinned entries.
