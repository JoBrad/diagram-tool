package aws

import (
	"strconv"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
)

// The record builders below each turn one SDK resource into a record. Those
// that return a bool report false for a resource that no longer exists, which
// the caller skips.

// Instance builds an EC2 instance's record. A terminated instance is gone.
func Instance(inst ec2types.Instance) (inventory.Record, bool) {
	id := awssdk.ToString(inst.InstanceId)
	if inst.State != nil && inst.State.Name == ec2types.InstanceStateNameTerminated {
		return inventory.Record{}, false
	}
	publicIP := awssdk.ToString(inst.PublicIpAddress)
	rec := record(id, TypeInstance, nameTag(inst.Tags, id))
	rec.Refs = placement(awssdk.ToString(inst.VpcId), awssdk.ToString(inst.SubnetId))
	rec.Attrs = map[string]string{
		AttrInstanceType:    string(inst.InstanceType),
		AttrPlatformDetails: awssdk.ToString(inst.PlatformDetails),
		AttrPrivateIP:       awssdk.ToString(inst.PrivateIpAddress),
		AttrPublicIP:        publicIP,
	}
	if publicIP != "" {
		rec.Tier = inventory.TierPublic
	}
	for _, sg := range inst.SecurityGroups {
		rec.Refs = append(rec.Refs, ref(inventory.RelMemberOf, TypeSecurityGroup, awssdk.ToString(sg.GroupId)))
	}
	return rec, true
}

// NATGateway builds a NAT gateway's record. A deleting, deleted, or failed
// NAT gateway is gone.
func NATGateway(nat ec2types.NatGateway) (inventory.Record, bool) {
	switch nat.State {
	case ec2types.NatGatewayStateDeleting, ec2types.NatGatewayStateDeleted, ec2types.NatGatewayStateFailed:
		return inventory.Record{}, false
	}
	id := awssdk.ToString(nat.NatGatewayId)
	var ips []string
	for _, a := range nat.NatGatewayAddresses {
		if ip := awssdk.ToString(a.PublicIp); ip != "" {
			ips = append(ips, ip)
		}
	}
	rec := record(id, TypeNATGateway, nameTag(nat.Tags, id))
	rec.Refs = placement(awssdk.ToString(nat.VpcId), awssdk.ToString(nat.SubnetId))
	rec.Attrs = map[string]string{AttrPublicIPs: strings.Join(ips, ",")}
	return rec, true
}

// VPNGateway builds a VPN gateway's record, placed in the VPC it is attached
// to. A deleted VPN gateway is gone.
func VPNGateway(vgw ec2types.VpnGateway) (inventory.Record, bool) {
	if vgw.State == ec2types.VpnStateDeleted {
		return inventory.Record{}, false
	}
	id := awssdk.ToString(vgw.VpnGatewayId)
	var vpc string
	for _, a := range vgw.VpcAttachments {
		if a.State == ec2types.AttachmentStatusAttached {
			vpc = awssdk.ToString(a.VpcId)
		}
	}
	rec := record(id, TypeVPNGateway, nameTag(vgw.Tags, id))
	rec.Refs = placement(vpc, "")
	return rec, true
}

// InternetGateway builds an internet gateway's record, placed in the VPC it
// is attached to.
func InternetGateway(igw ec2types.InternetGateway) inventory.Record {
	id := awssdk.ToString(igw.InternetGatewayId)
	var vpc string
	for _, a := range igw.Attachments {
		// The internet gateway API reports "available", which the SDK has no
		// constant for.
		if a.State == ec2types.AttachmentStatusAttached || string(a.State) == "available" {
			vpc = awssdk.ToString(a.VpcId)
		}
	}
	rec := record(id, TypeInternetGateway, nameTag(igw.Tags, id))
	rec.Refs = placement(vpc, "")
	return rec
}

// TransitGateway builds a transit gateway's record. A deleted transit gateway
// is gone.
func TransitGateway(tgw ec2types.TransitGateway) (inventory.Record, bool) {
	if tgw.State == ec2types.TransitGatewayStateDeleted {
		return inventory.Record{}, false
	}
	id := awssdk.ToString(tgw.TransitGatewayId)
	return record(id, TypeTransitGateway, nameTag(tgw.Tags, id)), true
}

// CustomerGateway builds a customer gateway's record. A deleted customer
// gateway is gone.
func CustomerGateway(cgw ec2types.CustomerGateway) (inventory.Record, bool) {
	if awssdk.ToString(cgw.State) == "deleted" {
		return inventory.Record{}, false
	}
	id := awssdk.ToString(cgw.CustomerGatewayId)
	rec := record(id, TypeCustomerGateway, nameTag(cgw.Tags, id))
	rec.Attrs = map[string]string{AttrIP: awssdk.ToString(cgw.IpAddress)}
	return rec, true
}

// PrefixList builds a managed prefix list's record.
func PrefixList(l ec2types.ManagedPrefixList) inventory.Record {
	return record(awssdk.ToString(l.PrefixListId), TypePrefixList, awssdk.ToString(l.PrefixListName))
}

// VPC builds a VPC's record.
func VPC(v ec2types.Vpc) inventory.Record {
	id := awssdk.ToString(v.VpcId)
	rec := record(id, TypeVPC, nameTag(v.Tags, id))
	rec.Attrs = map[string]string{AttrCIDR: awssdk.ToString(v.CidrBlock)}
	return rec
}

// Subnet builds a subnet's record. Route tables are not collected, so the
// launch flag decides the tier: public when set, else private.
func Subnet(s ec2types.Subnet) inventory.Record {
	id := awssdk.ToString(s.SubnetId)
	public := awssdk.ToBool(s.MapPublicIpOnLaunch)
	rec := record(id, TypeSubnet, nameTag(s.Tags, id))
	rec.Refs = placement(awssdk.ToString(s.VpcId), "")
	rec.Zone = awssdk.ToString(s.AvailabilityZone)
	rec.Tier = inventory.TierPrivate
	if public {
		rec.Tier = inventory.TierPublic
	}
	rec.Attrs = map[string]string{
		AttrCIDR:   awssdk.ToString(s.CidrBlock),
		AttrZone:   rec.Zone,
		AttrPublic: strconv.FormatBool(public),
	}
	return rec
}

// SecurityGroup builds a security group's record, with its rules.
func SecurityGroup(sg ec2types.SecurityGroup) inventory.Record {
	rec := record(awssdk.ToString(sg.GroupId), TypeSecurityGroup, awssdk.ToString(sg.GroupName))
	rec.Refs = placement(awssdk.ToString(sg.VpcId), "")
	rec.Rules = append(PermissionRules(sg.IpPermissions, false), PermissionRules(sg.IpPermissionsEgress, true)...)
	return rec
}

// VPNConnection builds a VPN connection's record: a connection from its VPN
// or transit gateway to its customer gateway. A deleted VPN connection is
// gone.
func VPNConnection(vpn ec2types.VpnConnection) (inventory.Record, bool) {
	if vpn.State == ec2types.VpnStateDeleted {
		return inventory.Record{}, false
	}
	gw := awssdk.ToString(vpn.VpnGatewayId)
	if gw == "" {
		gw = awssdk.ToString(vpn.TransitGatewayId)
	}
	rec := record(awssdk.ToString(vpn.VpnConnectionId), TypeVPNConnection, "")
	rec.From, rec.To, rec.Relation = gw, awssdk.ToString(vpn.CustomerGatewayId), RelVPNConnection
	return rec, true
}

// KMSKey builds a KMS key's record. Only its ID and display name are kept.
func KMSKey(id, name string) inventory.Record {
	return record(id, TypeKMSKey, name)
}

// Secret builds a Secrets Manager secret's record, by ARN and name only.
func Secret(arn, name string) inventory.Record {
	return record(arn, TypeSecret, name)
}

// Parameter builds an SSM parameter's record, by ARN and name only.
func Parameter(arn, name string) inventory.Record {
	return record(arn, TypeParameter, name)
}

// Generic builds the record of a resource type with no builder of its own,
// such as one read through include_all: its ID, name, placement, and
// security groups. A resource in several subnets gets a reference to each;
// Finish keeps one.
func Generic(rt, id, name, vpc string, subnets, groups []string) inventory.Record {
	rec := record(id, rt, name)
	for _, s := range subnets {
		rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, s))
	}
	rec.Refs = append(rec.Refs, placement(vpc, "")...)
	for _, g := range groups {
		rec.Refs = append(rec.Refs, ref(inventory.RelMemberOf, TypeSecurityGroup, g))
	}
	return rec
}

// record starts an AWS record.
func record(id, rt, name string) inventory.Record {
	return inventory.Record{ID: id, Type: rt, Platform: Platform, Name: name}
}

// placement returns references to a resource's subnet and VPC, when set.
func placement(vpc, subnet string) []inventory.Ref {
	var refs []inventory.Ref
	if subnet != "" {
		refs = append(refs, ref(inventory.RelContainedIn, TypeSubnet, subnet))
	}
	if vpc != "" {
		refs = append(refs, ref(inventory.RelContainedIn, TypeVPC, vpc))
	}
	return refs
}

func ref(rel inventory.Relation, resourceType, id string) inventory.Ref {
	return inventory.Ref{Relation: rel, Type: resourceType, ID: id}
}

// nameTag returns the Name tag, or fallback when there is none.
func nameTag(tags []ec2types.Tag, fallback string) string {
	for _, t := range tags {
		if t.Key != nil && *t.Key == "Name" && t.Value != nil {
			return *t.Value
		}
	}
	return fallback
}

// NameTags returns tags holding name as the Name tag, or none for an empty
// name, for sources that read a resource's name rather than its tags.
func NameTags(name string) []ec2types.Tag {
	if name == "" {
		return nil
	}
	return []ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String(name)}}
}
