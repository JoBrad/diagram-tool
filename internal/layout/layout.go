// Package layout positions a graph of nested boxes and links. It knows
// nothing about AWS or any drawing tool: a renderer builds the Graph from
// the model, with the sizes its own shapes need, and reads the positions
// back.
package layout

import "context"

// Item is a box to place. A container holds the items whose Parent names it
// and is sized to fit them; a leaf has a fixed size. IDs are unique.
type Item struct {
	ID string
	// Parent is the containing item's ID, empty at the top level.
	Parent    string
	Label     string
	Container bool
	// Width and Height are a leaf's size in points. Containers ignore them.
	// A layout may return a larger box, as a grid widens a leaf to its
	// column; renderers center an icon in the box it gets.
	Width, Height float64
	// Columns fixes a container's grid width, such as 1 row of n children.
	// Zero picks a near-square grid.
	Columns int
}

// Link is a connection to route between two leaves.
type Link struct {
	ID       string
	From, To string
	Label    string
}

// Graph is the input to a Layouter. Items must list a parent before its
// children.
type Graph struct {
	Items []Item
	Links []Link
}

// Box is a placed item in absolute coordinates, in points, with the origin
// at the top left.
type Box struct {
	X, Y, Width, Height float64
}

// Point is a position in the same coordinates as Box.
type Point struct {
	X, Y float64
}

// Result holds every placed item and routed link.
type Result struct {
	Boxes map[string]Box
	// Routes holds each link's path from its source to its target. When a
	// leaf's label is too wide for it, ELK places the label outside the box
	// and starts or ends the route beyond the label, not on the box edge.
	Routes map[string][]Point
}

// Layouter positions a graph.
type Layouter interface {
	Layout(ctx context.Context, g Graph) (*Result, error)
}
