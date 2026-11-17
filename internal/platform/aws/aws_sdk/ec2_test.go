package aws_sdk

import (
	"context"
	"reflect"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/config"
)

func TestEC2Filters(t *testing.T) {
	if got := ec2Filters("vpc-id", &config.ResourceQuery{}); got != nil {
		t.Errorf("empty query = %+v, want nil", got)
	}
	q := &config.ResourceQuery{
		IDs:     []string{"vpc-1", "vpc-2"},
		Filters: []config.Filter{{Name: "tag:Env", Values: []string{"prod"}}},
	}
	want := []ec2types.Filter{
		{Name: awssdk.String("vpc-id"), Values: []string{"vpc-1", "vpc-2"}},
		{Name: awssdk.String("tag:Env"), Values: []string{"prod"}},
	}
	if got := ec2Filters("vpc-id", q); !reflect.DeepEqual(got, want) {
		t.Errorf("ec2Filters = %+v, want %+v", got, want)
	}
}

// Each EC2 collector sends the query's IDs as its own ID filter, followed by
// the query's filters.
func TestEC2CollectorsSendFilters(t *testing.T) {
	tests := []struct {
		collector string
		idFilter  string
		filters   func(f *fakeCalls) []ec2types.Filter
	}{
		{
			"ec2.instances", "instance-id",
			filtersOf(func(in *ec2.DescribeInstancesInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.vpn_connections", "vpn-connection-id",
			filtersOf(func(in *ec2.DescribeVpnConnectionsInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.vpcs", "vpc-id",
			filtersOf(func(in *ec2.DescribeVpcsInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.subnets", "subnet-id",
			filtersOf(func(in *ec2.DescribeSubnetsInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.security_groups", "group-id",
			filtersOf(func(in *ec2.DescribeSecurityGroupsInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.nat_gateways", "nat-gateway-id",
			// DescribeNatGateways takes Filter, not Filters.
			filtersOf(func(in *ec2.DescribeNatGatewaysInput) []ec2types.Filter { return in.Filter }),
		},
		{
			"ec2.internet_gateways", "internet-gateway-id",
			filtersOf(func(in *ec2.DescribeInternetGatewaysInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.vpn_gateways", "vpn-gateway-id",
			filtersOf(func(in *ec2.DescribeVpnGatewaysInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.transit_gateways", "transit-gateway-id",
			filtersOf(func(in *ec2.DescribeTransitGatewaysInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.customer_gateways", "customer-gateway-id",
			filtersOf(func(in *ec2.DescribeCustomerGatewaysInput) []ec2types.Filter { return in.Filters }),
		},
		{
			"ec2.prefix_lists", "prefix-list-id",
			filtersOf(func(in *ec2.DescribeManagedPrefixListsInput) []ec2types.Filter { return in.Filters }),
		},
	}
	q := &config.ResourceQuery{
		IDs:     []string{"id-1"},
		Filters: []config.Filter{{Name: "tag:Env", Values: []string{"prod"}}},
	}
	for _, tt := range tests {
		t.Run(tt.collector, func(t *testing.T) {
			f := newFakes()
			if _, err := collectorNamed(t, tt.collector).collect(
				context.Background(), f.clients(), q, &collected{},
			); err != nil {
				t.Fatal(err)
			}
			want := []ec2types.Filter{
				{Name: awssdk.String(tt.idFilter), Values: []string{"id-1"}},
				{Name: awssdk.String("tag:Env"), Values: []string{"prod"}},
			}
			if got := tt.filters(&f.ec2.fakeCalls); !reflect.DeepEqual(got, want) {
				t.Errorf("filters = %+v, want %+v", got, want)
			}
		})
	}
}

// filtersOf returns the filters from the first In call a fake received, or
// nil when there was none.
func filtersOf[In any](get func(In) []ec2types.Filter) func(f *fakeCalls) []ec2types.Filter {
	return func(f *fakeCalls) []ec2types.Filter {
		ins := recorded[In](f)
		if len(ins) == 0 {
			return nil
		}
		return get(ins[0])
	}
}

func TestCollectEC2VPNConnectionsScrubsSecrets(t *testing.T) {
	region := &collected{}
	if _, err := collectEC2VPNConnections(
		context.Background(), newFakes().clients(), &config.ResourceQuery{}, region,
	); err != nil {
		t.Fatal(err)
	}
	for _, vpn := range region.VPNConnections {
		id := awssdk.ToString(vpn.VpnConnectionId)
		if vpn.CustomerGatewayConfiguration != nil {
			t.Errorf("%s kept CustomerGatewayConfiguration", id)
		}
		if vpn.Options == nil {
			continue
		}
		for i, tunnel := range vpn.Options.TunnelOptions {
			if tunnel.PreSharedKey != nil {
				t.Errorf("%s tunnel %d kept PreSharedKey", id, i)
			}
		}
	}
}
