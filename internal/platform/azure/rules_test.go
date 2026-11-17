package azure

import (
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"

	"diagram/internal/inventory"
)

func ptr[T any](v T) *T { return &v }

func secRule(
	dir armnetwork.SecurityRuleDirection,
	priority int32,
	p armnetwork.SecurityRulePropertiesFormat,
) *armnetwork.SecurityRule {
	p.Direction, p.Priority = &dir, &priority
	if p.Access == nil {
		p.Access = ptr(armnetwork.SecurityRuleAccessAllow)
	}
	return &armnetwork.SecurityRule{Properties: &p}
}

func TestSecurityRules(t *testing.T) {
	in, out := armnetwork.SecurityRuleDirectionInbound, armnetwork.SecurityRuleDirectionOutbound
	rules := []*armnetwork.SecurityRule{
		secRule(out, 100, armnetwork.SecurityRulePropertiesFormat{
			Protocol: ptr(armnetwork.SecurityRuleProtocolAsterisk), DestinationPortRange: ptr("*"),
			DestinationAddressPrefix: ptr("Internet"), SourceAddressPrefix: ptr("10.0.0.0/8"),
		}),
		secRule(in, 4096, armnetwork.SecurityRulePropertiesFormat{
			Protocol: ptr(armnetwork.SecurityRuleProtocolAsterisk), DestinationPortRange: ptr("*"),
			SourceAddressPrefix: ptr("*"), Access: ptr(armnetwork.SecurityRuleAccessDeny),
		}),
		secRule(in, 200, armnetwork.SecurityRulePropertiesFormat{
			Protocol: ptr(
				armnetwork.SecurityRuleProtocolTCP,
			),
			DestinationPortRanges: []*string{ptr("443"), ptr("8000-8080")},
			SourceAddressPrefixes: []*string{ptr("192.0.2.10"), ptr("2001:db8::/32")},
			SourceApplicationSecurityGroups: []*armnetwork.ApplicationSecurityGroup{
				{
					ID: ptr(
						"/subscriptions/S/resourceGroups/RG/providers/Microsoft.Network/applicationSecurityGroups/web",
					),
				},
			},
			Description: ptr("web"),
		}),
		nil,
		{},
	}
	port := func(n int32) *int32 { return &n }
	tcp := func(from, to int32, tt, target string) inventory.Rule {
		return inventory.Rule{
			Protocol: "tcp", FromPort: port(from), ToPort: port(to), TargetType: tt, Target: target, Description: "web",
		}
	}
	asg := "/subscriptions/s/resourcegroups/rg/providers/microsoft.network/applicationsecuritygroups/web"
	want := []inventory.Rule{
		tcp(443, 443, inventory.TargetCIDR, "192.0.2.10"),
		tcp(443, 443, inventory.TargetCIDRv6, "2001:db8::/32"),
		tcp(443, 443, TargetAppSecGroup, asg),
		tcp(8000, 8080, inventory.TargetCIDR, "192.0.2.10"),
		tcp(8000, 8080, inventory.TargetCIDRv6, "2001:db8::/32"),
		tcp(8000, 8080, TargetAppSecGroup, asg),
		{Deny: true, Protocol: "-1", TargetType: TargetServiceTag, Target: "*"},
		{Egress: true, Protocol: "-1", TargetType: TargetServiceTag, Target: "Internet"},
	}
	if got := SecurityRules(rules); !reflect.DeepEqual(got, want) {
		t.Errorf("SecurityRules =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPortRange(t *testing.T) {
	for in, want := range map[string][2]int32{"22": {22, 22}, "1024-65535": {1024, 65535}, " 80 - 81 ": {80, 81}} {
		from, to := portRange(in)
		if from == nil || to == nil || *from != want[0] || *to != want[1] {
			t.Errorf("portRange(%q) = %v, %v; want %v", in, from, to, want)
		}
	}
	for _, in := range []string{"*", "", "a-b"} {
		if from, to := portRange(in); from != nil || to != nil {
			t.Errorf("portRange(%q) = %v, %v; want nil", in, from, to)
		}
	}
}

func TestResourceType(t *testing.T) {
	for in, want := range map[string]string{
		"microsoft.network/virtualnetworks":          TypeVirtualNetwork,
		"Microsoft.ContainerService/ManagedClusters": TypeManagedCluster,
		"Microsoft.Web/sites":                        "Web/sites",
		"Dynatrace.Observability/monitors":           "Dynatrace.Observability/monitors",
		"microsoft.":                                 "microsoft.",
	} {
		if got := ResourceType(in); got != want {
			t.Errorf("ResourceType(%q) = %q, want %q", in, got, want)
		}
	}
}
