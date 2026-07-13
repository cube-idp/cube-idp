package pack

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// RenderDir kustomize-builds dir (which must contain kustomization.yaml) and
// returns the resulting objects. Exported because the cnoe-compat loader
// (Task 13) renders arbitrary directories through the same pipeline.
func RenderDir(dir string) ([]*unstructured.Unstructured, error) {
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
	return apply.ParseMultiDoc(y)
}
