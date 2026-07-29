package cluster

import (
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// ContextName derives the cube-owned kubeconfig context name. The prefix
// is the API group — one source of truth, never a second literal.
func ContextName(clusterName string) string {
	return v1alpha1.GroupVersion.Group + "/" + clusterName
}

// kubeconfig is a minimal kubeconfig model: just enough structure to
// rename and merge entries. Entry bodies stay raw so every field a
// provider emitted round-trips unchanged. Context bodies are fully typed —
// cluster/user/namespace/extensions is the complete upstream field set.
type kubeconfig struct {
	APIVersion     string          `json:"apiVersion,omitempty"`
	Kind           string          `json:"kind,omitempty"`
	Preferences    json.RawMessage `json:"preferences,omitempty"`
	Clusters       []namedEntry    `json:"clusters,omitempty"`
	Contexts       []contextEntry  `json:"contexts,omitempty"`
	Users          []namedEntry    `json:"users,omitempty"`
	CurrentContext string          `json:"current-context,omitempty"`
	Extensions     json.RawMessage `json:"extensions,omitempty"`
}

type namedEntry struct {
	Name    string          `json:"name"`
	Cluster json.RawMessage `json:"cluster,omitempty"`
	User    json.RawMessage `json:"user,omitempty"`
}

type contextEntry struct {
	Name    string      `json:"name"`
	Context contextBody `json:"context"`
}

type contextBody struct {
	Cluster    string          `json:"cluster"`
	User       string          `json:"user"`
	Namespace  string          `json:"namespace,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
}

// Rebrand renames a single-cluster, provider-native kubeconfig to
// contextName and stamps the context namespace when non-empty (design §4).
func Rebrand(raw []byte, contextName, namespace string) ([]byte, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	if len(kc.Clusters) != 1 || len(kc.Contexts) != 1 || len(kc.Users) != 1 {
		return nil, fmt.Errorf("expected single-cluster kubeconfig, got %d clusters / %d contexts / %d users",
			len(kc.Clusters), len(kc.Contexts), len(kc.Users))
	}
	kc.Clusters[0].Name = contextName
	kc.Users[0].Name = contextName
	kc.Contexts[0].Name = contextName
	kc.Contexts[0].Context.Cluster = contextName
	kc.Contexts[0].Context.User = contextName
	if namespace != "" {
		kc.Contexts[0].Context.Namespace = namespace
	}
	kc.CurrentContext = contextName
	out, err := yaml.Marshal(kc)
	if err != nil {
		return nil, fmt.Errorf("render kubeconfig: %w", err)
	}
	return out, nil
}

// Merge upserts incoming's entries into existing by name and adopts
// incoming's current-context. An empty existing yields incoming as-is.
func Merge(existing, incoming []byte) ([]byte, error) {
	if len(existing) == 0 {
		return incoming, nil
	}
	var dst, src kubeconfig
	if err := yaml.Unmarshal(existing, &dst); err != nil {
		return nil, fmt.Errorf("parse existing kubeconfig: %w", err)
	}
	if err := yaml.Unmarshal(incoming, &src); err != nil {
		return nil, fmt.Errorf("parse incoming kubeconfig: %w", err)
	}
	dst.Clusters = upsertNamed(dst.Clusters, src.Clusters)
	dst.Users = upsertNamed(dst.Users, src.Users)
	dst.Contexts = upsertContexts(dst.Contexts, src.Contexts)
	if src.CurrentContext != "" {
		dst.CurrentContext = src.CurrentContext
	}
	if dst.APIVersion == "" {
		dst.APIVersion, dst.Kind = src.APIVersion, src.Kind
	}
	out, err := yaml.Marshal(dst)
	if err != nil {
		return nil, fmt.Errorf("render merged kubeconfig: %w", err)
	}
	return out, nil
}

func upsertNamed(dst, src []namedEntry) []namedEntry {
	for _, s := range src {
		replaced := false
		for i := range dst {
			if dst[i].Name == s.Name {
				dst[i], replaced = s, true
				break
			}
		}
		if !replaced {
			dst = append(dst, s)
		}
	}
	return dst
}

func upsertContexts(dst, src []contextEntry) []contextEntry {
	for _, s := range src {
		replaced := false
		for i := range dst {
			if dst[i].Name == s.Name {
				dst[i], replaced = s, true
				break
			}
		}
		if !replaced {
			dst = append(dst, s)
		}
	}
	return dst
}
