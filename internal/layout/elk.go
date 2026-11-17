package layout

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
)

// ELK lays a graph out with the Eclipse Layout Kernel, run through D2's Go
// library. A 2026-10-01 trial on real diagrams picked it over Graphviz and
// dagre: it nests containers cleanly and routes links without crossings.
type ELK struct{}

// Layout implements Layouter. It writes the graph as a D2 script, compiles
// it with the ELK layout, and reads positions back by item ID.
func (ELK) Layout(ctx context.Context, g Graph) (*Result, error) {
	script, itemIDs, linkIDs, err := d2Script(g)
	if err != nil {
		return nil, err
	}

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("create text ruler: %w", err)
	}
	// D2 logs a warning with a stack trace when the context has no logger.
	ctx = d2log.With(ctx, slog.New(slog.DiscardHandler))
	diagram, _, err := d2lib.Compile(ctx, script, &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2elklayout.DefaultLayout, nil },
	}, &d2svg.RenderOpts{})
	if err != nil {
		return nil, fmt.Errorf("lay out with ELK: %w", err)
	}

	res := &Result{Boxes: map[string]Box{}, Routes: map[string][]Point{}}
	for _, s := range diagram.Shapes {
		res.Boxes[itemIDs[lastKey(s.ID)]] = Box{
			X:      float64(s.Pos.X),
			Y:      float64(s.Pos.Y),
			Width:  float64(s.Width),
			Height: float64(s.Height),
		}
	}
	// D2 returns connections in script order, which is link order.
	if len(diagram.Connections) != len(linkIDs) {
		return nil, fmt.Errorf("ELK returned %d routes for %d links", len(diagram.Connections), len(linkIDs))
	}
	for i, c := range diagram.Connections {
		route := make([]Point, 0, len(c.Route))
		for _, p := range c.Route {
			route = append(route, Point{X: p.X, Y: p.Y})
		}
		res.Routes[linkIDs[i]] = route
	}
	return res, nil
}

// d2Script writes the graph as D2: nested blocks for containers, fixed sizes
// for leaves, and links between full key paths. Each item is keyed by its
// position in the graph, since D2 splits keys on dots and an Azure resource
// ID holds them. It returns each key's item ID, and link IDs in script
// order.
func d2Script(g Graph) (string, map[string]string, []string, error) {
	items := map[string]Item{}
	children := map[string][]string{}
	keys := map[string]string{}
	itemIDs := map[string]string{}
	for i, it := range g.Items {
		if _, ok := items[it.ID]; ok {
			return "", nil, nil, fmt.Errorf("item %s: listed twice", it.ID)
		}
		if it.Parent != "" {
			if _, ok := items[it.Parent]; !ok {
				return "", nil, nil, fmt.Errorf("item %s: parent %s is not listed before it", it.ID, it.Parent)
			}
		}
		items[it.ID] = it
		children[it.Parent] = append(children[it.Parent], it.ID)
		keys[it.ID] = fmt.Sprintf("i%d", i)
		itemIDs[keys[it.ID]] = it.ID
	}

	var b strings.Builder
	var write func(id, indent string)
	write = func(id, indent string) {
		it := items[id]
		fmt.Fprintf(&b, "%s%s: {\n", indent, keys[id])
		fmt.Fprintf(&b, "%s  label: %s\n", indent, quote(it.Label))
		if !it.Container {
			fmt.Fprintf(&b, "%s  width: %d\n%s  height: %d\n", indent, int(it.Width), indent, int(it.Height))
		}
		if n := len(children[id]); it.Container && n > 1 {
			// D2 fills a grid in the direction of the first key it is given;
			// rows first fills left to right, then top to bottom.
			cols := cmp.Or(it.Columns, gridColumns(n))
			fmt.Fprintf(&b, "%s  grid-rows: %d\n%s  grid-columns: %d\n", indent, (n+cols-1)/cols, indent, cols)
		}
		for _, c := range children[id] {
			write(c, indent+"  ")
		}
		fmt.Fprintf(&b, "%s}\n", indent)
	}
	for _, id := range children[""] {
		write(id, "")
	}

	path := func(id string) string {
		var parts []string
		for id != "" {
			parts = append([]string{keys[id]}, parts...)
			id = items[id].Parent
		}
		return strings.Join(parts, ".")
	}
	linkIDs := make([]string, 0, len(g.Links))
	for _, l := range g.Links {
		if _, ok := items[l.From]; !ok {
			return "", nil, nil, fmt.Errorf("link %s: unknown source %s", l.ID, l.From)
		}
		if _, ok := items[l.To]; !ok {
			return "", nil, nil, fmt.Errorf("link %s: unknown target %s", l.ID, l.To)
		}
		fmt.Fprintf(&b, "%s -> %s", path(l.From), path(l.To))
		if l.Label != "" {
			fmt.Fprintf(&b, ": %s", quote(l.Label))
		}
		b.WriteString("\n")
		linkIDs = append(linkIDs, l.ID)
	}
	return b.String(), itemIDs, linkIDs, nil
}

// gridColumns lays n children out in a near-square grid. ELK's layered
// algorithm would put siblings with no links between them in one row.
func gridColumns(n int) int {
	return int(math.Ceil(math.Sqrt(float64(n))))
}

// quote makes s a D2 string.
func quote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
	return `"` + s + `"`
}

// lastKey returns the last segment of a D2 shape ID such as `i0.i3.i7`.
// d2Script writes keys without dots, so splitting on them is safe.
func lastKey(id string) string {
	parts := strings.Split(id, ".")
	return parts[len(parts)-1]
}
