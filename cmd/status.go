package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/cli-utils/pkg/object"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/doctor"
	"github.com/cube-idp/cube-idp/internal/engine"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// th is the adaptive palette for cmd's styled static renders (status,
// doctor verdicts) — detected once, dark on any doubt; styled paths only
// engage on a real TTY.
var th = theme.Detect(os.Stdin, os.Stdout)

const statusClusterTimeout = 3 * time.Minute

func newStatusCmd() *cobra.Command {
	var file string
	var details bool
	var output string
	var compact bool
	var watch bool
	var interval time.Duration
	var exitUnhealthy bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report cluster connectivity, engine-reported component health, and inventory size",
		RunE: func(c *cobra.Command, _ []string) error {
			jsonDoc, err := wantJSONDoc(output)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			p := ui.NewFor(out)
			cubeName, collect, err := statusConnect(c.Context(), file, p.Styled() && !jsonDoc)
			if err != nil {
				return err
			}
			if watch {
				return runStatusWatch(c.Context(), out, p, jsonDoc, cubeName, collect, details, compact, interval, exitUnhealthy)
			}
			snap, err := collect(c.Context())
			if err != nil {
				return err
			}
			allReady, err := renderStatusOnce(out, p, jsonDoc, cubeName, snap, details, compact)
			if err != nil {
				return err
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
	c.Flags().BoolVar(&watch, "watch", false, "re-render the status view until every component is Ready")
	c.Flags().DurationVar(&interval, "interval", 3*time.Second, "re-render interval for --watch")
	c.Flags().BoolVar(&exitUnhealthy, "exit-status", false, "with --watch: exit 1 if interrupted while any component is unhealthy")
	c.Flags().BoolVar(&compact, "compact", false, "hide Ready component rows (plain and styled renders)")
	addOutputFlag(c, &output)
	return c
}

// runStatusWatch is the `gh run watch` clone: the one-shot view
// re-rendered every interval until every component is Ready, then exit 0.
// On a rich TTY it runs as an inline Bubble Tea tick program (managed
// region only — AltScreen never set); everywhere else — pipes, CI, JSON —
// it is a plain appending loop with no ANSI clearing (flux-style repeated
// blocks). exitUnhealthy (--exit-status) turns an interrupt-while-unhealthy
// into a bare exit 1 via the T08 sentinel — the CI gate
// `cube-idp status --watch --exit-status && run-e2e`.
func runStatusWatch(ctx context.Context, out io.Writer, p *ui.Printer, jsonDoc bool, cube string, collect statusCollector, details, compact bool, interval time.Duration, exitUnhealthy bool) error {
	if p.Styled() && !jsonDoc && ui.IsTerminal(os.Stdin) && ui.IsTerminal(out) {
		return runStatusWatchLive(ctx, out, p, cube, collect, details, compact, interval, exitUnhealthy)
	}
	interrupted := func() error {
		if exitUnhealthy {
			return errExitCode(1)
		}
		return nil
	}
	first := true
	for {
		snap, err := collect(ctx)
		if err != nil {
			if ctx.Err() != nil { // interrupt mid-collect, not a collection failure
				return interrupted()
			}
			return err
		}
		if !first {
			fmt.Fprintln(out)
		}
		first = false
		allReady, err := renderStatusOnce(out, p, jsonDoc, cube, snap, details, compact)
		if err != nil {
			return err
		}
		if allReady {
			return nil
		}
		select {
		case <-ctx.Done():
			return interrupted()
		case <-time.After(interval):
		}
	}
}

// watchSnapMsg carries one collected-and-rendered observation into the
// watch model; watchTickMsg triggers the next collection.
type watchSnapMsg struct {
	view     string
	allReady bool
	err      error
}

type watchTickMsg struct{}

// watchModel is the inline tick program behind `status --watch` on a rich
// TTY: the managed region holds exactly the last rendered one-shot view
// (no new TUI — the same view on a timer), a tick re-collects and
// re-renders, all-Ready pushes the final view to scrollback and quits,
// ctrl-c quits with the interrupted flag for --exit-status.
type watchModel struct {
	collect     func() watchSnapMsg
	interval    time.Duration
	view        string
	allReady    bool
	interrupted bool
	err         error
}

func (m watchModel) Init() tea.Cmd {
	return func() tea.Msg { return m.collect() } // first observation immediately
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchSnapMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		if msg.allReady {
			// Persist the final Ready view in scrollback before the managed
			// region vanishes with the program (tea.Sequence for ordered
			// finals).
			m.allReady, m.view = true, ""
			return m, tea.Sequence(tea.Println(msg.view), tea.Quit)
		}
		m.view = msg.view
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return watchTickMsg{} })
	case watchTickMsg:
		return m, func() tea.Msg { return m.collect() }
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.interrupted = true
			return m, tea.Quit
		}
		return m, nil
	case tea.InterruptMsg:
		m.interrupted = true
		return m, tea.Quit
	}
	return m, nil
}

func (m watchModel) View() tea.View {
	return tea.NewView(m.view)
}

// runStatusWatchLive drives the watchModel to completion and maps its final
// state onto the command's exit contract: collection error → that error;
// all Ready → exit 0; interrupted (ctrl-c, SIGINT, context cancel) →
// exit 1 under --exit-status, else 0.
func runStatusWatchLive(ctx context.Context, out io.Writer, p *ui.Printer, cube string, collect statusCollector, details, compact bool, interval time.Duration, exitUnhealthy bool) error {
	m := watchModel{
		interval: interval,
		collect: func() watchSnapMsg {
			snap, err := collect(ctx)
			if err != nil {
				return watchSnapMsg{err: err}
			}
			var buf bytes.Buffer
			allReady, err := renderStatusOnce(&buf, p, false, cube, snap, details, compact)
			return watchSnapMsg{view: strings.TrimSuffix(buf.String(), "\n"), allReady: allReady, err: err}
		},
	}
	final, runErr := tea.NewProgram(m, tea.WithOutput(out), tea.WithInput(os.Stdin), tea.WithContext(ctx)).Run()
	fm, _ := final.(watchModel)
	switch {
	case fm.err != nil:
		return fm.err
	case fm.allReady:
		return nil
	case fm.interrupted || ctx.Err() != nil:
		if exitUnhealthy {
			return errExitCode(1)
		}
		return nil
	default:
		return runErr
	}
}

// statusSnapshot is one observation of the cube — everything the render
// paths need, collected in a single pass so every projection (and every
// --watch tick) works from the same consistent view.
type statusSnapshot struct {
	Health    []engine.ComponentHealth
	Inventory []object.ObjMetadata
	Access    []ui.PackAccess
	Spokes    []spokeStatus
}

// spokeStatus is one declared spoke's row in the status view (S4):
// Registered — the S3 hub registration secret exists in the engine's
// namespace; Reachable — the spoke API server answered /readyz using that
// secret's own payload (probed from this machine — kind spokes carry a
// docker-network-internal URL (https://<cluster>-control-plane:6443, from
// kind's internal kubeconfig), so the hub engine may reach them when
// this process cannot).
type spokeStatus struct {
	Name       string
	Provider   string
	Registered bool
	Reachable  bool
}

// spokeStatusRows projects doctor's spoke probe states into status rows.
func spokeStatusRows(states []doctor.SpokeState) []spokeStatus {
	rows := make([]spokeStatus, 0, len(states))
	for _, s := range states {
		rows = append(rows, spokeStatus{Name: s.Name, Provider: s.Provider, Registered: s.Registered, Reachable: s.Reachable})
	}
	return rows
}

// statusCollector produces one statusSnapshot per call; --watch invokes it
// once per tick over the connection statusConnect established once.
type statusCollector func(ctx context.Context) (statusSnapshot, error)

// statusConnect performs the one-time cluster setup (config load, provider
// resolve, read-only existence check, connection, engine) and returns the
// cube name plus a per-observation collector. Package-level seam
// (trust.go's trustInstall pattern) so watch tests never reach a cluster.
var statusConnect = connectStatus

func connectStatus(ctx context.Context, file string, withAccess bool) (string, statusCollector, error) {
	cube, err := config.Load(file)
	if err != nil {
		return "", nil, err
	}
	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return "", nil, err
	}
	// status is read-only: Ensure would CREATE a missing kind cluster.
	if err := requireClusterExists(ctx, prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
		return "", nil, err
	}
	ensureCtx, cancel := context.WithTimeout(ctx, statusClusterTimeout)
	conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
	cancel()
	if err != nil {
		return "", nil, err
	}
	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return "", nil, err
	}
	eng, err := enginefactory.New(cube.Spec.Engine)
	if err != nil {
		return "", nil, err
	}
	collect := func(ctx context.Context) (statusSnapshot, error) {
		health, err := eng.Health(ctx, a)
		if err != nil {
			return statusSnapshot{}, err
		}
		inventory, err := a.LoadInventory(ctx)
		if err != nil {
			return statusSnapshot{}, err
		}
		snap := statusSnapshot{Health: health, Inventory: inventory}
		if withAccess {
			// Access URLs come from the Pack records `up` writes —
			// zero new plumbing (get secrets already lists the same CRD)
			// and best-effort (nil on any error): styled-mode garnish
			// must never fail an otherwise-healthy status. Fetched only
			// for the styled render, so plain/JSON runs make no extra
			// API call and stay byte-frozen.
			snap.Access = packAccessRows(ctx, a.Client())
		}
		if len(cube.Spec.Spokes) > 0 {
			// S4: spoke rows — Registered from the S3 hub secret,
			// Reachable from a /readyz probe of the secret's own payload
			// (2s each, all spokes in parallel). Probe failures are
			// states, never errors: status must render a dead spoke, not
			// fail on it. Spoke-less cubes take no extra API call and
			// keep their frozen bytes.
			snap.Spokes = spokeStatusRows(doctor.ProbeSpokes(ctx, a.Client(), cube.Spec.Engine.Type, cube.Spec.Spokes))
		}
		return snap, nil
	}
	return cube.Metadata.Name, collect, nil
}

// renderStatusOnce projects one snapshot through whichever render path the
// mode picks (JSON doc, styled snapshot, byte-frozen plain) and reports the
// overall Ready verdict. compact hides Ready component rows in the human
// renders; the JSON document always carries the full component set — a
// machine consumer filters for itself, silently dropped rows would lie.
func renderStatusOnce(out io.Writer, p *ui.Printer, jsonDoc bool, cube string, snap statusSnapshot, details, compact bool) (bool, error) {
	allReady := len(snap.Health) > 0
	for _, h := range snap.Health {
		if !h.Ready {
			allReady = false
		}
	}
	health := snap.Health
	if compact {
		kept := make([]engine.ComponentHealth, 0, len(health))
		for _, h := range health {
			if !h.Ready {
				kept = append(kept, h)
			}
		}
		health = kept
	}
	switch {
	case jsonDoc:
		return allReady, writeStatusJSON(out, cube, snap.Health, snap.Spokes, snap.Inventory, details, allReady)
	case p.Styled():
		renderStatusStyled(out, p, health, snap.Spokes, snap.Inventory, details, snap.Access)
	default:
		renderStatusPlain(out, p, health, snap.Spokes, snap.Inventory, details)
	}
	return allReady, nil
}

// spokeStateCell renders one paired glyph+word state cell (semantic-color
// doctrine: the word always accompanies the glyph — color and symbol
// alone may never carry the meaning).
func spokeStateCell(p *ui.Printer, ok bool, okWord, badWord string) string {
	if ok {
		return p.Glyph(ui.GlyphOK) + " " + okWord
	}
	return p.Glyph(ui.GlyphErr) + " " + badWord
}

// renderStatusPlain reproduces the original plain bytes exactly (status'
// "%s %s Ready\n" plain path is byte-frozen). Glyph passes the
// bare character through in plain mode, so this is identical to the original
// inline fmt.Fprintf calls.
func renderStatusPlain(out io.Writer, p *ui.Printer, health []engine.ComponentHealth, spokes []spokeStatus, inventory []object.ObjMetadata, details bool) {
	for _, h := range health {
		if h.Ready {
			fmt.Fprintf(out, "%s %s Ready\n", p.Glyph(ui.GlyphOK), h.Name)
			continue
		}
		fmt.Fprintf(out, "%s %s %s\n", p.Glyph(ui.GlyphErr), h.Name, h.Message)
	}
	// S4: spokes section — new surface, emitted only when spokes are
	// declared, so spoke-less cubes keep the byte-frozen pre-S4 output.
	if len(spokes) > 0 {
		fmt.Fprintf(out, "\nspokes\n")
		for _, s := range spokes {
			fmt.Fprintf(out, "%-20s %-10s %s  %s\n", s.Name, s.Provider,
				spokeStateCell(p, s.Registered, "registered", "unregistered"),
				spokeStateCell(p, s.Reachable, "reachable", "unreachable"))
		}
	}
	fmt.Fprintf(out, "\n%d object(s) in inventory\n", len(inventory))
	if details {
		fmt.Fprintf(out, "\n%s", formatInventory(inventory))
	}
}

// renderStatusStyled is the rich static snapshot rendered on a real terminal: a
// glyph-led component table with dimmed status messages, the inventory count,
// the access URLs from the Pack records (when any), and, under --details, the
// inventory table. out is threaded explicitly (not p.Out()) so the --watch
// tick program can render the same view into its region buffer.
func renderStatusStyled(out io.Writer, p *ui.Printer, health []engine.ComponentHealth, spokes []spokeStatus, inventory []object.ObjMetadata, details bool, access []ui.PackAccess) {
	fmt.Fprintln(out, th.Section.Render("Components"))
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
		fmt.Fprintf(out, "  %s %-*s  %s\n", glyph, name, h.Name, th.Msg.Render(msg))
	}
	// S4: spokes section right after the components, mirroring their
	// glyph-led rows; omitted entirely when no spokes are declared.
	if len(spokes) > 0 {
		fmt.Fprintf(out, "\n%s\n", th.Section.Render("Spokes"))
		for _, s := range spokes {
			fmt.Fprintf(out, "  %-20s %-10s %s  %s\n", s.Name, s.Provider,
				spokeStateCell(p, s.Registered, "registered", "unregistered"),
				spokeStateCell(p, s.Reachable, "reachable", "unreachable"))
		}
	}
	if len(access) > 0 {
		fmt.Fprintf(out, "\n%s\n", th.Section.Render("Access"))
		for _, pk := range access {
			for _, u := range pk.URLs {
				fmt.Fprintf(out, "  %-12s %s\n", pk.Name, u)
			}
		}
	}
	fmt.Fprintf(out, "\n%s\n", th.Section.Render(fmt.Sprintf("%d object(s) in inventory", len(inventory))))
	if details {
		fmt.Fprintf(out, "\n%s", formatInventory(inventory))
	}
}

// packAccessRows reads the access URLs the Pack records already carry
// (spec.urls — written by pack.PackObject with ${GATEWAY_HOST} substituted at
// `up` time), for the styled status render. Best-effort by design: any list
// error (CRD absent on an older cube, RBAC, transient apiserver) returns nil
// and status simply omits the Access section — it never fails the command.
func packAccessRows(ctx context.Context, c client.Client) []ui.PackAccess {
	var list unstructured.UnstructuredList
	list.SetGroupVersionKind(packListGVK)
	if err := c.List(ctx, &list); err != nil {
		return nil
	}
	rows := make([]ui.PackAccess, 0, len(list.Items))
	for _, item := range list.Items {
		urls, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "urls")
		if len(urls) == 0 {
			continue
		}
		rows = append(rows, ui.PackAccess{Name: item.GetName(), URLs: urls})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// statusDoc is the gh-style status document emitted by `-o json` — one final
// object, distinct from the --progress=json event stream. The objects
// array is present only under --details; ready is the overall verdict that
// also drives the exit code.
type statusDoc struct {
	jsonDocHead
	Cube       string            `json:"cube"`
	Components []statusComponent `json:"components"`
	// Spokes is additive (the JSON document is additive-only): present only when
	// spokes are declared,
	// so consumers written before spokes existed, and spoke-less cubes, see an
	// unchanged document.
	Spokes    []statusSpoke   `json:"spokes,omitempty"`
	Inventory statusInventory `json:"inventory"`
	Ready     bool            `json:"ready"`
}

type statusComponent struct {
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

type statusSpoke struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Registered bool   `json:"registered"`
	Reachable  bool   `json:"reachable"`
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

func writeStatusJSON(out io.Writer, cube string, health []engine.ComponentHealth, spokes []spokeStatus, inventory []object.ObjMetadata, details, ready bool) error {
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
	for _, s := range spokes {
		doc.Spokes = append(doc.Spokes, statusSpoke{Name: s.Name, Provider: s.Provider, Registered: s.Registered, Reachable: s.Reachable})
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
