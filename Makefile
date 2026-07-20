BIN := cube-idp

build:
	CGO_ENABLED=0 go build -o $(BIN) .

test:
	go test ./...

truth-index:
	go run ./hack/truthindex -out hack/truth-index.json

truth-index-check:
	go run ./hack/truthindex -check

envtest-assets:
	go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path

test-apply:
	KUBEBUILDER_ASSETS=$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path) \
	go test ./internal/apply/ ./internal/engine/flux/ ./internal/up/ ./internal/syncer/ -v

test-engines:
	KUBEBUILDER_ASSETS=$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path) \
	go test ./internal/engine/... -v

.PHONY: build test truth-index truth-index-check envtest-assets test-apply test-engines
