name:    "envoy-gateway"
version: "0.1.0"
#Values: {}

// D14 (Owner Decisions #3): envoy-gateway's controller spawns Envoy proxy
// pods at Gateway-attach time — those pods' image never appears in this
// pack's rendered manifests (helm template output), so it's declared here
// for Task 6's prep step to pull/mirror. The proxy image is compiled into
// the operator binary, not exposed as a chart value: this pin matches
// Envoy Gateway v1.3.0's DefaultEnvoyProxyImage constant
// (api/v1alpha1/shared_types.go in the github.com/envoyproxy/gateway
// v1.3.0 source) — re-verify against that constant on any chart bump.
images: ["docker.io/envoyproxy/envoy:distroless-v1.33.0"]

// Data-plane Service (Phase 4 R7b): a stable, NON-colliding name for the
// generated Envoy proxy Service, declared so `up` points the CoreDNS
// *.<host> rewrite at the DATA PLANE instead of the controller Service.
gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}
