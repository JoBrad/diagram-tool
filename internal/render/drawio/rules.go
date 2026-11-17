package drawio

import (
	"slices"
	"strings"

	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

// ruleLinks draws inbound security group rules that allow traffic from
// another security group or a prefix list, as edges between the boxes the
// groups are drawn as, labeled with the ports.
//
// A group spanning subnets has a box in each, so every box of the receiving
// group gets its own edge: from the source group's box in the same subnet
// when there is one, else from its first box. A group with no box (its
// members sit outside any subnet or VPC) is stood for by its first drawn
// member, and a prefix list by its own box. Edges within one box and rules
// with nothing drawn at an end are skipped.
func ruleLinks(r *model.Region, present map[string]bool, boxes []*sgBox) []layout.Link {
	ends := ruleEnds(r, present, boxes)

	type pair struct{ from, to string }
	var order []pair
	ports := map[pair][]string{}
	for _, sg := range ofKind(r, isSecurityGroup) {
		targets := ends.boxesOf[sg.ID]
		if len(targets) == 0 {
			if m := ends.firstMember[sg.ID]; m != "" {
				targets = []endpoint{{id: m}}
			}
		}
		for _, rule := range sg.Rules {
			if rule.Egress || rule.Deny ||
				(rule.TargetType != inventory.TargetGroup && rule.TargetType != aws.TargetPrefixList) {
				continue
			}
			if rule.Target == sg.ID || len(targets) == 0 {
				continue
			}
			for _, to := range targets {
				from := ends.source(rule, to)
				if from == "" {
					warnf(
						r.Name,
						"%s: rule %s references %s, which has nothing drawn",
						sg.ID,
						portLabel(rule),
						rule.Target,
					)
					break
				}
				p := pair{from, to.id}
				if from == to.id || slices.Contains(ports[p], portLabel(rule)) {
					continue
				}
				if _, ok := ports[p]; !ok {
					order = append(order, p)
				}
				ports[p] = append(ports[p], portLabel(rule))
			}
		}
	}

	links := make([]layout.Link, 0, len(order))
	for _, p := range order {
		links = append(links, layout.Link{
			ID: "rule-" + p.from + "-" + p.to, From: p.from, To: p.to, Label: strings.Join(ports[p], ", "),
		})
	}
	return links
}

// endpoint is a drawn end of a rule edge: a security group box, with the
// subnet or VPC it sits in, or a single cell.
type endpoint struct {
	id, container string
}

// ruleEndpoints indexes what each group and prefix list is drawn as.
type ruleEndpoints struct {
	boxesOf     map[string][]endpoint // group ID -> its boxes, in drawing order
	firstMember map[string]string     // group ID -> its first drawn member
	present     map[string]bool
}

func ruleEnds(r *model.Region, present map[string]bool, boxes []*sgBox) ruleEndpoints {
	ends := ruleEndpoints{boxesOf: map[string][]endpoint{}, firstMember: map[string]string{}, present: present}
	for _, b := range boxes {
		for _, g := range b.groups {
			ends.boxesOf[g.ID] = append(ends.boxesOf[g.ID], endpoint{id: b.id, container: b.container})
		}
	}
	for _, e := range r.EdgesOf(model.RelMemberOf) {
		if _, ok := ends.firstMember[e.To]; !ok && present[e.From] {
			ends.firstMember[e.To] = e.From
		}
	}
	return ends
}

// source returns the drawn end a rule's traffic comes from, for an edge into
// to: the prefix list's box, or the source group's box in to's subnet, else
// its first box, else its first member. It returns "" when nothing is drawn.
func (e ruleEndpoints) source(rule model.Rule, to endpoint) string {
	if rule.TargetType == aws.TargetPrefixList {
		if e.present[rule.Target] {
			return rule.Target
		}
		return ""
	}
	boxes := e.boxesOf[rule.Target]
	if len(boxes) == 0 {
		return e.firstMember[rule.Target]
	}
	if i := slices.IndexFunc(boxes, func(b endpoint) bool { return b.container == to.container }); i >= 0 {
		return boxes[i].id
	}
	return boxes[0].id
}
