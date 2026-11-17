package aws_sdk

import (
	"log"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// collected holds what the collectors fetched for one region, as SDK types,
// until records turns it into inventory records. Each collector writes only
// its own field, which keeps concurrent collection free of locks.
type collected struct {
	Instances        []ec2types.Instance
	VPNConnections   []ec2types.VpnConnection
	InternetGateways []ec2types.InternetGateway
	NATGateways      []ec2types.NatGateway
	VPCs             []ec2types.Vpc
	Subnets          []ec2types.Subnet
	SecurityGroups   []ec2types.SecurityGroup
	VPNGateways      []ec2types.VpnGateway
	TransitGateways  []ec2types.TransitGateway
	CustomerGateways []ec2types.CustomerGateway
	PrefixLists      []ec2types.ManagedPrefixList

	// Sensitive-data stores, already records: only identifying fields are
	// kept. See kms.go, secretsmanager.go, and ssm.go.
	KMSKeys    []inventory.Record
	Secrets    []inventory.Record
	Parameters []inventory.Record
}

// records turns a region's SDK resources into inventory records with the
// platform's builders, skipping ones that no longer exist.
func (c *collected) records(region string) []inventory.Record {
	var out []inventory.Record
	for _, inst := range c.Instances {
		rec, ok := aws.Instance(inst)
		if !ok {
			log.Printf("WARN %s: skip %s: terminated", region, awssdk.ToString(inst.InstanceId))
			continue
		}
		out = append(out, rec)
	}
	out = appendBuilt(out, c.NATGateways, aws.NATGateway)
	out = appendBuilt(out, c.VPNGateways, aws.VPNGateway)
	out = appendAll(out, c.InternetGateways, aws.InternetGateway)
	out = appendBuilt(out, c.TransitGateways, aws.TransitGateway)
	out = appendBuilt(out, c.CustomerGateways, aws.CustomerGateway)
	out = appendAll(out, c.PrefixLists, aws.PrefixList)
	out = appendAll(out, c.VPCs, aws.VPC)
	out = appendAll(out, c.Subnets, aws.Subnet)
	out = appendAll(out, c.SecurityGroups, aws.SecurityGroup)
	out = appendBuilt(out, c.VPNConnections, aws.VPNConnection)
	out = append(out, c.KMSKeys...)
	out = append(out, c.Secrets...)
	return append(out, c.Parameters...)
}

// appendAll appends a record for every item.
func appendAll[T any](out []inventory.Record, items []T, build func(T) inventory.Record) []inventory.Record {
	for _, it := range items {
		out = append(out, build(it))
	}
	return out
}

// appendBuilt appends a record for every item that still exists.
func appendBuilt[T any](
	out []inventory.Record, items []T, build func(T) (inventory.Record, bool),
) []inventory.Record {
	for _, it := range items {
		if rec, ok := build(it); ok {
			out = append(out, rec)
		}
	}
	return out
}
