name:    "envoy-gateway"
version: "0.1.0"
#Values: {}

// D14 (Owner Decisions #3): envoy-gateway's controller spawns Envoy proxy
// pods at Gateway-attach time — those pods' image never appears in this
// pack's rendered manifests (helm template output), so it's declared here
// for Task 6's prep step to pull/mirror. RECONCILE: pin to the proxy image
// the pinned gateway-helm chart (chart.yaml, version 1.3.0) actually
// defaults to — verify once the chart is reachable (network was
// unavailable while authoring this pack; see README.md).
images: ["docker.io/envoyproxy/envoy:distroless-v1.33.0"]
