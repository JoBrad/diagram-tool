package aws

import (
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
)

// PermissionRules flattens EC2 security group permissions to one Rule per
// target, in the order AWS lists them.
func PermissionRules(perms []ec2types.IpPermission, egress bool) []inventory.Rule {
	var out []inventory.Rule
	for _, p := range perms {
		rule := func(tt, target string, description *string) inventory.Rule {
			return inventory.Rule{
				Egress: egress, Protocol: awssdk.ToString(p.IpProtocol), FromPort: p.FromPort, ToPort: p.ToPort,
				TargetType: tt, Target: target, Description: awssdk.ToString(description),
			}
		}
		for _, ip := range p.IpRanges {
			out = append(out, rule(inventory.TargetCIDR, awssdk.ToString(ip.CidrIp), ip.Description))
		}
		for _, ip := range p.Ipv6Ranges {
			out = append(out, rule(inventory.TargetCIDRv6, awssdk.ToString(ip.CidrIpv6), ip.Description))
		}
		for _, pair := range p.UserIdGroupPairs {
			out = append(out, rule(inventory.TargetGroup, awssdk.ToString(pair.GroupId), pair.Description))
		}
		for _, pl := range p.PrefixListIds {
			out = append(out, rule(TargetPrefixList, awssdk.ToString(pl.PrefixListId), pl.Description))
		}
	}
	return out
}
