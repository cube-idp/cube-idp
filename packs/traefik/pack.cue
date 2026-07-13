name:    "traefik"
version: "0.1.0"
#Values: {
	// nested under "deployment" to match the traefik chart's actual values
	// key (top-level "replicas" is rejected by the chart's values.schema.json
	// — verified with `helm template` against traefik/traefik 41.0.2).
	deployment: replicas: int & >0 | *1
}
