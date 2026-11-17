package model

import (
	"slices"

	"diagram/internal/inventory"
)

// prune drops the prunable records that nothing keeps, and returns the rest.
//
// Everything starts out kept. A prunable connection is kept while both its
// ends are. Any other prunable record is kept while a record in its KeepIf
// is kept, or while it holds a kept record that is not attached to it. Dropping
// repeats until nothing changes, because dropping one record can leave
// another unconnected: a customer gateway whose only VPN connection was
// dropped, for example.
func prune(records []inventory.Record) []inventory.Record {
	keep := make(map[string]bool, len(records))
	holds := map[string][]string{} // parent -> children that keep it
	for _, rec := range records {
		keep[rec.ID] = true
		if !rec.Attached && rec.Parent != "" {
			holds[rec.Parent] = append(holds[rec.Parent], rec.ID)
		}
	}
	anyKept := func(ids []string) bool {
		return slices.ContainsFunc(ids, func(id string) bool { return keep[id] })
	}

	for changed := true; changed; {
		changed = false
		for _, rec := range records {
			if !rec.Prunable || !keep[rec.ID] {
				continue
			}
			kept := anyKept(rec.KeepIf) || anyKept(holds[rec.ID])
			if isEdge(rec) {
				kept = keep[rec.From] && keep[rec.To]
			}
			if !kept {
				delete(keep, rec.ID)
				changed = true
			}
		}
	}
	if len(keep) == len(records) {
		return records
	}

	parentOf := parents(records)
	out := slices.DeleteFunc(slices.Clone(records), func(rec inventory.Record) bool { return !keep[rec.ID] })
	reparent(out, parentOf, func(id string) bool { return keep[id] })
	return out
}
