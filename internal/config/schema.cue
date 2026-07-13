package config

#Cube: {
	apiVersion: "cube-idp.dev/v1alpha1"
	kind:       "Cube"
	metadata: name: =~"^[a-z0-9][a-z0-9-]{0,30}$"
	spec: {
		cluster: {
			provider:           *"kind" | "existing"
			context?:           string
			kubernetesVersion:  *"v1.33.1" | string
			extraPorts?: [...{hostPort: int & >0 & <65536, nodePort: int & >0 & <65536}]
			registry?: {mirrors?: {[string]: string}, insecure?: [...string]}
			mounts?: [...{hostPath: string, nodePath: string}]
			providerConfig?: string
		}
		engine: type: *"flux" | "argocd"
		gateway: {
			pack: *"traefik" | string
			host: *"cube-idp.localtest.me" | string
			port: *8443 | (int & >0 & <65536)
		}
		packs?: [...{ref: string & !="", values?: {...}}]
	}
}
