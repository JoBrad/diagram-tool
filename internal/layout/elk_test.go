package layout

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// testGraph is a VPC with two subnets, a gateway in the VPC, and two
// external nodes linked to the gateway.
func testGraph() Graph {
	return Graph{
		Items: []Item{
			{ID: "vpc", Label: "VPC", Container: true},
			{ID: "subnet-a", Parent: "vpc", Label: "Subnet A", Container: true},
			{ID: "subnet-b", Parent: "vpc", Label: "Subnet B", Container: true},
			{ID: "i-1", Parent: "subnet-a", Label: "web", Width: 60, Height: 60},
			{ID: "i-2", Parent: "subnet-b", Label: "db", Width: 60, Height: 60},
			{ID: "vgw", Parent: "vpc", Label: "gateway", Width: 60, Height: 60},
			{ID: "cgw-1", Label: "office", Width: 60, Height: 60},
			{ID: "cgw-2", Label: "datacenter", Width: 60, Height: 60},
		},
		Links: []Link{
			{ID: "vpn-1", From: "vgw", To: "cgw-1"},
			{ID: "vpn-2", From: "vgw", To: "cgw-2"},
			{ID: "rule", From: "i-1", To: "i-2", Label: "tcp/5432"},
		},
	}
}

func TestELKLayout(t *testing.T) {
	g := testGraph()
	res, err := ELK{}.Layout(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}

	for _, it := range g.Items {
		box, ok := res.Boxes[it.ID]
		if !ok {
			t.Fatalf("no box for %s", it.ID)
		}
		if !it.Container && (box.Width < it.Width || box.Height < it.Height) {
			t.Errorf("%s: size %vx%v, want at least %vx%v", it.ID, box.Width, box.Height, it.Width, it.Height)
		}
		if it.Parent != "" && !inside(box, res.Boxes[it.Parent]) {
			t.Errorf("%s %+v is not inside its parent %s %+v", it.ID, box, it.Parent, res.Boxes[it.Parent])
		}
	}

	for i, a := range g.Items {
		for _, b := range g.Items[i+1:] {
			if a.Parent == b.Parent && overlap(res.Boxes[a.ID], res.Boxes[b.ID]) {
				t.Errorf("siblings %s and %s overlap", a.ID, b.ID)
			}
		}
	}

	for _, l := range g.Links {
		route := res.Routes[l.ID]
		if len(route) < 2 {
			t.Fatalf("%s: route has %d points", l.ID, len(route))
		}
		if !touches(route[0], res.Boxes[l.From]) || !touches(route[len(route)-1], res.Boxes[l.To]) {
			t.Errorf("%s: route %v does not run from %s to %s", l.ID, route, l.From, l.To)
		}
	}
}

func TestELKRejectsBadGraphs(t *testing.T) {
	for name, g := range map[string]Graph{
		"repeated ID": {Items: []Item{{ID: "a", Width: 10, Height: 10}, {ID: "a", Width: 10, Height: 10}}},
		"parent after": {
			Items: []Item{{ID: "child", Parent: "box", Width: 10, Height: 10}, {ID: "box", Container: true}},
		},
		"unknown target": {
			Items: []Item{{ID: "a", Width: 10, Height: 10}},
			Links: []Link{{ID: "l", From: "a", To: "missing"}},
		},
	} {
		if _, err := (ELK{}).Layout(context.Background(), g); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// TestELKAnyID checks that IDs D2 would misread, such as Azure resource IDs
// with dots and slashes, lay out and come back under their own IDs.
func TestELKAnyID(t *testing.T) {
	vnet := "/subscriptions/s/providers/microsoft.network/virtualnetworks/v"
	g := Graph{
		Items: []Item{
			{ID: vnet, Label: "v", Container: true},
			{ID: vnet + "/subnets/a", Parent: vnet, Label: "a", Width: 60, Height: 60},
			{ID: `quote"d`, Label: "q", Width: 60, Height: 60},
		},
		Links: []Link{{ID: "l", From: vnet + "/subnets/a", To: `quote"d`}},
	}
	res, err := ELK{}.Layout(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range g.Items {
		if _, ok := res.Boxes[it.ID]; !ok {
			t.Errorf("no box for %s", it.ID)
		}
	}
	if len(res.Routes["l"]) < 2 {
		t.Errorf("route = %v", res.Routes["l"])
	}
}

func inside(child, parent Box) bool {
	return child.X >= parent.X && child.Y >= parent.Y &&
		child.X+child.Width <= parent.X+parent.Width && child.Y+child.Height <= parent.Y+parent.Height
}

func overlap(a, b Box) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

// touches reports whether p is at the box: within its width, and within a
// label's height above or below it, because ELK starts a route beyond a
// label it placed outside the box.
func touches(p Point, b Box) bool {
	const labelHeight = 40.0
	return p.X >= b.X && p.X <= b.X+b.Width && p.Y >= b.Y-labelHeight && p.Y <= b.Y+b.Height+labelHeight &&
		!math.IsNaN(p.X) && !math.IsNaN(p.Y)
}

// TestELKGrid checks that unlinked siblings wrap into a near-square grid
// instead of one row.
func TestELKGrid(t *testing.T) {
	g := Graph{Items: []Item{{ID: "subnet", Label: "subnet", Container: true}}}
	for i := range 9 {
		g.Items = append(g.Items, Item{ID: fmt.Sprintf("i-%d", i), Parent: "subnet", Label: "x", Width: 60, Height: 60})
	}
	res, err := ELK{}.Layout(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	xs, ys := map[float64]bool{}, map[float64]bool{}
	for _, it := range g.Items[1:] {
		xs[res.Boxes[it.ID].X] = true
		ys[res.Boxes[it.ID].Y] = true
	}
	if len(xs) != 3 || len(ys) != 3 {
		t.Errorf("9 siblings in %d columns and %d rows, want 3 and 3", len(xs), len(ys))
	}
}

func TestGridColumns(t *testing.T) {
	for n, want := range map[int]int{2: 2, 4: 2, 5: 3, 9: 3, 10: 4, 37: 7} {
		if got := gridColumns(n); got != want {
			t.Errorf("gridColumns(%d) = %d, want %d", n, got, want)
		}
	}
}

// TestELKColumns checks that a fixed column count overrides the near-square
// grid: 4 children in 4 columns make one row.
func TestELKColumns(t *testing.T) {
	g := Graph{Items: []Item{{ID: "region", Label: "region", Container: true, Columns: 4}}}
	for i := range 4 {
		g.Items = append(g.Items, Item{ID: fmt.Sprintf("v-%d", i), Parent: "region", Label: "x", Width: 60, Height: 60})
	}
	res, err := ELK{}.Layout(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	ys := map[float64]bool{}
	for _, it := range g.Items[1:] {
		ys[res.Boxes[it.ID].Y] = true
	}
	if len(ys) != 1 {
		t.Errorf("4 children in %d rows, want 1", len(ys))
	}
}

// TestELKGridRowMajor checks that a grid fills left to right, then top to
// bottom: with 2 columns, the second child sits right of the first.
func TestELKGridRowMajor(t *testing.T) {
	g := Graph{Items: []Item{{ID: "box", Label: "box", Container: true, Columns: 2}}}
	for i := range 4 {
		g.Items = append(g.Items, Item{ID: fmt.Sprintf("c-%d", i), Parent: "box", Label: "x", Width: 60, Height: 60})
	}
	res, err := ELK{}.Layout(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	c0, c1, c2 := res.Boxes["c-0"], res.Boxes["c-1"], res.Boxes["c-2"]
	if c1.Y != c0.Y || c1.X <= c0.X || c2.Y <= c0.Y || c2.X != c0.X {
		t.Errorf("c-0 %+v, c-1 %+v, c-2 %+v; want c-1 right of c-0 and c-2 below it", c0, c1, c2)
	}
}
