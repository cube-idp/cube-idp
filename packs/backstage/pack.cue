name:    "backstage"
version: "0.1.0"
#Values: {
	replicas: int & >0 | *1
}

// D11: no credential — backstage has no default admin login in this
// starter config, just the app URL. D15: ${GATEWAY_HOST} substitutes to the
// configured gateway's host[:port] (RenderFor); https is the canonical
// scheme (websecure listener, Phase 2 D6/D12).
expose: {
	urls: ["https://backstage.${GATEWAY_HOST}"]
}
