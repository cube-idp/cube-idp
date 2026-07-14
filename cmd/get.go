package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/ui"
)

const (
	cliSecretLabel = "cube-idp.dev/cli-secret"
	packNameLabel  = "cube-idp.dev/pack-name"
)

// secretRow is one CLI-surfaced secret, ready for tabular display.
// Placeholder, when set, is rendered in the DATA column instead of Fields —
// used when a Pack's authSecretRef points at a Secret that doesn't exist
// (yet), so the pack still shows up instead of silently vanishing.
type secretRow struct {
	Pack, Namespace, Name string
	Fields                map[string]string
	Placeholder           string
}

// filterCLISecrets keeps only secrets labeled cube-idp.dev/cli-secret=true,
// optionally narrowed to a single pack, and flattens each secret's Data map
// into plain strings for display.
func filterCLISecrets(secrets []corev1.Secret, packFilter string) []secretRow {
	var rows []secretRow
	for _, s := range secrets {
		if s.Labels[cliSecretLabel] != "true" {
			continue
		}
		pack := s.Labels[packNameLabel]
		if packFilter != "" && pack != packFilter {
			continue
		}
		row := secretRow{Pack: pack, Namespace: s.Namespace, Name: s.Name, Fields: map[string]string{}}
		for k, v := range s.Data {
			row.Fields[k] = string(v)
		}
		rows = append(rows, row)
	}
	return rows
}

// packListGVK identifies the D11 Pack CRD's list kind (internal/pack's
// discoverability record). get secrets lists it unstructured so this
// read-only command never has to import internal/pack, which would drag in
// the fetch/render machinery it has no need of.
var packListGVK = schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "PackList"}

// packSecretRows is the D11 primary path: list Pack records and, for every
// pack whose spec.authSecretRef is set, follow it to the referenced Secret,
// merging spec.impliedFields underneath the secret's own keys (the secret's
// own keys win on conflict — impliedFields only fills in what the secret
// itself doesn't carry, e.g. ArgoCD's implicit "admin" username, which is
// never actually stored in argocd-initial-admin-secret). packFilter narrows
// to one pack by name; "" means all. covered reports every pack name
// resolved this way, so the legacy label fallback can skip it.
func packSecretRows(ctx context.Context, c client.Client, packFilter string) (rows []secretRow, covered map[string]bool, err error) {
	var list unstructured.UnstructuredList
	list.SetGroupVersionKind(packListGVK)
	if err := c.List(ctx, &list); err != nil {
		return nil, nil, err
	}
	covered = map[string]bool{}
	for _, item := range list.Items {
		name := item.GetName()
		if packFilter != "" && name != packFilter {
			continue
		}
		ns, nsOK, _ := unstructured.NestedString(item.Object, "spec", "authSecretRef", "namespace")
		secName, nameOK, _ := unstructured.NestedString(item.Object, "spec", "authSecretRef", "name")
		if !nsOK || !nameOK || ns == "" || secName == "" {
			continue // D11: nil authSecretRef means the pack exposes no credential
		}
		covered[name] = true
		var sec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: secName}, &sec); err != nil {
			if apierrors.IsNotFound(err) {
				// A dangling authSecretRef (e.g. argocd-initial-admin-secret
				// before Argo CD's first boot finishes) must not abort the
				// whole listing — surface the pack with an explicit marker
				// instead. impliedFields are deliberately NOT shown here:
				// alone they'd read as a usable credential.
				rows = append(rows, secretRow{Pack: name, Namespace: ns, Name: secName,
					Placeholder: fmt.Sprintf("<secret %s/%s not found>", ns, secName)})
				continue
			}
			return nil, nil, err
		}
		fields := map[string]string{}
		implied, _, _ := unstructured.NestedStringMap(item.Object, "spec", "impliedFields")
		for k, v := range implied {
			fields[k] = v
		}
		for k, v := range sec.Data {
			fields[k] = string(v)
		}
		rows = append(rows, secretRow{Pack: name, Namespace: sec.Namespace, Name: sec.Name, Fields: fields})
	}
	return rows, covered, nil
}

// legacyDeprecationNote is the D11 grace-period message: the phase-1 label
// convention (cube-idp.dev/cli-secret + cube-idp.dev/pack-name) is honored
// one more release for packs that haven't declared expose.authSecretRef yet.
func legacyDeprecationNote(pack string) string {
	return fmt.Sprintf("note: %s was found via the legacy %s + %s labels; pack authors should declare expose.authSecretRef in pack.cue (label support ends next release)",
		pack, cliSecretLabel, packNameLabel)
}

// secretsForDisplay is the D11 `get secrets` pivot: Pack -> authSecretRef ->
// Secret is primary; any pack not resolved that way falls back to the
// legacy cli-secret label convention, prefixed with a deprecation note per
// pack found only there.
func secretsForDisplay(ctx context.Context, c client.Client, packFilter string) (rows []secretRow, notes []string, err error) {
	rows, covered, err := packSecretRows(ctx, c, packFilter)
	if err != nil {
		return nil, nil, err
	}
	var list corev1.SecretList
	if err := c.List(ctx, &list, client.MatchingLabels{cliSecretLabel: "true"}); err != nil {
		return nil, nil, err
	}
	seenNote := map[string]bool{}
	for _, r := range filterCLISecrets(list.Items, packFilter) {
		if covered[r.Pack] {
			continue // already resolved via the pack's own expose.authSecretRef
		}
		if !seenNote[r.Pack] {
			seenNote[r.Pack] = true
			notes = append(notes, legacyDeprecationNote(r.Pack))
		}
		rows = append(rows, r)
	}
	return rows, notes, nil
}

func newGetCmd() *cobra.Command {
	var file string
	get := &cobra.Command{Use: "get", Short: "Read cube-idp-managed resources"}
	get.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")

	var pack string
	var output string
	secrets := &cobra.Command{
		Use:   "secrets",
		Short: "List credentials packs exposed for CLI consumption (cube-idp.dev/cli-secret=true)",
		RunE: func(c *cobra.Command, _ []string) error {
			jsonDoc, err := wantJSONDoc(output)
			if err != nil {
				return err
			}
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// get is read-only: Ensure would CREATE a missing kind cluster.
			if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
				return err
			}
			// Bound the connect like status/down — no infinite spinner on an
			// unreachable cluster.
			ensureCtx, cancel := context.WithTimeout(c.Context(), 3*time.Minute)
			conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
			cancel()
			if err != nil {
				return err
			}
			a, err := apply.New(conn.REST, cube.Metadata.Name)
			if err != nil {
				return err
			}

			rows, notes, err := secretsForDisplay(c.Context(), a.Client(), pack)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if jsonDoc {
				return writeSecretsJSON(out, rows, notes)
			}
			p := ui.NewFor(out)
			for _, n := range notes {
				// ModePlain: exactly fmt.Fprintln(out, n), unchanged from
				// before Task 15.3. ModeStyled: prefixed with the amber
				// warning glyph — the same unification doctor/status use.
				p.Warn("%s", n)
			}
			printSecretRows(out, rows)
			return nil
		},
	}
	secrets.Flags().StringVarP(&pack, "pack", "p", "", "only show secrets for this pack")
	addOutputFlag(secrets, &output)
	get.AddCommand(secrets)
	return get
}

// secretsDoc is the gh-style `get secrets` document (design doc §10): the
// secret rows with their flattened fields, plus any legacy-label deprecation
// notes surfaced as data rather than warning lines.
type secretsDoc struct {
	jsonDocHead
	Secrets []secretDocRow `json:"secrets"`
	Notes   []string       `json:"notes,omitempty"`
}

type secretDocRow struct {
	Pack        string            `json:"pack"`
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Fields      map[string]string `json:"fields,omitempty"`
	Placeholder string            `json:"placeholder,omitempty"`
}

func writeSecretsJSON(out io.Writer, rows []secretRow, notes []string) error {
	doc := secretsDoc{jsonDocHead: jsonDocHead{V: docSchemaVersion}, Secrets: make([]secretDocRow, 0, len(rows)), Notes: notes}
	for _, r := range rows {
		doc.Secrets = append(doc.Secrets, secretDocRow{
			Pack: r.Pack, Namespace: r.Namespace, Name: r.Name, Fields: r.Fields, Placeholder: r.Placeholder,
		})
	}
	return writeJSONDoc(out, doc)
}

// cellEscaper keeps secret values from corrupting the tabwriter table:
// newlines (e.g. PEM keys) and tabs are shown as literal \n and \t.
var cellEscaper = strings.NewReplacer("\n", `\n`, "\t", `\t`)

// printSecretRows renders rows as an aligned PACK/NAMESPACE/NAME/DATA table.
func printSecretRows(w io.Writer, rows []secretRow) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PACK\tNAMESPACE\tNAME\tDATA")
	for _, r := range rows {
		keys := make([]string, 0, len(r.Fields))
		for k := range r.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, cellEscaper.Replace(r.Fields[k])))
		}
		data := strings.Join(pairs, ",")
		if data == "" && r.Placeholder != "" {
			data = r.Placeholder
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Pack, r.Namespace, r.Name, data)
	}
	tw.Flush()
}
