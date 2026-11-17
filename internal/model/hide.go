package model

import (
	"slices"

	"diagram/internal/inventory"
)

// hide removes the records the config hides, and returns the rest.
//
// A record whose parent is hidden moves up to the nearest ancestor that is
// still drawn: a hidden subnet's resources sit in its VPC. HideChildren
// removes everything inside a record, at any depth, and every member of a
// group. A connection to a hidden record goes with it, as does a membership
// in a hidden group.
func hide(records []inventory.Record) []inventory.Record {
	children := map[string][]string{}
	members := map[string][]string{}
	for _, rec := range records {
		children[rec.Parent] = append(children[rec.Parent], rec.ID)
		for _, ref := range rec.Refs {
			if ref.Relation == inventory.RelMemberOf {
				members[ref.ID] = append(members[ref.ID], rec.ID)
			}
		}
	}

	hidden := map[string]bool{}
	var hideBelow func(id string)
	hideBelow = func(id string) {
		for _, c := range slices.Concat(children[id], members[id]) {
			if !hidden[c] {
				hidden[c] = true
				hideBelow(c)
			}
		}
	}
	for _, rec := range records {
		if rec.Hidden {
			hidden[rec.ID] = true
		}
	}
	for _, rec := range records {
		if rec.HideChildren {
			hideBelow(rec.ID)
		}
	}
	if len(hidden) == 0 {
		return records
	}

	parentOf := parents(records)
	out := slices.DeleteFunc(slices.Clone(records), func(rec inventory.Record) bool {
		return hidden[rec.ID] || (isEdge(rec) && (hidden[rec.From] || hidden[rec.To]))
	})
	reparent(out, parentOf, func(id string) bool { return !hidden[id] })
	for i := range out {
		out[i].Refs = slices.DeleteFunc(slices.Clone(out[i].Refs), func(ref inventory.Ref) bool {
			return hidden[ref.ID]
		})
	}
	return out
}
