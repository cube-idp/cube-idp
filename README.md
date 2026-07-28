# cube-idp

`cube-idp` is a single Go binary for standing up an internal developer
platform from one declarative config document.

**Status: v0 — config baseline.** The repository was reset to a greenfield
baseline on 2026-07-27; only the config domain exists today. The previous
implementation is preserved in git history on `main`. Structure and
rationale: [docs/design/2026-07-27-back-to-basics-structure.md](docs/design/2026-07-27-back-to-basics-structure.md).
What's next: [docs/plans/ROADMAP.md](docs/plans/ROADMAP.md).

## Build

```
make build          # → ./cube-idp
```

Requires Go 1.26+.

## Usage

Two commands exist, both operating on a `Config` document
(`cube-idp.dev/v1alpha1`):

```
$ cube-idp config validate -f examples/cube.yaml
config "dev" is valid

$ cube-idp config show -f examples/cube.yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
```

Exit codes: `0` valid, `2` config error (rendered as a `CUBE-CFG-NNN` code
with a remediation hint), `1` anything else.

Minimal config ([examples/cube.yaml](examples/cube.yaml)):

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
```

## Development

```
make test           # go vet + go test ./... -count=1
make lint           # golangci-lint (funlen 50) + 300-line file gate
make generate       # controller-gen deepcopy (output is committed)
```

Agent and contributor rules: [CLAUDE.md](CLAUDE.md). Release notes:
[CHANGELOG.md](CHANGELOG.md).
