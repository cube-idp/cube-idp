name:        "backstage"
version:     "0.1.0"
description: "developer portal"
#Values: {
	// nested under "backstage" to match the chart's actual values key
	// (templates/backstage-deployment.yaml reads .Values.backstage.replicas;
	// the root schema has no additionalProperties: false, so a top-level
	// "replicas" would silently no-op — same nesting convention and pitfall
	// as packs/traefik, verified against backstage/backstage 2.4.0).
	backstage: replicas: int & >0 | *1
}

// D11: no credential — backstage has no default admin login in this
// starter config, just the app URL. D15: ${GATEWAY_HOST} substitutes to the
// configured gateway's host[:port] (RenderFor); https is the canonical
// scheme (websecure listener, Phase 2 D6/D12).
expose: {
	urls: ["https://backstage.${GATEWAY_HOST}"]
}
