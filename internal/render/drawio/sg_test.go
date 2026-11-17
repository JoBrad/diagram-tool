package drawio

import (
	"reflect"
	"strings"
	"testing"

	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

// sgRegion builds a region from security groups, each given as its rule
// count, and memberships as leaf -> groups. home places each leaf.
func sgRegion(rules map[string]int, members map[string][]string) *model.Region {
	r := &model.Region{Name: "us-east-1", Nodes: []*model.Node{
		{ID: "vpc-1", Type: aws.TypeVPC},
		{ID: "subnet-a", Type: aws.TypeSubnet, Parent: "vpc-1"},
		{ID: "subnet-b", Type: aws.TypeSubnet, Parent: "vpc-1"},
	}}
	for id, n := range rules {
		r.Nodes = append(r.Nodes, &model.Node{
			ID: id, Type: aws.TypeSecurityGroup, Name: "name-" + id, Rules: make([]model.Rule, n),
		})
	}
	for leaf, groups := range members {
		for _, g := range groups {
			r.Edges = append(r.Edges, &model.Edge{Relation: model.RelMemberOf, From: leaf, To: g})
		}
	}
	return r
}

// tree renders boxes and leaf placement as "child>parent" pairs.
func tree(boxes []*sgBox, inBox map[string]string) map[string]string {
	out := map[string]string{}
	for _, b := range boxes {
		out[b.id] = b.parent
	}
	for leaf, box := range inBox {
		out[leaf] = box
	}
	return out
}

func TestSGBoxes(t *testing.T) {
	inA := func(leaves ...string) map[string]string {
		h := map[string]string{}
		for _, l := range leaves {
			h[l] = "subnet-a"
		}
		return h
	}
	tests := []struct {
		name    string
		rules   map[string]int
		members map[string][]string
		home    map[string]string
		want    map[string]string
	}{
		{
			name:    "subset nests inside superset",
			rules:   map[string]int{"sg-main": 1, "sg-app": 1},
			members: map[string][]string{"i-1": {"sg-main", "sg-app"}, "i-2": {"sg-main"}},
			home:    inA("i-1", "i-2"),
			want: map[string]string{
				"sg-main-in-subnet-a": "subnet-a", "sg-app-in-subnet-a": "sg-main-in-subnet-a",
				"i-1": "sg-app-in-subnet-a", "i-2": "sg-main-in-subnet-a",
			},
		},
		{
			name:    "disjoint groups sit side by side",
			rules:   map[string]int{"sg-x": 1, "sg-y": 1},
			members: map[string][]string{"i-1": {"sg-x"}, "i-2": {"sg-y"}},
			home:    inA("i-1", "i-2"),
			want: map[string]string{
				"sg-x-in-subnet-a": "subnet-a", "sg-y-in-subnet-a": "subnet-a",
				"i-1": "sg-x-in-subnet-a", "i-2": "sg-y-in-subnet-a",
			},
		},
		{
			name:    "group spanning subnets gets a box in each",
			rules:   map[string]int{"sg-main": 1},
			members: map[string][]string{"i-1": {"sg-main"}, "i-2": {"sg-main"}},
			home:    map[string]string{"i-1": "subnet-a", "i-2": "subnet-b"},
			want: map[string]string{
				"sg-main-in-subnet-a": "subnet-a", "sg-main-in-subnet-b": "subnet-b",
				"i-1": "sg-main-in-subnet-a", "i-2": "sg-main-in-subnet-b",
			},
		},
		{
			// Equal sizes: the group with more rules is placed first, so it is
			// the outer box, and the other keeps only the shared member.
			name:    "partial overlap",
			rules:   map[string]int{"sg-big": 5, "sg-small": 1},
			members: map[string][]string{"i-1": {"sg-big"}, "i-2": {"sg-big", "sg-small"}, "i-3": {"sg-small"}},
			home:    inA("i-1", "i-2", "i-3"),
			want: map[string]string{
				"sg-big-in-subnet-a": "subnet-a", "sg-small-in-subnet-a": "sg-big-in-subnet-a",
				"i-1": "sg-big-in-subnet-a", "i-2": "sg-small-in-subnet-a",
			},
		},
		{
			name:    "region-level and ungrouped resources get no box",
			rules:   map[string]int{"sg-main": 1, "sg-unused": 1},
			members: map[string][]string{"fn-1": {"sg-main"}},
			home:    map[string]string{"fn-1": "us-east-1", "i-9": "subnet-a"},
			want:    map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boxes, inBox := sgBoxes(sgRegion(tt.rules, tt.members), tt.home)
			if got := tree(boxes, inBox); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tree = %v, want %v", got, tt.want)
			}
			// Parents come before children, as the layout requires.
			seen := map[string]bool{"subnet-a": true, "subnet-b": true}
			for _, b := range boxes {
				if !seen[b.parent] {
					t.Errorf("box %s comes before its parent %s", b.id, b.parent)
				}
				seen[b.id] = true
			}
		})
	}
}

func TestSGBoxesMergeIdentical(t *testing.T) {
	r := sgRegion(map[string]int{"sg-1": 1, "sg-2": 3}, map[string][]string{"i-1": {"sg-1", "sg-2"}})
	boxes, inBox := sgBoxes(r, map[string]string{"i-1": "subnet-a"})
	if len(boxes) != 1 || inBox["i-1"] != boxes[0].id {
		t.Fatalf("boxes = %+v, inBox = %v; want one shared box", boxes, inBox)
	}
	if got := boxes[0].label(); got != "name-sg-1, name-sg-2" {
		t.Errorf("label = %q, want both names", got)
	}
	data := boxes[0].data()
	if data[0] != [2]string{"type", "AWS::EC2::SecurityGroup"} || !strings.Contains(data[1][1], "sg-1, sg-2") {
		t.Errorf("data = %v, want the rt and both IDs", data)
	}
}
