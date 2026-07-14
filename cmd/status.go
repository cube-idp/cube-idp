package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/fluxcd/cli-utils/pkg/object"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/ui"
)

const statusClusterTimeout = 3 * time.Minute

func newStatusCmd() *cobra.Command {
	var file string
	var details bool
	var output string
	c := &cobra.Command{
		Use:   "status",
		Short: "Report cluster connectivity, engine-reported component health, and inventory size",
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
			// status is read-only: Ensure would CREATE a missing kind cluster.
			if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(c.Context(), statusClusterTimeout)
			conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
			cancel()
			if err != nil {
				return err
			}
			a, err := apply.New(conn.REST, cube.Metadata.Name)
			if err != nil {
				return err
			}
			eng, err := enginefactory.New(cube.Spec.Engine.Type)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			p := ui.NewFor(out)
			health, err := eng.Health(c.Context(), a)
			if err != nil {
				return err
			}
			allReady := len(health) > 0
			for _, h := range health {
				if !h.Ready {
					allReady = false
				}
			}

			inventory, err := a.LoadInventory(c.Context())
			if err != nil {
				return err
			}

			switch {
			case jsonDoc:
				if err := writeStatusJSON(out, cube.Metadata.Name, health, inventory, details, allReady); err != nil {
					return err
				}
			case p.Styled():
				renderStatusStyled(p, health, inventory, details)
			default:
				renderStatusPlain(out, p, health, inventory, details)
			}

			if !allReady {
				return diag.New(diag.CodeEngineHealthTimeout, "one or more components are not ready",
					"inspect the components listed above with kubectl; re-run `cube-idp up` if needed")
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&details, "details", false, "show inventory objects")
	addOutputFlag(c, &output)
	return c
}

// renderStatusPlain reproduces the pre-14c plain bytes exactly (design doc §8
// item 4: status' "%s %s Ready\n" plain path is byte-frozen). Glyph passes the
// bare character through in plain mode, so this is identical to the phase-1
// inline fmt.Fprintf calls.
func renderStatusPlain(out io.Writer, p *ui.Printer, health []engine.ComponentHealth, inventory []object.ObjMetadata, details bool) {
	for _, h := range health {
		if h.Ready {
			fmt.Fprintf(out, "%s %s Ready\n", p.Glyph(ui.GlyphOK), h.Name)
			continue
		}
		fmt.Fprintf(out, "%s %s %s\n", p.Glyph(ui.GlyphErr), h.Name, h.Message)
	}
	fmt.Fprintf(out, "\n%d object(s) in inventory\n", len(inventory))
	if details {
		fmt.Fprintf(out, "\n%s", formatInventory(inventory))
	}
}

var (
	statusHeaderStyle = lipgloss.NewStyle().Bold(true)
	statusDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// renderStatusStyled is the stage-B rich static snapshot (design doc §10): a
// glyph-led component table with dimmed status messages, the inventory count,
// and, under --details, the inventory table. Transient static output — it
// exits immediately (no --watch, no resident view).
func renderStatusStyled(p *ui.Printer, health []engine.ComponentHealth, inventory []object.ObjMetadata, details bool) {
	out := p.Out()
	fmt.Fprintln(out, statusHeaderStyle.Render("Components"))
	name := 0
	for _, h := range health {
		if len(h.Name) > name {
			name = len(h.Name)
		}
	}
	for _, h := range health {
		glyph, msg := p.Glyph(ui.GlyphOK), "Ready"
		if !h.Ready {
			glyph, msg = p.Glyph(ui.GlyphErr), h.Message
		}
		fmt.Fprintf(out, "  %s %-*s  %s\n", glyph, name, h.Name, statusDimStyle.Render(msg))
	}
	fmt.Fprintf(out, "\n%s\n", statusHeaderStyle.Render(fmt.Sprintf("%d object(s) in inventory", len(inventory))))
	if details {
		fmt.Fprintf(out, "\n%s", formatInventory(inventory))
	}
}

// statusDoc is the gh-style status document (design doc §10). The objects
// array is present only under --details; ready is the overall verdict that
// also drives the exit code.
type statusDoc struct {
	jsonDocHead
	Cube       string           `json:"cube"`
	Components []statusComponent `json:"components"`
	Inventory  statusInventory   `json:"inventory"`
	Ready      bool              `json:"ready"`
}

type statusComponent struct {
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

type statusInventory struct {
	Count   int            `json:"count"`
	Objects []statusObject `json:"objects,omitempty"`
}

type statusObject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func writeStatusJSON(out io.Writer, cube string, health []engine.ComponentHealth, inventory []object.ObjMetadata, details, ready bool) error {
	doc := statusDoc{
		jsonDocHead: jsonDocHead{V: docSchemaVersion},
		Cube:        cube,
		Components:  make([]statusComponent, 0, len(health)),
		Inventory:   statusInventory{Count: len(inventory)},
		Ready:       ready,
	}
	for _, h := range health {
		doc.Components = append(doc.Components, statusComponent{Name: h.Name, Ready: h.Ready, Message: h.Message})
	}
	if details {
		doc.Inventory.Objects = inventoryObjects(inventory)
	}
	return writeJSONDoc(out, doc)
}

// inventoryObjects sorts the inventory (Kind, Namespace, Name — the same order
// formatInventory uses) and projects it into the document's object rows.
func inventoryObjects(inv []object.ObjMetadata) []statusObject {
	sorted := make([]object.ObjMetadata, len(inv))
	copy(sorted, inv)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GroupKind.Kind != sorted[j].GroupKind.Kind {
			return sorted[i].GroupKind.Kind < sorted[j].GroupKind.Kind
		}
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})
	objs := make([]statusObject, 0, len(sorted))
	for _, o := range sorted {
		objs = append(objs, statusObject{Kind: o.GroupKind.Kind, Namespace: o.Namespace, Name: o.Name})
	}
	return objs
}

// formatInventory takes a slice of ObjMetadata and returns a tabwriter table
// with header KIND\tNAMESPACE\tNAME, sorted by Kind, Namespace, then Name.
// Cluster-scoped objects (empty Namespace) show "-" for namespace.
func formatInventory(inv []object.ObjMetadata) string {
	// Sort by Kind, then Namespace, then Name
	sorted := make([]object.ObjMetadata, len(inv))
	copy(sorted, inv)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GroupKind.Kind != sorted[j].GroupKind.Kind {
			return sorted[i].GroupKind.Kind < sorted[j].GroupKind.Kind
		}
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})

	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)
	fmt.Fprint(w, "KIND\tNAMESPACE\tNAME\n")
	for _, obj := range sorted {
		namespace := obj.Namespace
		if namespace == "" {
			namespace = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", obj.GroupKind.Kind, namespace, obj.Name)
	}
	w.Flush()
	return buf.String()
}
