package aws_sdk

import (
	"context"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/config"
)

// ec2Filters converts a query to EC2 filters. IDs go in as the idFilter
// filter, not the call's IDs parameter, because the IDs parameter fails the
// whole call when any ID is missing from the region.
func ec2Filters(idFilter string, q *config.ResourceQuery) []ec2types.Filter {
	var filters []ec2types.Filter
	if len(q.IDs) > 0 {
		filters = append(filters, ec2types.Filter{Name: awssdk.String(idFilter), Values: q.IDs})
	}
	for _, f := range q.Filters {
		filters = append(filters, ec2types.Filter{Name: awssdk.String(f.Name), Values: f.Values})
	}
	return filters
}

// pager is the method set shared by the SDK's generated EC2 paginators.
type pager[Page any] interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*ec2.Options)) (Page, error)
}

// collectPages drains a paginator and returns the items from every page.
func collectPages[Page, Item any](ctx context.Context, p pager[Page], items func(Page) []Item) ([]Item, error) {
	var all []Item
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, items(page)...)
	}
	return all, nil
}

func collectEC2Instances(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeInstancesInput{Filters: ec2Filters("instance-id", q)}
	pages := ec2.NewDescribeInstancesPaginator(api.ec2, input)
	reservations, err := collectPages(
		ctx,
		pages,
		func(p *ec2.DescribeInstancesOutput) []ec2types.Reservation { return p.Reservations },
	)
	if err != nil {
		return 0, err
	}
	var instances []ec2types.Instance
	for _, res := range reservations {
		instances = append(instances, res.Instances...)
	}
	region.Instances = instances
	return len(instances), nil
}

// DescribeVpnConnections has no pagination; one call returns every match.
func collectEC2VPNConnections(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeVpnConnectionsInput{Filters: ec2Filters("vpn-connection-id", q)}
	out, err := api.ec2.DescribeVpnConnections(ctx, input)
	if err != nil {
		return 0, err
	}
	for i := range out.VpnConnections {
		scrubVPNSecrets(&out.VpnConnections[i])
	}
	region.VPNConnections = out.VpnConnections
	return len(out.VpnConnections), nil
}

// scrubVPNSecrets drops the tunnel pre-shared keys, so they never reach
// logs, exports, or anything else built from the inventory. Both fields
// carry them: CustomerGatewayConfiguration is the full device configuration
// including the keys.
func scrubVPNSecrets(vpn *ec2types.VpnConnection) {
	vpn.CustomerGatewayConfiguration = nil
	if vpn.Options == nil {
		return
	}
	for i := range vpn.Options.TunnelOptions {
		vpn.Options.TunnelOptions[i].PreSharedKey = nil
	}
}

func collectEC2VPCs(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeVpcsInput{Filters: ec2Filters("vpc-id", q)}
	pages := ec2.NewDescribeVpcsPaginator(api.ec2, input)
	vpcs, err := collectPages(ctx, pages, func(p *ec2.DescribeVpcsOutput) []ec2types.Vpc { return p.Vpcs })
	if err != nil {
		return 0, err
	}
	region.VPCs = vpcs
	return len(vpcs), nil
}

func collectEC2Subnets(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeSubnetsInput{Filters: ec2Filters("subnet-id", q)}
	pages := ec2.NewDescribeSubnetsPaginator(api.ec2, input)
	subnets, err := collectPages(ctx, pages, func(p *ec2.DescribeSubnetsOutput) []ec2types.Subnet { return p.Subnets })
	if err != nil {
		return 0, err
	}
	region.Subnets = subnets
	return len(subnets), nil
}

// Security groups carry their rules, including references to other groups in
// IpPermissions[].UserIdGroupPairs, so no separate rules call is needed.
func collectEC2SecurityGroups(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeSecurityGroupsInput{Filters: ec2Filters("group-id", q)}
	pages := ec2.NewDescribeSecurityGroupsPaginator(api.ec2, input)
	groups, err := collectPages(
		ctx,
		pages,
		func(p *ec2.DescribeSecurityGroupsOutput) []ec2types.SecurityGroup { return p.SecurityGroups },
	)
	if err != nil {
		return 0, err
	}
	region.SecurityGroups = groups
	return len(groups), nil
}

func collectEC2InternetGateways(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeInternetGatewaysInput{Filters: ec2Filters("internet-gateway-id", q)}
	pages := ec2.NewDescribeInternetGatewaysPaginator(api.ec2, input)
	igws, err := collectPages(
		ctx,
		pages,
		func(p *ec2.DescribeInternetGatewaysOutput) []ec2types.InternetGateway { return p.InternetGateways },
	)
	if err != nil {
		return 0, err
	}
	region.InternetGateways = igws
	return len(igws), nil
}

// DescribeNatGateways takes Filter, not Filters
func collectEC2NATGateways(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeNatGatewaysInput{Filter: ec2Filters("nat-gateway-id", q)}
	pages := ec2.NewDescribeNatGatewaysPaginator(api.ec2, input)
	nats, err := collectPages(
		ctx,
		pages,
		func(p *ec2.DescribeNatGatewaysOutput) []ec2types.NatGateway { return p.NatGateways },
	)
	if err != nil {
		return 0, err
	}
	region.NATGateways = nats
	return len(nats), nil
}

// DescribeVpnGateways has no pagination; one call returns every match.
func collectEC2VPNGateways(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeVpnGatewaysInput{Filters: ec2Filters("vpn-gateway-id", q)}
	out, err := api.ec2.DescribeVpnGateways(ctx, input)
	if err != nil {
		return 0, err
	}
	region.VPNGateways = out.VpnGateways
	return len(out.VpnGateways), nil
}

func collectEC2TransitGateways(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeTransitGatewaysInput{Filters: ec2Filters("transit-gateway-id", q)}
	pages := ec2.NewDescribeTransitGatewaysPaginator(api.ec2, input)
	tgws, err := collectPages(
		ctx,
		pages,
		func(p *ec2.DescribeTransitGatewaysOutput) []ec2types.TransitGateway { return p.TransitGateways },
	)
	if err != nil {
		return 0, err
	}
	region.TransitGateways = tgws
	return len(tgws), nil
}

// DescribeCustomerGateways has no pagination; one call returns every match.
func collectEC2CustomerGateways(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeCustomerGatewaysInput{Filters: ec2Filters("customer-gateway-id", q)}
	out, err := api.ec2.DescribeCustomerGateways(ctx, input)
	if err != nil {
		return 0, err
	}
	region.CustomerGateways = out.CustomerGateways
	return len(out.CustomerGateways), nil
}

// collectEC2PrefixLists fetches managed prefix lists, including AWS-managed
// ones that security group rules reference. Only each list's ID and name are
// used, so its entries are not fetched.
func collectEC2PrefixLists(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ec2.DescribeManagedPrefixListsInput{Filters: ec2Filters("prefix-list-id", q)}
	lists, err := collectPages(ctx, ec2.NewDescribeManagedPrefixListsPaginator(api.ec2, input),
		func(p *ec2.DescribeManagedPrefixListsOutput) []ec2types.ManagedPrefixList { return p.PrefixLists })
	if err != nil {
		return 0, err
	}
	region.PrefixLists = lists
	return len(lists), nil
}
