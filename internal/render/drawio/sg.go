package drawio

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"diagram/internal/model"
	"diagram/internal/platform/azure"
)

// sgStyle is draw.io's AWS palette security group, a plain rectangle with no
// shape name, made a container so its members move with it, and dashed so it
// reads as a grouping rather than a network boundary.
var sgStyle = sgStyleFor("#DD3522")

// sgStyleFor is the security group box drawn in color, for a group the
// config colors.
func sgStyleFor(color string) string {
	return "fillColor=none;strokeColor=" + color + ";dashed=1;verticalAlign=top;fontStyle=0;fontColor=" + color + ";" +
		"whiteSpace=wrap;html=1;container=1;pointerEvents=0;collapsible=0;recursiveResize=0;"
}

// sgBox is one security group box: the members a group has inside one
// container (a subnet, or a VPC for resources in no subnet). A group whose
// members span subnets gets a box in each.
type sgBox struct {
	id, parent string
	// container is the subnet or VPC the box is drawn in, at any depth.
	container string
	// groups are the security groups the box stands for. Groups with the same
	// members in a container share one box.
	groups  []*model.Node
	members []string
}

// label names the box after its groups.
func (b *sgBox) label() string {
	names := make([]string, len(b.groups))
	for i, g := range b.groups {
		names[i] = g.Name
	}
	return strings.Join(names, ", ")
}

// data is the box's cell data: its type and its groups' IDs.
func (b *sgBox) data() [][2]string {
	ids := make([]string, len(b.groups))
	for i, g := range b.groups {
		ids[i] = g.ID
	}
	idKey := "awsId"
	if b.groups[0].Platform == azure.Platform {
		idKey = "azureId"
	}
	return [][2]string{{"type", string(b.groups[0].Type)}, {idKey, strings.Join(ids, ", ")}}
}

// sgBoxes nests the security groups inside each container by member set, and
// returns the boxes, parents before children, and the box each placed leaf
// goes in. home maps each drawn leaf to its container.
//
// Within a container, larger groups are placed first, so a group whose
// members are a subset of another's lands inside it. When two groups only
// partly overlap, one box cannot hold the other: the later group keeps the
// members that sit together in one box, and its other members stay where they
// are. Ties go to the group with more rules, which reads as the outer one.
// Every leaf keeps all its groups in its own cell data either way.
func sgBoxes(r *model.Region, home map[string]string) ([]*sgBox, map[string]string) {
	groups := map[string]*model.Node{}
	for _, n := range ofKind(r, isSecurityGroup) {
		groups[n.ID] = n
	}
	containers := map[string]bool{}
	for _, n := range r.Nodes {
		if isNetwork(n) {
			containers[n.ID] = true
		}
	}

	// members[container][group] lists the group's leaves in that container.
	members := map[string]map[string][]string{}
	for _, e := range r.EdgesOf(model.RelMemberOf) {
		container, ok := home[e.From]
		if !ok || !containers[container] || groups[e.To] == nil {
			continue
		}
		if members[container] == nil {
			members[container] = map[string][]string{}
		}
		members[container][e.To] = append(members[container][e.To], e.From)
	}

	var boxes []*sgBox
	inBox := map[string]string{}
	for _, container := range slices.Sorted(maps.Keys(members)) {
		placed := nestGroups(container, members[container], groups)
		for _, b := range placed {
			for _, m := range b.members {
				inBox[m] = b.id
			}
		}
		boxes = append(boxes, placed...)
	}
	return boxes, inBox
}

// nestGroups places one container's groups, larger first, each inside the box
// that already holds most of its members.
func nestGroups(container string, byGroup map[string][]string, groups map[string]*model.Node) []*sgBox {
	candidates := mergeIdentical(byGroup, groups)
	slices.SortFunc(candidates, func(a, b *sgBox) int {
		return cmp.Or(
			cmp.Compare(len(b.members), len(a.members)),
			cmp.Compare(ruleCount(b), ruleCount(a)),
			cmp.Compare(a.groups[0].ID, b.groups[0].ID),
		)
	})

	current := map[string]string{} // leaf -> box (or container) it sits in now
	for _, members := range byGroup {
		for _, m := range members {
			current[m] = container
		}
	}
	for _, b := range candidates {
		counts := map[string]int{}
		for _, m := range b.members {
			counts[current[m]]++
		}
		b.parent = slices.MaxFunc(slices.Sorted(maps.Keys(counts)), func(x, y string) int {
			return cmp.Compare(counts[x], counts[y])
		})
		b.id = b.groups[0].ID + "-in-" + container
		b.container = container
		b.members = slices.DeleteFunc(b.members, func(m string) bool { return current[m] != b.parent })
		for _, m := range b.members {
			current[m] = b.id
		}
	}
	return candidates
}

// mergeIdentical returns one candidate box per distinct member set, holding
// every group with that set, ordered by name.
func mergeIdentical(byGroup map[string][]string, groups map[string]*model.Node) []*sgBox {
	bySet := map[string]*sgBox{}
	for _, id := range slices.Sorted(maps.Keys(byGroup)) {
		members := slices.Sorted(slices.Values(byGroup[id]))
		key := strings.Join(members, ",")
		if b, ok := bySet[key]; ok {
			b.groups = append(b.groups, groups[id])
			continue
		}
		bySet[key] = &sgBox{groups: []*model.Node{groups[id]}, members: members}
	}
	var out []*sgBox
	for _, key := range slices.Sorted(maps.Keys(bySet)) {
		b := bySet[key]
		slices.SortFunc(
			b.groups,
			func(x, y *model.Node) int { return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID)) },
		)
		out = append(out, b)
	}
	return out
}

// ruleCount is the number of rules across a box's groups.
func ruleCount(b *sgBox) int {
	n := 0
	for _, g := range b.groups {
		n += len(g.Rules)
	}
	return n
}
