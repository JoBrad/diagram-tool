package model

import (
	"reflect"
	"testing"

	"diagram/internal/inventory"
)

// hideRecords is a network with two zones, a server in each, a group holding
// the first server, and a link from a gateway in the network to an outside
// endpoint.
func hideRecords() []inventory.Record {
	return []inventory.Record{
		rec("net", "network", ""),
		rec("zone-a", "zone", "net"),
		rec("zone-b", "zone", "net"),
		member(rec("s-1", "server", "zone-a"), "group"),
		rec("s-2", "server", "zone-b"),
		rec("group", "group", "net"),
		rec("gw", "gateway", "net"),
		rec("end", "endpoint", ""),
		link("l-1", "gw", "end"),
	}
}

func TestHide(t *testing.T) {
	type flags struct{ hide, children bool }
	tests := []struct {
		name string
		set  map[string]flags
		want []string
	}{
		{"nothing hidden", nil, []string{
			"net@", "zone-a@net", "zone-b@net", "s-1@zone-a", "s-2@zone-b", "group@net", "gw@net", "end@",
			"member_of:s-1->group", "l-1",
		}},
		{"hidden zones move their contents to the network", map[string]flags{
			"zone-a": {hide: true}, "zone-b": {hide: true},
		}, []string{
			"net@", "s-1@net", "s-2@net", "group@net", "gw@net", "end@", "member_of:s-1->group", "l-1",
		}},
		{"hidden network moves its contents to the region", map[string]flags{"net": {hide: true}}, []string{
			"zone-a@", "zone-b@", "s-1@zone-a", "s-2@zone-b", "group@", "gw@", "end@", "member_of:s-1->group", "l-1",
		}},
		{"hide_children empties zones but keeps them", map[string]flags{
			"zone-a": {children: true}, "zone-b": {children: true},
		}, []string{
			"net@", "zone-a@net", "zone-b@net", "group@net", "gw@net", "end@", "l-1",
		}},
		{"hide and hide_children drop the network and everything in it", map[string]flags{
			"net": {hide: true, children: true},
		}, []string{"end@"}},
		{"hide_children on a group drops its members", map[string]flags{"group": {children: true}}, []string{
			"net@", "zone-a@net", "zone-b@net", "s-2@zone-b", "group@net", "gw@net", "end@", "l-1",
		}},
		{"hidden groups drop their memberships", map[string]flags{"group": {hide: true}}, []string{
			"net@", "zone-a@net", "zone-b@net", "s-1@zone-a", "s-2@zone-b", "gw@net", "end@", "l-1",
		}},
		{"hidden links", map[string]flags{"l-1": {hide: true}}, []string{
			"net@", "zone-a@net", "zone-b@net", "s-1@zone-a", "s-2@zone-b", "group@net", "gw@net", "end@",
			"member_of:s-1->group",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := hideRecords()
			for i := range records {
				f := tt.set[records[i].ID]
				records[i].Hidden, records[i].HideChildren = f.hide, f.children
			}
			r := &Region{Name: "r1"}
			r.add(hide(records))
			if got := layout(r); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("layout =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}
