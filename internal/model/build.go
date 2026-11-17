package model

import (
	"diagram/internal/inventory"
)

// add turns records into the region's nodes and edges. A record that
// connects two others becomes an edge carrying its own ID; every other
// record becomes a node, with an edge for each group it is a member of.
func (r *Region) add(records []inventory.Record) {
	for _, rec := range records {
		if isEdge(rec) {
			r.Edges = append(r.Edges, &Edge{ID: rec.ID, Relation: Relation(rec.Relation), From: rec.From, To: rec.To})
			continue
		}
		for _, ref := range rec.Refs {
			if ref.Relation != inventory.RelMemberOf {
				r.warnf("%s: reference %s to %s %s is not drawn", rec.ID, ref.Relation, ref.Type, ref.ID)
				continue
			}
			r.Edges = append(r.Edges, &Edge{Relation: RelMemberOf, From: rec.ID, To: ref.ID})
		}
		r.Nodes = append(r.Nodes, &Node{
			ID:       rec.ID,
			Type:     ResourceType(rec.Type),
			Platform: rec.Platform,
			Name:     rec.Name,
			Parent:   rec.Parent,
			Zone:     rec.Zone,
			Tier:     rec.Tier,
			Attrs:    rec.Attrs,
			Rules:    rec.Rules,
		})
	}
	r.byID = nil
}

// isEdge reports whether a record connects two others rather than being
// drawn as a node.
func isEdge(rec inventory.Record) bool {
	return rec.From != "" || rec.To != ""
}

// parents maps each record to its parent.
func parents(records []inventory.Record) map[string]string {
	out := make(map[string]string, len(records))
	for _, rec := range records {
		out[rec.ID] = rec.Parent
	}
	return out
}

// reparent moves each record whose parent is gone up to the nearest ancestor
// that is still present, so a parent is always a record of the region.
func reparent(records []inventory.Record, parentOf map[string]string, present func(string) bool) {
	for i := range records {
		p := records[i].Parent
		for p != "" && !present(p) {
			p = parentOf[p]
		}
		records[i].Parent = p
	}
}
