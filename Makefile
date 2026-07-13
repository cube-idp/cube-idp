BIN := cube-idp

build:
	CGO_ENABLED=0 go build -o $(BIN) .

test:
	go test ./...

envtest-assets:
	go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path

test-apply:
	KUBEBUILDER_ASSETS=$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path) \
	go test ./internal/apply/ ./internal/engine/flux/ -v

.PHONY: build test envtest-assets test-apply
