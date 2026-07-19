package config

#Cube: {
	apiVersion: "cube-idp.dev/v1alpha1"
	kind:       "Cube"
	metadata: name: =~"^[a-z0-9][a-z0-9-]{0,30}$"
	spec: {
		cluster: {
			provider: *"kind" | "existing" | "k3d"
			context?: string
			// No CUE default: for provider "existing" an explicit version is a
			// node-creation field and must be rejected (CUBE-1003, spec §4.1);
			// Load fills "v1.33.1" for provider "kind" after decode.
			kubernetesVersion?: string
			extraPorts?: [...{hostPort: int & >0 & <65536, nodePort: int & >0 & <65536}]
			registry?: {mirrors?: {[string]: string}, insecure?: [...string]}
			mounts?: [...{hostPath: string, nodePath: string}]
			providerConfigRef?: string & !=""
			forProvider?: {...}
		}
		engine: {
			type: *"flux" | "argocd"
			// Engine pack source override (engine-as-pack spec §3.1); unset =
			// the published cube-engine-<type> default pinned in Go
			// (config.defaultEngineRefs).
			ref?: string & !=""
			// OPEN chart values (D3) — content validation is helm's, not CUE's.
			values?: {...}
			// GT16 (P8): opt-in engine self-management — unchanged.
			selfManage?: bool
		}
		gateway: {
			pack: *"traefik" | string
			host: *"cube-idp.localtest.me" | string
			port: *8443 | (int & >0 & <65536)
			// Opt-in plain-HTTP host port (U2, decision 3): mapped onto the
			// gateway packs' pinned HTTP NodePort 30080 only when set.
			httpPort?: int & >0 & <65536
			ref?: string & !=""
		}
		packs?: [...{ref: string & !="", valuesRef?: string & !="", values?: {...}, extraManifests?: string & !="", delivery?: "oci" | "repo", dependsOn?: [...string & !=""]}]
		spokes?: [...{
			name: =~"^[a-z0-9][a-z0-9-]{0,30}$"
			cluster: {
				provider: *"kind" | "existing"
				context?: string
				kubernetesVersion?: string
				// registry mirrors the hub cluster's field: Go's SpokeSpec
				// reuses ClusterSpec (S2/S3 hand it to cluster.New), whose
				// non-pointer Registry always marshals as `registry: {}` —
				// without this the spoke add/remove round-trip is
				// unwritable. extraPorts/mounts stay deliberately
				// disallowed for spokes in v1 (GT6); providerConfigRef/
				// forProvider are allowed — spoke parity is the point of
				// the forProvider design (spec 2026-07-18 §3).
				registry?: {mirrors?: {[string]: string}, insecure?: [...string]}
				providerConfigRef?: string & !=""
				forProvider?: {...}
			}
		}]
	}
}
