package azure

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"

	"diagram/internal/inventory"
)

// SecurityRules flattens a network security group's rules to one Rule per
// target and destination port range. Rules are listed in the order Azure
// evaluates them: inbound before outbound, each by priority.
//
// An inbound rule's target is where traffic comes from, and an outbound
// rule's is where it goes, as for AWS. Ports are destination ports, the
// ports AWS rules name.
func SecurityRules(rules []*armnetwork.SecurityRule) []inventory.Rule {
	props := make([]*armnetwork.SecurityRulePropertiesFormat, 0, len(rules))
	for _, r := range rules {
		if r != nil && r.Properties != nil {
			props = append(props, r.Properties)
		}
	}
	slices.SortStableFunc(props, func(a, b *armnetwork.SecurityRulePropertiesFormat) int {
		return cmp.Or(
			cmp.Compare(boolInt(egress(a)), boolInt(egress(b))),
			cmp.Compare(val(a.Priority), val(b.Priority)),
		)
	})

	var out []inventory.Rule
	for _, p := range props {
		out = append(out, ruleTargets(p)...)
	}
	return out
}

// ruleTargets returns one rule's targets, each once per port range.
func ruleTargets(p *armnetwork.SecurityRulePropertiesFormat) []inventory.Rule {
	isEgress := egress(p)
	prefix, prefixes, groups := p.SourceAddressPrefix, p.SourceAddressPrefixes, p.SourceApplicationSecurityGroups
	if isEgress {
		prefix, prefixes = p.DestinationAddressPrefix, p.DestinationAddressPrefixes
		groups = p.DestinationApplicationSecurityGroups
	}
	type target struct{ kind, id string }
	var targets []target
	for _, a := range nonEmpty(prefix, prefixes) {
		targets = append(targets, target{addressKind(a), a})
	}
	for _, g := range groups {
		if g != nil && g.ID != nil {
			targets = append(targets, target{TargetAppSecGroup, strings.ToLower(*g.ID)})
		}
	}

	var out []inventory.Rule
	for _, ports := range nonEmpty(p.DestinationPortRange, p.DestinationPortRanges) {
		fromPort, toPort := portRange(ports)
		for _, t := range targets {
			out = append(out, inventory.Rule{
				Egress:      isEgress,
				Deny:        val(p.Access) == armnetwork.SecurityRuleAccessDeny,
				Protocol:    protocol(p.Protocol),
				FromPort:    fromPort,
				ToPort:      toPort,
				TargetType:  t.kind,
				Target:      t.id,
				Description: val(p.Description),
			})
		}
	}
	return out
}

func egress(p *armnetwork.SecurityRulePropertiesFormat) bool {
	return val(p.Direction) == armnetwork.SecurityRuleDirectionOutbound
}

// nonEmpty returns the single value when set, else the list, without empty
// entries. Azure fills one or the other.
func nonEmpty(one *string, many []*string) []string {
	var out []string
	if v := val(one); v != "" {
		out = append(out, v)
	}
	for _, v := range many {
		if s := val(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// addressKind says whether an address prefix is an IPv4 or IPv6 address or
// CIDR, or a service tag.
func addressKind(a string) string {
	if p, err := netip.ParsePrefix(a); err == nil {
		return cidrKind(p.Addr())
	}
	if addr, err := netip.ParseAddr(a); err == nil {
		return cidrKind(addr)
	}
	return TargetServiceTag
}

func cidrKind(a netip.Addr) string {
	if a.Is4() {
		return inventory.TargetCIDR
	}
	return inventory.TargetCIDRv6
}

// protocol returns the rule's protocol as AWS writes it: lowercase, with
// "-1" for all.
func protocol(p *armnetwork.SecurityRuleProtocol) string {
	v := val(p)
	if v == "" || v == armnetwork.SecurityRuleProtocolAsterisk {
		return "-1"
	}
	return strings.ToLower(string(v))
}

// portRange parses "443" or "1024-65535". Every port ("*"), or a range that
// does not parse, gives nil, as AWS rules leave ports unset for all.
func portRange(s string) (from, to *int32) {
	lo, hi, isRange := strings.Cut(s, "-")
	if !isRange {
		hi = lo
	}
	a, errA := strconv.ParseInt(strings.TrimSpace(lo), 10, 32)
	b, errB := strconv.ParseInt(strings.TrimSpace(hi), 10, 32)
	if errA != nil || errB != nil {
		return nil, nil
	}
	f, t := int32(a), int32(b)
	return &f, &t
}

// boolInt is 1 for true, so false sorts first.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
