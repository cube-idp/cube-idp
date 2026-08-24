GO ?= go
FLUX_VERSION ?= v2.9.2

.PHONY: build test test-e2e generate lint filelen flux-manifests

build:
	CGO_ENABLED=0 $(GO) build -o cube-idp ./cmd/cube-idp

test:
	$(GO) vet ./...
	$(GO) test ./... -count=1

# Real kind conformance — needs Docker/Podman; never part of the green gate.
# KUBECONFIG stays inside the worktree per CLAUDE.md §7.
test-e2e:
	CUBE_E2E=1 KUBECONFIG=$(CURDIR)/.kube/config $(GO) test ./internal/cluster/kind/... ./tests/... -count=1 -timeout 20m -v

generate:
	$(GO) tool controller-gen object paths=./api/config/v1alpha1

# Regenerate the vendored Flux install manifests embedded by the substrate
# pack (internal/engine/substrate). Requires the flux CLI at $(FLUX_VERSION) —
# upstream's v-prefixed spelling of substrate.Version — since a different
# version yields different bytes and fails the provenance test. After running,
# paste the printed sha256 into manifestsSHA256
# (internal/engine/substrate/substrate.go); Version and the pack.cue version
# must equal $(FLUX_VERSION) without the v prefix. Kept out of `generate`
# (needs the external flux CLI).
flux-manifests:
	@have=$$(flux version --client 2>/dev/null | awk '/flux:/{print $$2}'); \
	if [ "$$have" != "$(FLUX_VERSION)" ]; then \
		echo "flux CLI is $$have, need $(FLUX_VERSION) — install the pinned version first"; exit 1; \
	fi
	flux install --export --components=source-controller,kustomize-controller,helm-controller \
		> internal/engine/substrate/manifests/flux.yaml
	@echo "regenerated internal/engine/substrate/manifests/flux.yaml — update manifestsSHA256 to:"
	@shasum -a 256 internal/engine/substrate/manifests/flux.yaml | awk '{print "  "$$1}'

lint: filelen
	golangci-lint run ./...

# Files stay under 300 lines (generated code exempt) — design §7.
filelen:
	@bad=$$(find . -name '*.go' -not -name 'zz_generated*' -not -path './.git/*' \
		| xargs wc -l | awk '$$1 > 300 && $$2 != "total" {print $$2" ("$$1" lines)"}'); \
	if [ -n "$$bad" ]; then \
		echo "files exceed 300 lines:"; echo "$$bad"; exit 1; \
	fi
