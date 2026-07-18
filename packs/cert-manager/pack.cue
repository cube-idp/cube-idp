name:        "cert-manager"
version:     "0.1.0"
description: "TLS certificate automation"
#Values: {
	// top-level "replicaCount" is the chart's real values key (verified
	// against jetstack/cert-manager 1.16.3's values.schema.json — the brief's
	// "replicas" copy-paste from the traefik pack doesn't match this chart,
	// same class of pitfall traefik's own pack.cue documents: chart-specific
	// values.schema.json rejects unknown top-level keys).
	replicaCount: int & >0 | *1
}
