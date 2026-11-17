// Package drawio renders a model.Diagram as a draw.io file.
//
// Unlike Cloudcraft, draw.io takes explicit geometry, so the layout comes
// from a layout.Layouter: containers get the sizes it computed, children are
// placed relative to their parent. Edges start from the layout's bends, and
// draw.io reroutes them orthogonally around shapes once one moves. Resource
// attributes are kept as cell data, which draw.io shows under Edit Data.
package drawio

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"log"
	"net/url"
	"strings"

	"diagram/internal/config"
	"diagram/internal/layout"
	"diagram/internal/model"
)

// cell is one draw.io vertex or edge, before it is written as XML.
type cell struct {
	id, parent, label, style string
	// edge cells have a source and target; vertices have a box.
	edge           bool
	source, target string
	// data is written as cell attributes, shown under Edit Data.
	data [][2]string
	// icon cells are drawn iconSize square at the top center of their
	// layout slot.
	icon bool
}

// Labels use Roboto, and literal values such as IDs, addresses, CIDRs, and
// ports use Roboto Mono. A draw.io file cannot embed a font, so every cell
// names fontSource, a stylesheet that serves both, and draw.io loads it when
// online. Offline, labels fall back to draw.io's default font.
const (
	sansFont = "Roboto"
	monoFont = "Roboto Mono"
)

var fontSource = url.QueryEscape(
	"https://fonts.googleapis.com/css2?family=Roboto:wght@400;700&family=Roboto+Mono:wght@400;700&display=swap",
)

// withText adds the text defaults to a cell style: the default text color
// and the sans font unless the style names its own, and the stylesheet that
// serves the fonts.
func withText(style string) string {
	if !strings.Contains(style, "fontColor=") {
		style += "fontColor=" + textColor + ";"
	}
	if !strings.Contains(style, "fontFamily=") {
		style += "fontFamily=" + sansFont + ";"
	}
	return style + "fontSource=" + fontSource + ";"
}

// mono sets s in the mono font, inside an html=1 label.
func mono(s string) string {
	return `<span style="font-family:` + monoFont + `">` + html.EscapeString(s) + "</span>"
}

// Render lays the diagram out with l, colors it with colors, and returns it
// as a draw.io file.
func Render(
	ctx context.Context,
	name string,
	m *model.Diagram,
	l layout.Layouter,
	colors config.Colors,
) ([]byte, error) {
	g, cells := graph(m, colors)
	res, err := l.Layout(ctx, g)
	if err != nil {
		return nil, err
	}

	root := []any{
		mxCell{ID: "0"},
		mxCell{ID: "1", Parent: "0"},
	}
	for _, c := range cells {
		root = append(root, c.xml(res))
	}

	file := mxFile{
		Host: "cloudcraft",
		Diagram: mxDiagram{
			Name: name, ID: "diagram",
			Model: mxGraphModel{Grid: "1", GridSize: "10", Guides: "1", Page: "0", Root: mxRoot{Cells: root}},
		},
	}
	out, err := xml.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode draw.io XML: %w", err)
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// xml converts a cell to its XML element, placing it from the layout.
func (c cell) xml(res *layout.Result) any {
	mc := mxCell{Style: withText(c.style), Parent: "1"}
	if c.edge {
		mc.Edge = "1"
		mc.Source, mc.Target = c.source, c.target
		mc.Geometry = &mxGeometry{Relative: "1", As: "geometry", Points: waypoints(res.Routes[c.id])}
		if c.label != "" {
			mc.Geometry.X = edgeLabelAt
		}
	} else {
		mc.Vertex = "1"
		if c.parent != "" {
			mc.Parent = c.parent
		}
		box := res.Boxes[c.id]
		if c.icon {
			box = layout.Box{X: box.X + (box.Width-iconSize)/2, Y: box.Y, Width: iconSize, Height: iconSize}
		}
		// Child geometry is relative to the parent drawn in the file, which
		// can sit above the layout parent when that is a layout-only block.
		if p, ok := res.Boxes[c.parent]; ok && c.parent != "" {
			box.X -= p.X
			box.Y -= p.Y
		}
		mc.Geometry = &mxGeometry{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, As: "geometry"}
	}

	attrs := []xml.Attr{{Name: xml.Name{Local: "label"}, Value: c.label}, {Name: xml.Name{Local: "id"}, Value: c.id}}
	for _, kv := range c.data {
		attrs = append(attrs, xml.Attr{Name: xml.Name{Local: kv[0]}, Value: kv[1]})
	}
	return userObject{Attrs: attrs, Cell: mc}
}

// edgeLabelAt places an edge's label along it, from -1 at the source to 1 at
// the target. Rule edges run from the side whose traffic a rule allows, so
// their ports sit near that side, where a reader looks for them, rather than
// midway along a long route.
const edgeLabelAt = -0.7

// waypoints returns a route's interior bends as draw.io waypoints, or nil for
// a straight route. The route's own ends are left out: they sit on the
// layout slot, not the shape, and draw.io joins each end to its shape
// itself. Edge styles carry libavoidRouting, so draw.io treats these points
// as the router's: it replaces them when a connected shape moves, instead of
// leaving the edge stuck on its old path.
func waypoints(route []layout.Point) *mxArray {
	if len(route) <= 2 {
		return nil
	}
	pts := &mxArray{As: "points"}
	for _, p := range route[1 : len(route)-1] {
		pts.Points = append(pts.Points, mxPoint{X: p.X, Y: p.Y})
	}
	return pts
}

// warnf reports something left out of the diagram, so it surfaces for triage
// instead of disappearing.
func warnf(region, format string, args ...any) {
	log.Printf("WARN %s: "+format, append([]any{region}, args...)...)
}

// draw.io XML elements.
type (
	mxFile struct {
		XMLName xml.Name  `xml:"mxfile"`
		Host    string    `xml:"host,attr"`
		Diagram mxDiagram `xml:"diagram"`
	}
	mxDiagram struct {
		Name  string       `xml:"name,attr"`
		ID    string       `xml:"id,attr"`
		Model mxGraphModel `xml:"mxGraphModel"`
	}
	mxGraphModel struct {
		Grid     string `xml:"grid,attr"`
		GridSize string `xml:"gridSize,attr"`
		Guides   string `xml:"guides,attr"`
		Page     string `xml:"page,attr"`
		Root     mxRoot `xml:"root"`
	}
	mxRoot struct {
		Cells []any
	}
	// userObject wraps a cell to carry custom attributes; the cell's ID
	// moves onto it.
	userObject struct {
		XMLName xml.Name   `xml:"UserObject"`
		Attrs   []xml.Attr `xml:",any,attr"`
		Cell    mxCell
	}
	mxCell struct {
		XMLName  xml.Name    `xml:"mxCell"`
		ID       string      `xml:"id,attr,omitempty"`
		Style    string      `xml:"style,attr,omitempty"`
		Parent   string      `xml:"parent,attr,omitempty"`
		Vertex   string      `xml:"vertex,attr,omitempty"`
		Edge     string      `xml:"edge,attr,omitempty"`
		Source   string      `xml:"source,attr,omitempty"`
		Target   string      `xml:"target,attr,omitempty"`
		Geometry *mxGeometry `xml:"mxGeometry,omitempty"`
	}
	mxGeometry struct {
		X        float64  `xml:"x,attr,omitempty"`
		Y        float64  `xml:"y,attr,omitempty"`
		Width    float64  `xml:"width,attr,omitempty"`
		Height   float64  `xml:"height,attr,omitempty"`
		Relative string   `xml:"relative,attr,omitempty"`
		As       string   `xml:"as,attr"`
		Points   *mxArray `xml:"Array,omitempty"`
	}
	mxArray struct {
		As     string    `xml:"as,attr"`
		Points []mxPoint `xml:"mxPoint"`
	}
	mxPoint struct {
		X float64 `xml:"x,attr"`
		Y float64 `xml:"y,attr"`
	}
)
