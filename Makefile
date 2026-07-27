GO ?= go

.PHONY: build test generate lint filelen

build:
	CGO_ENABLED=0 $(GO) build -o cube-idp ./cmd/cube-idp

test:
	$(GO) vet ./...
	$(GO) test ./... -count=1

generate:
	$(GO) tool controller-gen object paths=./api/config/v1alpha1

lint: filelen
	golangci-lint run ./...

# Files stay under 300 lines (generated code exempt) — design §7.
filelen:
	@bad=$$(find . -name '*.go' -not -name 'zz_generated*' -not -path './.git/*' \
		| xargs wc -l | awk '$$1 > 300 && $$2 != "total" {print $$2" ("$$1" lines)"}'); \
	if [ -n "$$bad" ]; then \
		echo "files exceed 300 lines:"; echo "$$bad"; exit 1; \
	fi
