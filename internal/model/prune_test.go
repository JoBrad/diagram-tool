package model

import (
	"slices"
	"testing"

	"diagram/internal/inventory"
)

// prunable marks r prunable, kept while any of keepIf is.
func prunable(r inventory.Record, keepIf ...string) inventory.Record {
	r.Prunable, r.KeepIf = true, keepIf
	return r
}

// attached returns r as an attachment: it does not keep its parent.
func attached(r inventory.Record) inventory.Record {
	r.Attached = true
	return r
}

func ids(records []inventory.Record) []string {
	var out []string
	for _, r := range records {
		out = append(out, r.ID+"@"+r.Parent)
	}
	return out
}

func TestPrune(t *testing.T) {
	tests := []struct {
		name    string
		records []inventory.Record
		want    []string
	}{
		{
			name: "records that are not prunable stay",
			records: []inventory.Record{
				rec("net", "network", ""), rec("server", "server", ""),
			},
			want: []string{"net@", "server@"},
		},
		{
			// A container is kept only by what it holds: an empty zone does
			// not keep its network, and the network does not keep it.
			name: "a container is kept by a kept child",
			records: []inventory.Record{
				prunable(rec("net", "network", "")),
				prunable(rec("zone-a", "zone", "net")),
				prunable(rec("zone-empty", "zone", "net")),
				rec("server", "server", "zone-a"),
				prunable(rec("net-empty", "network", "")),
			},
			want: []string{"net@", "zone-a@net", "server@zone-a"},
		},
		{
			// The pair would otherwise keep each other.
			name: "an attachment does not keep its parent",
			records: []inventory.Record{
				prunable(rec("net", "network", "")),
				prunable(attached(rec("gw", "gateway", "net")), "net"),
			},
			want: nil,
		},
		{
			name: "an attachment is kept with a kept parent",
			records: []inventory.Record{
				prunable(rec("net", "network", "")),
				rec("server", "server", "net"),
				prunable(attached(rec("gw", "gateway", "net")), "net"),
			},
			want: []string{"net@", "server@net", "gw@net"},
		},
		{
			// The link goes for its missing end, which leaves the gateway on
			// its other end with nothing keeping it.
			name: "dropping a link cascades to its ends",
			records: []inventory.Record{
				prunable(rec("gw", "gateway", ""), "l-1"),
				prunable(link("l-1", "gw", "missing")),
				prunable(rec("gw-2", "gateway", ""), "l-2"),
				prunable(rec("end-2", "endpoint", ""), "l-2"),
				prunable(link("l-2", "gw-2", "end-2")),
			},
			want: []string{"gw-2@", "end-2@", "l-2@"},
		},
		{
			name: "a link that is not prunable stays without its ends",
			records: []inventory.Record{
				link("l-1", "gw", "missing"),
			},
			want: []string{"l-1@"},
		},
		{
			// The attachment is kept by override and its parent is pruned,
			// so it moves up to the region.
			name: "a kept record whose parent goes moves up",
			records: []inventory.Record{
				prunable(rec("net", "network", "")),
				prunable(rec("zone", "zone", "net")),
				attached(rec("gw", "gateway", "zone")),
			},
			want: []string{"gw@"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(prune(tt.records)); !slices.Equal(got, tt.want) {
				t.Errorf("kept %v, want %v", got, tt.want)
			}
		})
	}
}
