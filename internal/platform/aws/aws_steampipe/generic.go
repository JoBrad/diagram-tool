package aws_steampipe

import (
	"context"
	"fmt"
	"log"
	"slices"

	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
	"diagram/internal/steampipe"
)

// sgColumns are the jsonb columns that list a resource's security groups.
// Steampipe names them differently per table, and stores either group IDs
// or objects holding one.
var sgColumns = []string{"security_groups", "vpc_security_group_ids", "vpc_security_groups"}

// subnetColumns are the jsonb columns that list the subnets of a resource
// without a single subnet_id: Lambda's vpc_subnet_ids, RDS's subnets, a load
// balancer's availability_zones, and a network ACL's associations.
var subnetColumns = []string{"vpc_subnet_ids", "subnets", "availability_zones", "associations"}

// Object keys that hold a security group or subnet ID in those columns.
var (
	sgKeys     = []string{"GroupId", "VpcSecurityGroupId"}
	subnetKeys = []string{"SubnetId", "SubnetIdentifier"}
)

// discovery finds the AWS tables include_all can read: every table with an
// ARN to use as the ID, a title, network placement, and the scoping
// columns. Tables without a VPC are left out because they would have nowhere
// to be drawn.
var discovery = steampipe.Discovery{
	Prefix:   "aws_",
	Required: []string{"arn", "title", "vpc_id", "account_id", "region", "_deleted_at"},
	Optional: slices.Concat([]string{"subnet_id"}, sgColumns, subnetColumns),
}

// resourceTypes maps a discovered table to the resource type it holds, as the
// AWS CLI and CloudFormation name it. A table with no entry keeps its table
// name as its type, and the run logs it.
var resourceTypes = map[string]string{
	"aws_ebs_volume":                    "AWS::EC2::Volume",
	"aws_ec2_application_load_balancer": "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"aws_ec2_network_interface":         "AWS::EC2::NetworkInterface",
	"aws_ecs_service":                   "AWS::ECS::Service",
	"aws_lambda_function":               "AWS::Lambda::Function",
	"aws_rds_db_cluster":                "AWS::RDS::DBCluster",
	"aws_rds_db_instance":               "AWS::RDS::DBInstance",
	"aws_s3_bucket":                     "AWS::S3::Bucket",
	"aws_transfer_server":               "AWS::Transfer::Server",
}

// skipTables are discovered tables include_all leaves out, with the reason.
var skipTables = map[string]string{
	// A network ACL applies to subnets rather than living in one, and drawing
	// it would keep otherwise empty subnets.
	"aws_vpc_network_acl": "network ACLs are not drawn",
}

// genericTables returns a table for every discovered AWS table that has no
// hand-written mapping. Each reads only its ID, name, placement, and security
// group columns, never the rest of the row.
func genericTables(ctx context.Context, src steampipe.Source) ([]table, error) {
	found, err := steampipe.Discover(ctx, src, discovery)
	if err != nil {
		return nil, err
	}
	mapped := map[string]bool{}
	for _, t := range tables {
		mapped[t.Name] = true
	}

	var out []table
	for _, d := range found {
		if mapped[d.Table] {
			continue
		}
		if reason, ok := skipTables[d.Table]; ok {
			log.Printf("Skipping %s: %s", d.Table, reason)
			continue
		}
		has := func(c string) bool { return d.Columns[c] }
		sgs := slices.DeleteFunc(slices.Clone(sgColumns), func(c string) bool { return !has(c) })
		subnets := slices.DeleteFunc(slices.Clone(subnetColumns), func(c string) bool { return !has(c) })
		columns := []string{"arn", "title", "vpc_id"}
		if has("subnet_id") {
			columns = append(columns, "subnet_id")
		}
		rt, ok := resourceTypes[d.Table]
		if !ok {
			log.Printf("WARN %s: no resource type mapped, using the table name", d.Table)
			rt = d.Table
		}
		out = append(out, table{
			Table: steampipe.Table{
				Name: d.Table, IDColumn: "arn", Columns: slices.Concat(columns, sgs, subnets),
			},
			resource: d.Table,
			record:   genericRecord(rt, sgs, subnets),
		})
	}
	return out, nil
}

// genericRecord returns a record reader for a discovered table. A row's
// subnet_id wins; without one, the first subnet column that lists any
// subnets gives them all, and aws.Finish keeps one.
func genericRecord(rt string, sgColumns, subnetColumns []string) func(steampipe.Row) (inventory.Record, bool, error) {
	return func(row steampipe.Row) (inventory.Record, bool, error) {
		id := steampipe.Text(row, "arn")
		name := steampipe.Text(row, "title")
		if name == "" {
			name = id
		}
		var subnets []string
		if s := steampipe.Text(row, "subnet_id"); s != "" {
			subnets = []string{s}
		}
		for _, col := range subnetColumns {
			if len(subnets) > 0 {
				break
			}
			ids, err := steampipe.JSONIDs(row, col, subnetKeys)
			if err != nil {
				return inventory.Record{}, false, fmt.Errorf("%s %s: %w", rt, id, err)
			}
			subnets = ids
		}
		var groups []string
		for _, col := range sgColumns {
			ids, err := steampipe.JSONIDs(row, col, sgKeys)
			if err != nil {
				return inventory.Record{}, false, fmt.Errorf("%s %s: %w", rt, id, err)
			}
			groups = append(groups, ids...)
		}
		return aws.Generic(rt, id, name, steampipe.Text(row, "vpc_id"), subnets, groups), true, nil
	}
}
