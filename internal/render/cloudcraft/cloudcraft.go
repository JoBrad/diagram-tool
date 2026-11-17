// Package cloudcraft renders a model.Diagram as Cloudcraft blueprint data.
//
// Component IDs are the AWS resource IDs, as in Cloudcraft's own scans. Group
// members, rule targets, edge ends, and label anchors can then name a
// resource directly, and rebuilding a diagram reproduces the same IDs.
//
// Several things here exist only because of how Cloudcraft draws: empty
// groups get placeholder nodes, prefix lists become stand-in security groups,
// and labels are anchored to their node with relTo.
//
// Rendering one region runs in three passes:
//  1. drawers add nodes, each with a placement saying where it belongs.
//  2. layout assigns every node a position from its placement.
//  3. linkers add groups and edges, which can only refer to drawn nodes.
//
// To add a resource type, write a drawer or linker for it and register it in
// drawers or linkers.
package cloudcraft

import (
	"log"

	cc "github.com/DataDog/cloudcraft-go"

	"diagram/internal/config"
	"diagram/internal/model"
)

// component is one Cloudcraft node, group, edge, or text item. Cloudcraft has
// no published schema for these, so they are built as JSON objects matching
// exported diagrams.
type component = map[string]any

// drawer adds components for one resource type in one region.
type drawer func(r *model.Region, d *diagram)

// drawers add nodes. They run before layout, in this order.
var drawers = []drawer{
	drawInstances,
	drawNATGateways,
	drawVPNGateways,
	drawInternetGateways,
	drawTransitGateways,
	drawCustomerGateways,
	drawPrefixLists,
	// Last, so it sees every other node's placement.
	drawContainerPlaceholders,
}

// linkers add groups and edges. They run after layout and in this order,
// because linkContainers puts the security groups that linkSecurityGroups
// drew inside their VPCs.
var linkers = []drawer{
	linkSecurityGroups,
	linkContainers,
	linkVPNConnections,
}

// diagram accumulates the components for one region.
type diagram struct {
	region     string
	nodes      []component
	groups     []component
	edges      []component
	text       []component
	placements []placement
	// drawn holds the ID of every node and group added so far.
	drawn map[string]bool
}

// Build returns blueprint data drawing every region side by side, with the
// display options cfg sets.
func Build(name string, cfg *config.Config, m *model.Diagram) *cc.BlueprintData {
	data := &cc.BlueprintData{Name: name, Grid: "standard"}
	if cfg.Diagram.Projection != "" {
		data.Projection = string(cfg.Diagram.Projection)
	}
	if cfg.Diagram.Theme != "" {
		data.Theme = &cc.Theme{Base: string(cfg.Diagram.Theme)}
	}
	// Cloudcraft hides every layer named in disabledLayers.
	if cfg.Diagram.ShowGrid != nil && !*cfg.Diagram.ShowGrid {
		data.DisabledLayers = append(data.DisabledLayers, "grid")
	}
	offsetX := 0.0
	for _, r := range m.Regions {
		d := &diagram{region: r.Name, drawn: map[string]bool{}}
		for _, draw := range drawers {
			draw(r, d)
		}
		width := d.layout(offsetX)
		for _, link := range linkers {
			link(r, d)
		}
		offsetX += width + regionGap

		data.Nodes = append(data.Nodes, d.nodes...)
		data.Groups = append(data.Groups, d.groups...)
		data.Edges = append(data.Edges, d.edges...)
		data.Text = append(data.Text, d.text...)
	}
	return data
}

// addNode adds a node and records where layout should place it.
func (d *diagram) addNode(c component, p placement) {
	id := c["id"].(string)
	p.node = c
	d.nodes = append(d.nodes, c)
	d.placements = append(d.placements, p)
	d.drawn[id] = true
}

// addGroup adds a group. Cloudcraft sizes a group around its members, so a
// group with none cannot be shown; addGroup skips it and returns false.
func (d *diagram) addGroup(c component) bool {
	if len(c["nodes"].([]string)) == 0 {
		return false
	}
	d.groups = append(d.groups, c)
	d.drawn[c["id"].(string)] = true
	return true
}

// label adds a text label anchored to a node, so the label moves with it.
func (d *diagram) label(nodeID, text string) {
	d.text = append(d.text, component{
		"id":   nodeID + "-label",
		"type": "isotext",
		"text": text,
		"mapPos": component{
			"relTo":  nodeID,
			"offset": []float64{0.5, 1},
		},
		"outline":          true,
		"standing":         false,
		"textSize":         16,
		"textWrap":         true,
		"direction":        "down",
		"isometric":        true,
		"horizontalOrigin": "center",
	})
}

// warnf reports something left out of the diagram, so it surfaces for
// triage instead of disappearing.
func (d *diagram) warnf(format string, args ...any) {
	log.Printf("WARN %s: "+format, append([]any{d.region}, args...)...)
}
