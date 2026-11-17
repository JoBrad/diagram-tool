package aws_steampipe

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
	"diagram/internal/steampipe"
)

// includeAllSource is testSource plus a discovery result and rows for the
// discovered tables.
func includeAllSource() *fakeSource {
	src := testSource()
	src.rows["information_schema"] = []steampipe.Row{
		row("table_name", "aws_ec2_instance", "columns", "subnet_id,security_groups"),
		row("table_name", "aws_lambda_function", "columns", "vpc_subnet_ids,vpc_security_group_ids"),
		row("table_name", "aws_rds_db_instance", "columns", "subnets,vpc_security_groups"),
		row("table_name", "aws_example_eni", "columns", "associations,subnet_id"),
		row("table_name", "aws_bad;name", "columns", ""),
		row("table_name", "aws_vpc_network_acl", "columns", "associations"),
	}
	src.rows["aws_lambda_function"] = []steampipe.Row{
		row("region", "us-east-1", "arn", "arn:fn-1", "title", "worker", "vpc_id", "vpc-1",
			"vpc_security_group_ids", `["sg-1"]`, "vpc_subnet_ids", `["subnet-b","subnet-a"]`),
		row("region", "us-east-1", "arn", "arn:fn-2", "title", "NULL", "vpc_id", "NULL",
			"vpc_security_group_ids", "NULL"),
	}
	src.rows["aws_rds_db_instance"] = []steampipe.Row{
		row("region", "us-east-1", "arn", "arn:db-1", "title", "db", "vpc_id", "vpc-1",
			"vpc_security_groups", `[{"VpcSecurityGroupId":"sg-1","Status":"active"}]`,
			"subnets", `[{"SubnetIdentifier":"subnet-c","SubnetStatus":"Active"}]`),
	}
	src.rows["aws_example_eni"] = []steampipe.Row{
		row("region", "us-east-1", "arn", "arn:eni-1", "title", "eni", "vpc_id", "vpc-1", "subnet_id", "subnet-a",
			"associations", `[{"SubnetId":"subnet-z"}]`),
	}
	return src
}

func TestCollectIncludeAll(t *testing.T) {
	src := includeAllSource()
	acct := withAccount(cfgWith([]string{"us-east-1"}, nil), "111111111111")
	acct.Resources.IncludeAll = true
	regions, err := Collect(context.Background(), src, acct)
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]inventory.Record{}
	for _, rec := range regions[0].Records {
		byID[rec.ID] = rec
	}
	// Instances are read without being named, as include_all reads every
	// mapped table too.
	if _, ok := byID["i-1"]; !ok {
		t.Error("include_all did not read instances")
	}

	gen := aws.Generic
	want := map[string]inventory.Record{
		// Every listed subnet is kept, in table order; aws.Finish picks one.
		"arn:fn-1": gen("AWS::Lambda::Function", "arn:fn-1", "worker", "vpc-1",
			[]string{"subnet-b", "subnet-a"}, []string{"sg-1"}),
		"arn:fn-2": gen("AWS::Lambda::Function", "arn:fn-2", "arn:fn-2", "", nil, nil),
		"arn:db-1": gen("AWS::RDS::DBInstance", "arn:db-1", "db", "vpc-1", []string{"subnet-c"}, []string{"sg-1"}),
		// subnet_id wins over the associations list.
		"arn:eni-1": gen("aws_example_eni", "arn:eni-1", "eni", "vpc-1", []string{"subnet-a"}, nil),
	}
	for id, w := range want {
		if got := byID[id]; !reflect.DeepEqual(got, w) {
			t.Errorf("%s = %+v, want %+v", id, got, w)
		}
	}

	// The mapped instance table is not read twice, and the bad name is never
	// written into SQL.
	var reads []string
	for _, c := range src.calls {
		reads = append(reads, fromTable.FindStringSubmatch(c.sql)[1])
		if strings.Contains(c.sql, "aws_bad") || strings.Contains(c.sql, "*") || strings.Contains(c.sql, "user_data") {
			t.Errorf("unsafe query: %s", c.sql)
		}
	}
	if n := strings.Count(strings.Join(reads, " "), "aws_ec2_instance"); n != 1 {
		t.Errorf("aws_ec2_instance read %d times, want 1", n)
	}
	if slices.Contains(reads, "aws_vpc_network_acl") {
		t.Error("network ACLs were read; include_all skips them")
	}
	eni := src.table("aws_example_eni")
	if eni == nil || !strings.Contains(eni.sql, "subnet_id::text") || !strings.Contains(eni.sql, "arn::text") {
		t.Errorf("eni query = %+v, want arn and subnet_id", eni)
	}
}

func TestCollectIncludeAllErrors(t *testing.T) {
	acct := func() *config.AWSAccount {
		a := withAccount(cfgWith([]string{"us-east-1"}, nil), "111111111111")
		a.Resources.IncludeAll = true
		return a
	}
	src := includeAllSource()
	src.rows["aws_rds_db_instance"] = []steampipe.Row{
		row("region", "us-east-1", "arn", "arn:db-1", "vpc_security_groups", `[1]`),
	}
	if _, err := Collect(context.Background(), src, acct()); err == nil ||
		!strings.Contains(err.Error(), "AWS::RDS::DBInstance arn:db-1: decode vpc_security_groups") {
		t.Errorf("err = %v, want a decode error naming the resource", err)
	}

	failing := &failOn{fakeSource: includeAllSource(), table: "information_schema"}
	if _, err := Collect(context.Background(), failing, acct()); err == nil ||
		!strings.Contains(err.Error(), "discover Steampipe tables") {
		t.Errorf("err = %v, want a discovery error", err)
	}

	failing = &failOn{fakeSource: includeAllSource(), table: "aws_lambda_function"}
	if _, err := Collect(context.Background(), failing, acct()); err == nil ||
		!strings.Contains(err.Error(), "read aws_lambda_function") {
		t.Errorf("err = %v, want a read error", err)
	}
}

// failOn fails queries against one table.
type failOn struct {
	*fakeSource
	table string
}

func (f *failOn) Query(ctx context.Context, sql string, args ...any) ([]steampipe.Row, error) {
	if fromTable.FindStringSubmatch(sql)[1] == f.table {
		return nil, context.Canceled
	}
	return f.fakeSource.Query(ctx, sql, args...)
}

func TestGenericRecordBadSubnets(t *testing.T) {
	read := genericRecord("aws_example", nil, []string{"subnets"})
	if _, _, err := read(row("arn", "arn:x-1", "subnets", "[1]")); err == nil ||
		!strings.Contains(err.Error(), "aws_example arn:x-1: decode subnets") {
		t.Errorf("err = %v, want a decode error naming the resource", err)
	}
}
