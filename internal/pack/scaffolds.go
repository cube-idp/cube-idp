package pack

// namePlaceholder is what the templates below carry where the pack's name
// goes. A placeholder rather than a format verb keeps the templates readable
// as the files they become.
const namePlaceholder = "PACKNAME"

// scaffolds is the payload each pack type starts from. Every scaffold renders
// as written — that is the point of the command, and the tests assert it — so
// each carries a real object rather than a commented-out example.
//
// A helm pack is coordinates rather than content, so its scaffold has no
// payload at all and its chart block is a placeholder: the url is the one
// thing no scaffold can know. --from-chart fills the rest of that block in
// from a real chart; this is the same skeleton with nothing to read it from.
var scaffolds = map[Type]map[string]string{
	TypeRaw: {
		MetadataFile: `// ` + namePlaceholder + ` — scaffolded by cube-idp.
name:    "` + namePlaceholder + `"
version: "0.1.0"
type:    "raw"

// A raw pack renders the manifests under manifests/ as they are written.
// Values have no meaning for it: supplying any is a coded error, which is
// why this pack declares no #Values.
`,
		ManifestsDir + "/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + namePlaceholder + `
data:
  greeting: hello from ` + namePlaceholder + `
`,
	},
	TypeHelm: {
		MetadataFile: `// ` + namePlaceholder + ` — scaffolded by cube-idp.
name:    "` + namePlaceholder + `"
version: "0.1.0"
type:    "helm"

// A helm pack delegates: it carries the chart's coordinates and never the
// chart itself. Rendering it emits a Flux HelmRelease and its source CR.
//
// TODO: replace url, name, and version with the chart you mean to install.
// The url below is a placeholder under a reserved host, so this pack renders
// as written but installs nothing until you fill it in. For a chart published
// as an OCI artifact, replace the whole block with:
//   chart: {kind: "oci", url: "oci://<host>/<path>", version: "1.0.0"}
chart: {
	kind:    "repo"
	url:     "` + chartPlaceholderURL + `"
	name:    "` + namePlaceholder + `"
	version: "1.0.0"
}

// #Values is a closed definition: only the fields declared here may be
// supplied, and each one's *default fills in when it is not. Helm values are
// nested, and they reach the HelmRelease's spec.values exactly as written.
#Values: {
	replicaCount: int | *1
}
`,
	},
	TypeKustomize: {
		MetadataFile: `// ` + namePlaceholder + ` — scaffolded by cube-idp.
name:    "` + namePlaceholder + `"
version: "0.1.0"
type:    "kustomize"

// #Values is a closed definition: only the fields declared here may be
// supplied, and each one's *default fills in when it is not. A kustomize
// pack's values must be flat strings — they are substituted into ${...}
// references in the built output.
#Values: {
	greeting: string | *"hello"
}
`,
		"kustomization.yaml": `resources:
- configmap.yaml
`,
		"configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + namePlaceholder + `
data:
  greeting: ${greeting}
`,
	},
}
