package pack

import (
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// flatStringValues narrows validated #Values to the flat string map kustomize
// substitution can consume.
//
// Kustomize has no values concept; its customization axis is overlays and
// patches, which belong in the payload. The one semantic that does not fight
// the tool is post-build variable substitution, and that is textual — so a
// nested map, a list, a null, or a non-string scalar has no meaningful
// projection onto it. Rejecting them beats guessing at a stringification the
// author never asked for.
func flatStringValues(values map[string]any) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, name := range sortedKeys(values) {
		str, ok := values[name].(string)
		if !ok {
			return nil, newValuesNotFlatError(name, values[name])
		}
		out[name] = str
	}
	return out, nil
}

// substitute expands ${VAR} references in the scalar values of every object,
// in place. Keys, comments, and the raw document bytes are never touched, so
// the result cannot become invalid YAML.
//
// Every unresolved variable across every object is collected before reporting,
// so one run names all of them rather than one per re-run.
func substitute(objs []*unstructured.Unstructured, vars map[string]string) error {
	missing := map[string]struct{}{}
	for _, obj := range objs {
		// expandValue rewrites a map in place, so there is nothing to assign
		// back — and nothing that could quietly blank an object out.
		expandValue(obj.Object, vars, missing)
	}
	if len(missing) == 0 {
		return nil
	}
	return newSubstitutionError(sortedKeys(missing))
}

// expandValue walks a decoded YAML value, expanding string leaves only. Maps
// and slices are rewritten in place; only a string leaf produces a new value,
// which is why the caller assigns the result back for those.
func expandValue(value any, vars map[string]string, missing map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			typed[key] = expandValue(nested, vars, missing)
		}
		return typed
	case []any:
		for i, nested := range typed {
			typed[i] = expandValue(nested, vars, missing)
		}
		return typed
	case string:
		return expand(typed, vars, missing)
	default:
		return value
	}
}

// expand rewrites one scalar.
//
// The grammar is deliberately small: ${NAME} is the only reference form,
// $${NAME} escapes to a literal ${NAME}, and a $ that does not open a
// reference is literal. There are no shell-style defaults — #Values defaults
// are the one defaulting mechanism, and a second one in a different syntax is
// a cliff. A ${...} whose contents are not a valid name is not a reference and
// is left alone.
//
// The result is always a string: expanding a whole scalar does not retype it.
func expand(s string, vars map[string]string, missing map[string]struct{}) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "$${") {
			b.WriteString("${")
			i += len("$${")
			continue
		}
		name, width, ok := reference(s[i:])
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		value, found := vars[name]
		if !found {
			missing[name] = struct{}{}
		}
		b.WriteString(value)
		i += width
	}
	return b.String()
}

// reference matches a leading ${NAME} and returns the name with the number of
// bytes consumed. NAME is [A-Za-z_][A-Za-z0-9_]*.
func reference(s string) (name string, width int, ok bool) {
	if !strings.HasPrefix(s, "${") {
		return "", 0, false
	}
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return "", 0, false
	}
	name = s[len("${"):end]
	if !isVarName(name) {
		return "", 0, false
	}
	return name, end + 1, true
}

func isVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// sortedKeys returns a map's keys in a stable order, so that error messages
// and iteration do not depend on Go's map ordering.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
