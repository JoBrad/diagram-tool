package azure

import (
	"reflect"
	"slices"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

func rec(id, rt string, refs ...inventory.Ref) inventory.Record {
	r := record(id, rt, id)
	r.Refs = refs
	r.Attrs = map[string]string{}
	return r
}

func in(rt, id string) inventory.Ref { return ref(inventory.RelContainedIn, rt, id) }
func member(id string) inventory.Ref { return ref(inventory.RelMemberOf, TypeNetworkSecurityGroup, id) }
func nicRef(id string) inventory.Ref { return ref(RelNetworkInterface, TypeNetworkInterface, id) }
func pipRef(id string) inventory.Ref { return ref(RelPublicIP, TypePublicIP, id) }
func withAttr(r inventory.Record, k, v string) inventory.Record {
	r.Attrs[k] = v
	return r
}

// testRegion is a virtual network with two subnets (app, whose network
// security group is nsg-app, and data), a VM with two network interfaces,
// a firewall, a private endpoint, an attached and a loose disk, a key vault,
// and an AKS cluster.
func testRegion() *inventory.Region {
	const vnet = "/x/virtualnetworks/vnet"
	app, data := vnet+"/subnets/app", vnet+"/subnets/data"
	return &inventory.Region{Name: "eastus", Records: []inventory.Record{
		rec(vnet, TypeVirtualNetwork),
		rec(app, TypeSubnet, in(TypeVirtualNetwork, vnet), ref(RelSubnetNSG, TypeNetworkSecurityGroup, "nsg-app")),
		rec(data, TypeSubnet, in(TypeVirtualNetwork, vnet)),
		rec("nsg-app", TypeNetworkSecurityGroup),
		rec("nsg-nic", TypeNetworkSecurityGroup),
		rec("nsg-unused", TypeNetworkSecurityGroup),
		withAttr(rec("nic-1", TypeNetworkInterface, in(TypeSubnet, app), pipRef("pip-1"), member("nsg-nic")),
			AttrPrivateIP, "10.0.1.4"),
		withAttr(rec("nic-2", TypeNetworkInterface, in(TypeSubnet, data)), AttrPrivateIP, "10.0.2.4"),
		withAttr(rec("pip-1", TypePublicIP), AttrIP, "203.0.113.5"),
		withAttr(rec("pip-fw", TypePublicIP), AttrIP, "203.0.113.6"),
		rec("vm-1", TypeVirtualMachine, nicRef("nic-1"), nicRef("nic-2"), nicRef("nic-gone")),
		// Its interface was not read, so it sits in the region.
		rec("vm-2", TypeVirtualMachine, nicRef("nic-gone")),
		rec("fw", TypeFirewall, in(TypeSubnet, vnet+"/subnets/azurefirewallsubnet"), pipRef("pip-fw")),
		rec("pe", TypePrivateEndpoint, in(TypeSubnet, data)),
		rec("disk-os", TypeDisk, ref(RelManagedBy, TypeVirtualMachine, "vm-1")),
		rec("disk-loose", TypeDisk),
		rec("kv", TypeKeyVault),
		Generic(TypeManagedCluster, "aks", "aks", []string{data}),
	}}
}

func TestResolve(t *testing.T) {
	r := testRegion()
	resolveInterfaces(r)
	resolveParents(r)

	const vnet = "/x/virtualnetworks/vnet"
	app, data := vnet+"/subnets/app", vnet+"/subnets/data"
	want := map[string]string{
		vnet: "", app: vnet, data: vnet, "nsg-app": "", "nsg-nic": "", "nsg-unused": "",
		"vm-1": app, "vm-2": "",
		"fw": vnet, // its subnet was not read, so it falls back to the subnet's network
		"pe": data, "disk-os": app, "disk-loose": "", "kv": "", "aks": data,
	}
	got := map[string]string{}
	for _, rec := range r.Records {
		got[rec.ID] = rec.Parent
		for _, ref := range rec.Refs {
			if ref.Relation != inventory.RelMemberOf {
				t.Errorf("%s kept reference %+v", rec.ID, ref)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parents =\n%v\nwant\n%v", got, want)
	}

	vm := r.Records[slices.IndexFunc(r.Records, func(rec inventory.Record) bool { return rec.ID == "vm-1" })]
	if vm.Attrs[AttrPrivateIP] != "10.0.1.4" || vm.Attrs[AttrPublicIPs] != "203.0.113.5" ||
		vm.Tier != inventory.TierPublic {
		t.Errorf("vm-1 = %+v, want its primary interface's IPs and public tier", vm)
	}
	if want := []inventory.Ref{member("nsg-nic"), member("nsg-app")}; !reflect.DeepEqual(vm.Refs, want) {
		t.Errorf("vm-1 refs = %+v, want its interface's and its subnet's groups", vm.Refs)
	}
	fw := r.Records[slices.IndexFunc(r.Records, func(rec inventory.Record) bool { return rec.ID == "fw" })]
	if fw.Attrs[AttrPublicIPs] != "203.0.113.6" {
		t.Errorf("fw = %+v, want its public IP", fw)
	}
}

func TestAddMembership(t *testing.T) {
	refs := addMembership([]inventory.Ref{member("a")}, "a")
	if refs = addMembership(refs, "b"); !reflect.DeepEqual(refs, []inventory.Ref{member("a"), member("b")}) {
		t.Errorf("refs = %+v, want each group once", refs)
	}
	r := inventory.Record{}
	setAttr(&r, "k", "v")
	if r.Attrs["k"] != "v" {
		t.Errorf("setAttr on nil attrs = %+v", r.Attrs)
	}
}

func TestFinish(t *testing.T) {
	no, yes := false, true
	cfg := &config.Config{
		Cloud: config.Clouds{Azure: &config.AzureSubscription{Resources: config.AzureResources{
			Network: &config.AzureNetworkResources{
				Subnets:               &config.AzureResourceQuery{Hide: true},
				NetworkSecurityGroups: &config.AzureResourceQuery{ShowUnconnectedResources: &yes},
				VirtualNetworks:       &config.AzureResourceQuery{IDs: []string{"/x"}, HideChildren: true},
			},
		}}},
		Options: config.Options{ShowUnconnectedResources: &no},
	}
	r := testRegion()
	Finish(cfg, []*inventory.Region{r})

	byID := map[string]inventory.Record{}
	for _, rec := range r.Records {
		byID[rec.ID] = rec
	}
	if _, ok := byID["nic-1"]; ok {
		t.Error("network interfaces were not dropped")
	}
	vnet, app := byID["/x/virtualnetworks/vnet"], byID["/x/virtualnetworks/vnet/subnets/app"]
	if vnet.Prunable || !vnet.HideChildren {
		t.Errorf("vnet = %+v, want listed IDs kept and its children hidden", vnet)
	}
	if !app.Prunable || !app.Hidden {
		t.Errorf("subnet = %+v, want prunable and hidden", app)
	}
	if nsg := byID["nsg-app"]; nsg.Prunable || !reflect.DeepEqual(nsg.KeepIf, []string{"vm-1"}) {
		t.Errorf("nsg-app = %+v, want shown by its type's setting and kept by vm-1", nsg)
	}
	for _, id := range []string{"vm-1", "fw", "pe", "disk-os", "kv", "aks"} {
		if byID[id].Prunable || byID[id].Hidden {
			t.Errorf("%s = %+v, want always drawn", id, byID[id])
		}
	}
}
