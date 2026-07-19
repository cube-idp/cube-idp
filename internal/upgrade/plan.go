// Package upgrade implements `cube-idp upgrade --plan`: a non-mutating
// preview of what re-running `up` would change — pack pins vs cube.lock,
// plus the kernel object diff.
package upgrade

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/cube-idp/cube-idp/internal/cfgload"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/diff"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
)

type Row struct {
	Name, Current, Latest, Change string
}

// Plan loads cfgPath, resolves each configured pack's CURRENT upstream pin
// (without fetching content, except the getter-ref probe ResolveRemote
// itself documents), and compares it against cube.lock's Resolved field —
// then runs the Task 6 kernel diff. Nothing here mutates cluster or cache
// state beyond ResolveRemote's own getter-ref probe fetch.
func Plan(ctx context.Context, cfgPath string, out io.Writer) (bool, error) {
	cube, err := cfgload.Load(ctx, cfgPath)
	if err != nil {
		return false, err
	}
	lf, err := lock.Read(lock.PathForOrigin(cfgPath, cube.Origin().Remote))
	if err != nil {
		return false, err
	}
	if lf == nil {
		return false, diag.New(diag.CodeLockCorrupt, "no cube.lock found next to "+cfgPath,
			"run `cube-idp up` once to create it; upgrade --plan compares against it")
	}

	cacheDir, err := pack.DefaultCacheDir()
	if err != nil {
		return false, err
	}

	// Same ref list `up` uses: gateway pack first, then spec.packs.
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	changed := false
	var rows []Row
	for _, pr := range refs {
		latest, err := pack.ResolveRemote(ctx, pr.Ref, cacheDir)
		if err != nil {
			return false, err
		}
		current := ""
		if locked := lockEntryByRef(lf, pr.Ref); locked != nil {
			current = locked.Resolved
		}
		row := classify(current, latest)
		row.Name = pr.Ref
		if row.Change != "up to date" {
			changed = true
		}
		rows = append(rows, row)
	}
	extra, err := refRows(ctx, cube, lf, cacheDir)
	if err != nil {
		return false, err
	}
	for _, r := range extra {
		if r.Change != "up to date" {
			changed = true
		}
	}
	rows = append(rows, extra...)
	fmt.Fprint(out, renderTable(rows))

	ui.NewFor(out).Section("\nKernel + delivery object changes:")
	kernelChanged, err := diff.Run(ctx, cfgPath, out)
	if err != nil {
		return false, err
	}
	return changed || kernelChanged, nil
}

// refRows builds the attribution rows for the non-chart remote sources
// (spec 2026-07-19 §6): one row per pack `valuesRef` and one for the hub
// cluster's `providerConfigRef`. Each gets its OWN line item so "values
// source changed" can never masquerade as a chart change. ResolveRemote
// computes the would-be pin without pulling (the getter-ref probe excepted,
// its documented exception). No `tuning(engine)` row exists: engine.tuning
// was removed by engine-as-pack, so there is no tuningRef to attribute.
// Hub cluster only — spoke providerConfigRef pins are out of scope.
func refRows(ctx context.Context, cube *config.Cube, lf *lock.File, cacheDir string) ([]Row, error) {
	var rows []Row
	// The gateway pack takes no valuesRef (it takes no values either), so
	// iterating spec.packs covers every values source `up` can record.
	for _, pr := range cube.Spec.Packs {
		if pr.ValuesRef == "" {
			continue
		}
		latest, err := pack.ResolveRemote(ctx, pr.ValuesRef, cacheDir)
		if err != nil {
			return nil, err
		}
		current := ""
		if locked := lockEntryByRef(lf, pr.Ref); locked != nil {
			current = locked.ValuesPin
		}
		row := classify(current, latest)
		row.Name = fmt.Sprintf("values(%s)", pr.ValuesRef)
		rows = append(rows, row)
	}
	if pcr := cube.Spec.Cluster.ProviderConfigRef; pcr != "" {
		latest, err := pack.ResolveRemote(ctx, pcr, cacheDir)
		if err != nil {
			return nil, err
		}
		current := ""
		if lf.Cluster != nil {
			current = lf.Cluster.ProviderConfigPin
		}
		row := classify(current, latest)
		row.Name = fmt.Sprintf("providerConfig(%s)", pcr)
		rows = append(rows, row)
	}
	return rows, nil
}

// classify compares a locked pin against the would-be pin. It is pin-string
// based (not lock.Entry based) so pack rows, values rows and providerConfig
// rows share one verdict rule; an absent locked pin reads as "new".
func classify(current, latest string) Row {
	switch {
	case current == "":
		return Row{Latest: latest, Change: "new (not in cube.lock)"}
	case current == latest:
		return Row{Current: current, Latest: latest, Change: "up to date"}
	default:
		return Row{Current: current, Latest: latest, Change: "update available"}
	}
}

func lockEntryByRef(f *lock.File, ref string) *lock.Entry {
	for i := range f.Packs {
		if f.Packs[i].Ref == ref {
			return &f.Packs[i]
		}
	}
	return nil
}

func renderTable(rows []Row) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PACK\tCURRENT\tLATEST\tCHANGE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, shorten(r.Current), shorten(r.Latest), r.Change)
	}
	w.Flush()
	return b.String()
}

func shorten(pin string) string {
	if len(pin) > 24 {
		return pin[:24] + "…"
	}
	return pin
}
