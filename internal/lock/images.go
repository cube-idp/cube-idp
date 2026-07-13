package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ImagesFrom extracts every container image referenced by the objects,
// walking any containers/initContainers/ephemeralContainers list at any
// depth (covers Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Pods).
func ImagesFrom(objs []*unstructured.Unstructured) []string {
	set := map[string]struct{}{}
	for _, o := range objs {
		walkImages(o.Object, set)
	}
	out := make([]string, 0, len(set))
	for img := range set {
		out = append(out, img)
	}
	sort.Strings(out)
	return out
}

func walkImages(v any, set map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "containers" || k == "initContainers" || k == "ephemeralContainers" {
				if list, ok := val.([]any); ok {
					for _, c := range list {
						if cm, ok := c.(map[string]any); ok {
							if img, ok := cm["image"].(string); ok && img != "" {
								set[img] = struct{}{}
							}
						}
					}
				}
			}
			walkImages(val, set)
		}
	case []any:
		for _, e := range t {
			walkImages(e, set)
		}
	}
}

// RenderedHash is a stable content hash of the rendered objects
// (sigs.k8s.io/yaml marshals via JSON with sorted keys).
func RenderedHash(objs []*unstructured.Unstructured) (string, error) {
	h := sha256.New()
	for _, o := range objs {
		b, err := yaml.Marshal(o.Object)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte("---\n"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
