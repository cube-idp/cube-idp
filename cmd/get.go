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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
)

const (
	cliSecretLabel = "cube-idp.dev/cli-secret"
	packNameLabel  = "cube-idp.dev/pack-name"
)

// secretRow is one CLI-surfaced secret, ready for tabular display.
type secretRow struct {
	Pack, Namespace, Name string
	Fields                map[string]string
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

func newGetCmd() *cobra.Command {
	var file string
	get := &cobra.Command{Use: "get", Short: "Read cube-idp-managed resources"}
	get.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")

	var pack string
	secrets := &cobra.Command{
		Use:   "secrets",
		Short: "List credentials packs exposed for CLI consumption (cube-idp.dev/cli-secret=true)",
		RunE: func(c *cobra.Command, _ []string) error {
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

			var list corev1.SecretList
			if err := a.Client().List(c.Context(), &list, client.MatchingLabels{cliSecretLabel: "true"}); err != nil {
				return err
			}
			rows := filterCLISecrets(list.Items, pack)
			printSecretRows(c.OutOrStdout(), rows)
			return nil
		},
	}
	secrets.Flags().StringVarP(&pack, "pack", "p", "", "only show secrets for this pack")
	get.AddCommand(secrets)
	return get
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Pack, r.Namespace, r.Name, strings.Join(pairs, ","))
	}
	tw.Flush()
}
