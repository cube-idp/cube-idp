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
// contextName and stamps the context namespace when non-empty, so kubectl
// and clientcmd-based clients pick up the cube-owned identity.
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
// Only the keys cube-idp understands (clusters, contexts, users,
// current-context, plus apiVersion/kind when absent) are touched; every
// other top-level key in the user's file passes through untouched —
// "never destroys what we don't understand" is structural, not assumed.
func Merge(existing, incoming []byte) ([]byte, error) {
	if len(existing) == 0 {
		return incoming, nil
	}
	var dst, src map[string]any
	if err := yaml.Unmarshal(existing, &dst); err != nil {
		return nil, fmt.Errorf("parse existing kubeconfig: %w", err)
	}
	if err := yaml.Unmarshal(incoming, &src); err != nil {
		return nil, fmt.Errorf("parse incoming kubeconfig: %w", err)
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for _, key := range []string{"clusters", "contexts", "users"} {
		dstList, _ := dst[key].([]any)
		srcList, _ := src[key].([]any)
		if merged := upsert(dstList, srcList, entryName); len(merged) > 0 {
			dst[key] = merged
		}
	}
	if cc, _ := src["current-context"].(string); cc != "" {
		dst["current-context"] = cc
	}
	if v, _ := dst["apiVersion"].(string); v == "" {
		dst["apiVersion"], dst["kind"] = src["apiVersion"], src["kind"]
	}
	out, err := yaml.Marshal(dst)
	if err != nil {
		return nil, fmt.Errorf("render merged kubeconfig: %w", err)
	}
	return out, nil
}

// Remove deletes every entry named contextName from a kubeconfig's
// clusters/contexts/users lists, plus current-context when it pointed at
// the removed context — the exact reverse of installing a Rebrand-ed
// config with Merge. Same map-based model as Merge: every key cube-idp
// does not understand passes through untouched. The previous
// current-context is not restorable (Merge overwrote it), so it is
// unset, matching kubectl's delete-context behavior. The bool reports
// whether anything changed; when false the returned bytes are the input,
// so callers can skip rewriting an untouched file.
func Remove(existing []byte, contextName string) ([]byte, bool, error) {
	if len(existing) == 0 {
		return existing, false, nil
	}
	var kc map[string]any
	if err := yaml.Unmarshal(existing, &kc); err != nil {
		return nil, false, fmt.Errorf("parse kubeconfig: %w", err)
	}
	changed := false
	for _, key := range []string{"clusters", "contexts", "users"} {
		list, _ := kc[key].([]any)
		kept := make([]any, 0, len(list))
		for _, e := range list {
			if entryName(e) != contextName {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(list) {
			continue // untouched lists keep their original value, even empty ones
		}
		changed = true
		if len(kept) == 0 {
			delete(kc, key)
		} else {
			kc[key] = kept
		}
	}
	if cc, _ := kc["current-context"].(string); cc == contextName {
		delete(kc, "current-context")
		changed = true
	}
	if !changed {
		return existing, false, nil
	}
	out, err := yaml.Marshal(kc)
	if err != nil {
		return nil, false, fmt.Errorf("render kubeconfig: %w", err)
	}
	return out, true, nil
}

// entryName extracts the name of a kubeconfig list entry. Incoming
// entries always carry names (Rebrand stamps them); a nameless entry in
// the existing file yields "" and is never displaced by a named one.
func entryName(e any) string {
	m, _ := e.(map[string]any)
	name, _ := m["name"].(string)
	return name
}

// upsert replaces dst elements whose name matches a src element and
// appends the rest, preserving dst order.
func upsert[E any](dst, src []E, name func(E) string) []E {
	for _, s := range src {
		replaced := false
		for i := range dst {
			if name(dst[i]) == name(s) {
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
