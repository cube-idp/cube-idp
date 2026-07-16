package pack

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// RenderDirFor kustomize-builds dir and applies the D15 gateway substitution
// to the built YAML bytes BEFORE parsing — the same pre-parse byte-level
// substitute() the manifests/ walk and renderHelm already apply, closing the
// documented D15 asymmetry. A zero gw is the identity (byte-identical to the
// pre-R6 RenderDir output).
func RenderDirFor(dir string, gw config.GatewaySpec) ([]*unstructured.Unstructured, error) {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackKustomizeErr,
			fmt.Sprintf("kustomize render failed for %s", dir),
			"check kustomization.yaml; try `kubectl kustomize` on the directory to reproduce")
	}
	y, err := resMap.AsYaml()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackKustomizeErr,
			fmt.Sprintf("kustomize output for %s is not serializable", dir),
			"check kustomization.yaml for exotic transformer output")
	}
	y = []byte(substitute(string(y), gw))
	return apply.ParseMultiDoc(y)
}

// RenderDir is RenderDirFor with a zero GatewaySpec — cnoe's loader and any
// gateway-less caller keep exactly today's behavior. Exported because the
// cnoe-compat loader (Task 13) renders arbitrary directories through the
// same pipeline.
func RenderDir(dir string) ([]*unstructured.Unstructured, error) {
	return RenderDirFor(dir, config.GatewaySpec{})
}
