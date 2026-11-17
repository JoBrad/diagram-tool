package aws

import (
	"reflect"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
)

func TestPermissionRules(t *testing.T) {
	s := awssdk.String
	perms := []ec2types.IpPermission{
		{
			IpProtocol: s("tcp"), FromPort: awssdk.Int32(22), ToPort: awssdk.Int32(22),
			IpRanges:         []ec2types.IpRange{{CidrIp: s("10.0.0.0/8"), Description: s("vpn")}},
			Ipv6Ranges:       []ec2types.Ipv6Range{{CidrIpv6: s("::/0")}},
			UserIdGroupPairs: []ec2types.UserIdGroupPair{{GroupId: s("sg-2")}},
			PrefixListIds:    []ec2types.PrefixListId{{PrefixListId: s("pl-1")}},
		},
		{IpProtocol: s("-1"), IpRanges: []ec2types.IpRange{{CidrIp: s("0.0.0.0/0")}}},
	}
	ssh := func(tt, target, description string) inventory.Rule {
		return inventory.Rule{
			Egress: true, Protocol: "tcp", FromPort: awssdk.Int32(22), ToPort: awssdk.Int32(22),
			TargetType: tt, Target: target, Description: description,
		}
	}
	want := []inventory.Rule{
		ssh("cidr", "10.0.0.0/8", "vpn"),
		ssh("cidr6", "::/0", ""),
		ssh("security_group", "sg-2", ""),
		ssh("prefix_list", "pl-1", ""),
		{Egress: true, Protocol: "-1", TargetType: "cidr", Target: "0.0.0.0/0"},
	}
	if got := PermissionRules(perms, true); !reflect.DeepEqual(got, want) {
		t.Errorf("PermissionRules = %+v, want %+v", got, want)
	}
	if got := PermissionRules(nil, false); got != nil {
		t.Errorf("PermissionRules(nil) = %+v, want nil", got)
	}
}
