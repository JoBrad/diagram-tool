// Package aws_sdk collects the resources a diagram config names, live from
// the AWS APIs through the SDK, into inventory regions.
//
// Every collector in every region runs concurrently. Each collector writes
// only its own field of the region's collected resources, which is what keeps
// that safe without locks. Once all have run, package aws's record builders
// turn each region's resources into inventory records.
//
// To add a resource type:
//  1. Add its query field to config.Resources (or a service struct under it).
//  2. Add its resource type, a record builder, and its selection rules to
//     package aws (aws.go, records.go, and finish.go).
//  3. Add a field to collected (records.go) to hold the results, and build
//     them into records in collected.records.
//  4. Write a collect function with the collectFunc signature that assigns
//     only that field. Add any SDK method it calls to the matching API
//     interface in clients, and to the fakes in fake_test.go.
//  5. Register it in collectors.
package aws_sdk

import (
	"context"
	"fmt"
	"log"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"golang.org/x/sync/errgroup"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

// maxConcurrentCalls caps in-flight collectors. EC2 throttles describe calls
// per account; the SDK retries throttled calls, but the cap keeps retries
// rare.
const maxConcurrentCalls = 8

// collectFunc fetches the resources matching q, assigns them to its own field
// on region, and returns how many it found.
type collectFunc func(
	ctx context.Context, api *clients, q *config.ResourceQuery, region *collected,
) (int, error)

// clients holds one region's SDK clients, as the parts of each client the
// collectors call. The SDK clients implement them; tests swap in fakes so
// they exercise the collectors without the SDK's transport or the network.
// The paginators take these same interfaces.
type clients struct {
	ec2            ec2API
	kms            kmsAPI
	secretsManager secretsmanager.ListSecretsAPIClient
	ssm            ssm.DescribeParametersAPIClient
}

type ec2API interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeVpcsAPIClient
	ec2.DescribeSubnetsAPIClient
	ec2.DescribeSecurityGroupsAPIClient
	ec2.DescribeInternetGatewaysAPIClient
	ec2.DescribeNatGatewaysAPIClient
	ec2.DescribeTransitGatewaysAPIClient
	ec2.DescribeManagedPrefixListsAPIClient
	DescribeVpnConnections(
		ctx context.Context, in *ec2.DescribeVpnConnectionsInput, optFns ...func(*ec2.Options),
	) (*ec2.DescribeVpnConnectionsOutput, error)
	DescribeVpnGateways(
		ctx context.Context, in *ec2.DescribeVpnGatewaysInput, optFns ...func(*ec2.Options),
	) (*ec2.DescribeVpnGatewaysOutput, error)
	DescribeCustomerGateways(
		ctx context.Context, in *ec2.DescribeCustomerGatewaysInput, optFns ...func(*ec2.Options),
	) (*ec2.DescribeCustomerGatewaysOutput, error)
}

type kmsAPI interface {
	kms.ListAliasesAPIClient
	kms.ListKeysAPIClient
}

// newClients builds the SDK clients for the region awsCfg names.
func newClients(awsCfg awssdk.Config) *clients {
	return &clients{
		ec2:            ec2.NewFromConfig(awsCfg),
		kms:            kms.NewFromConfig(awsCfg),
		secretsManager: secretsmanager.NewFromConfig(awsCfg),
		ssm:            ssm.NewFromConfig(awsCfg),
	}
}

// collector ties one resource type's config query to its collect function.
type collector struct {
	// name is the resource's path in the config file, used in logs and errors.
	name string
	// query returns the config's query for this type, or nil when the config
	// does not name it.
	query func(r *config.Resources) *config.ResourceQuery
	// always collects the type even when the config does not name it. Set for
	// container types, so the diagram shows every VPC, subnet, and so on.
	always  bool
	collect collectFunc
}

// collectors lists every supported resource type.
var collectors = []collector{
	{
		name:    "ec2.instances",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.Instances }),
		collect: collectEC2Instances,
	},
	{
		name:    "ec2.vpn_connections",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.VPNConnections }),
		collect: collectEC2VPNConnections,
	},
	{
		name:    "ec2.vpcs",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.VPCs }),
		always:  true,
		collect: collectEC2VPCs,
	},
	{
		name:    "ec2.subnets",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.Subnets }),
		always:  true,
		collect: collectEC2Subnets,
	},
	{
		name:    "ec2.security_groups",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.SecurityGroups }),
		always:  true,
		collect: collectEC2SecurityGroups,
	},
	{
		name:    "ec2.nat_gateways",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.NATGateways }),
		always:  true,
		collect: collectEC2NATGateways,
	},
	{
		name:    "ec2.internet_gateways",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.InternetGateways }),
		always:  true,
		collect: collectEC2InternetGateways,
	},
	{
		name:    "ec2.vpn_gateways",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.VPNGateways }),
		always:  true,
		collect: collectEC2VPNGateways,
	},
	{
		name:    "ec2.transit_gateways",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.TransitGateways }),
		always:  true,
		collect: collectEC2TransitGateways,
	},
	{
		name:    "ec2.customer_gateways",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.CustomerGateways }),
		always:  true,
		collect: collectEC2CustomerGateways,
	},
	{
		name:    "ec2.prefix_lists",
		query:   config.EC2Query(func(r *config.EC2Resources) *config.ResourceQuery { return r.PrefixLists }),
		always:  true,
		collect: collectEC2PrefixLists,
	},
	{name: "kms.keys", query: func(r *config.Resources) *config.ResourceQuery {
		if r.KMS == nil {
			return nil
		}
		return r.KMS.Keys
	}, collect: collectKMSKeys},
	{name: "secretsmanager.secrets", query: func(r *config.Resources) *config.ResourceQuery {
		if r.SecretsManager == nil {
			return nil
		}
		return r.SecretsManager.Secrets
	}, collect: collectSecrets},
	{name: "ssm.parameters", query: func(r *config.Resources) *config.ResourceQuery {
		if r.SSM == nil {
			return nil
		}
		return r.SSM.Parameters
	}, collect: collectSSMParameters},
}

// Collect fetches every configured resource type from every configured
// region of the account. The first failure cancels the remaining calls.
func Collect(ctx context.Context, acct *config.AWSAccount) ([]*inventory.Region, error) {
	baseCfg, err := loadAWSConfig(ctx, acct.SSOProfileName)
	if err != nil {
		return nil, err
	}
	return collectAll(ctx, acct, baseCfg, newClients)
}

// collectAll is Collect with the AWS config already loaded and the client
// constructor passed in, so tests can supply fakes.
func collectAll(
	ctx context.Context,
	acct *config.AWSAccount,
	baseCfg awssdk.Config,
	newClients func(awssdk.Config) *clients,
) ([]*inventory.Region, error) {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentCalls)

	all := make([]*collected, len(acct.Regions))
	for i, name := range acct.Regions {
		region := &collected{}
		all[i] = region
		awsCfg := baseCfg.Copy()
		awsCfg.Region = name
		api := newClients(awsCfg)

		for _, c := range collectors {
			q := c.query(&acct.Resources)
			if q == nil {
				if !c.always {
					continue
				}
				q = &config.ResourceQuery{}
			}
			g.Go(func() error {
				n, err := c.collect(ctx, api, q, region)
				if err != nil {
					return fmt.Errorf("collect %s in %s: %w", c.name, name, err)
				}
				log.Printf("Found %d %s in %s", n, c.name, name)
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	regions := make([]*inventory.Region, len(acct.Regions))
	for i, name := range acct.Regions {
		regions[i] = &inventory.Region{Name: name, Records: all[i].records(name)}
	}
	return regions, nil
}

// loadAWSConfig loads AWS credentials for the profile. An empty profile falls
// back to the SDK's default credential chain.
func loadAWSConfig(ctx context.Context, profile string) (awssdk.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awssdk.Config{}, fmt.Errorf("load AWS config for profile %q: %w", profile, err)
	}
	return awsCfg, nil
}
