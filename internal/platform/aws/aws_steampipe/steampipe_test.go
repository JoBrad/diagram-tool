package aws_steampipe

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
	"diagram/internal/steampipe"
)

// call is one query the fake source received.
type call struct {
	sql  string
	args []any
}

// fakeSource returns canned rows per table and records every query.
type fakeSource struct {
	rows  map[string][]steampipe.Row
	err   error
	calls []call
}

// fromTable names the table a query reads; discovery reads "information_schema".
var fromTable = regexp.MustCompile(`FROM (\w+)`)

func (f *fakeSource) Query(_ context.Context, sql string, args ...any) ([]steampipe.Row, error) {
	f.calls = append(f.calls, call{sql, args})
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[fromTable.FindStringSubmatch(sql)[1]], nil
}

// table returns the query sent for a table, or nil.
func (f *fakeSource) table(name string) *call {
	for i, c := range f.calls {
		if fromTable.FindStringSubmatch(c.sql)[1] == name {
			return &f.calls[i]
		}
	}
	return nil
}

// row builds a steampipe.Row from column/value pairs. A "NULL" value reads as SQL NULL.
func row(pairs ...string) steampipe.Row {
	r := steampipe.Row{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "NULL" {
			r[pairs[i]] = nil
			continue
		}
		v := pairs[i+1]
		r[pairs[i]] = &v
	}
	return r
}

// cfgWith returns an account with no account ID; withAccount sets one.
func cfgWith(regions []string, q ec2) *config.AWSAccount {
	return &config.AWSAccount{
		Source: config.SourceSteampipe, Regions: regions,
		Resources: config.Resources{Services: map[string]map[string]*config.ResourceQuery{"ec2": q}},
	}
}

// ec2 maps an EC2 type's key to its query.
type ec2 = map[string]*config.ResourceQuery

func withAccount(acct *config.AWSAccount, id string) *config.AWSAccount {
	acct.AccountID = id
	return acct
}

func testSource() *fakeSource {
	return &fakeSource{rows: map[string][]steampipe.Row{
		"aws_ec2_instance": {
			row("region", "us-east-1", "instance_id", "i-1", "title", "web", "instance_state", "running",
				"instance_type", "t3.micro", "platform_details", "Linux/UNIX", "private_ip_address", "10.0.0.5/32",
				"public_ip_address", "203.0.113.10/32", "subnet_id", "subnet-a", "vpc_id", "vpc-1",
				"security_groups", `[{"GroupId":"sg-1","GroupName":"web-sg"}]`),
			row("region", "us-east-1", "instance_id", "i-2", "instance_state", "terminated"),
			row("region", "eu-west-1", "instance_id", "i-3", "instance_state", "running"),
		},
		"aws_vpc": {
			row("region", "us-east-1", "vpc_id", "vpc-1", "title", "vpc-1", "cidr_block", "10.0.0.0/16"),
		},
		"aws_vpc_subnet": {
			row("region", "us-west-2", "subnet_id", "subnet-a", "title", "NULL", "vpc_id", "vpc-1",
				"cidr_block", "10.0.0.0/24", "availability_zone", "us-west-2a", "map_public_ip_on_launch", "true"),
		},
		"aws_vpc_security_group": {
			row("region", "us-east-1", "group_id", "sg-1", "group_name", "web-sg", "vpc_id", "vpc-1",
				"ip_permissions",
				`[{"IpProtocol":"tcp","FromPort":443,"ToPort":443,"IpRanges":[{"CidrIp":"0.0.0.0/0"}]}]`,
				"ip_permissions_egress",
				`[{"IpProtocol":"-1","UserIdGroupPairs":[{"GroupId":"sg-2","Description":"peer"}],`+
					`"PrefixListIds":[{"PrefixListId":"pl-1"}],"Ipv6Ranges":[{"CidrIpv6":"::/0"}]}]`),
		},
	}}
}

func TestCollect(t *testing.T) {
	src := testSource()
	cfg := cfgWith([]string{"us-east-1", "us-west-2"}, ec2{"instances": {}})
	regions, err := Collect(context.Background(), src, withAccount(cfg, "111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0].Name != "us-east-1" || regions[1].Name != "us-west-2" {
		t.Fatalf("regions = %+v, want us-east-1 and us-west-2 in config order", regions)
	}

	var ids []string
	for _, rec := range regions[0].Records {
		ids = append(ids, rec.ID)
	}
	// i-2 is terminated, and i-3 is in a region the config does not name.
	if want := []string{"i-1", "vpc-1", "sg-1"}; !slices.Equal(ids, want) {
		t.Errorf("us-east-1 records = %v, want %v", ids, want)
	}

	inst := regions[0].Records[0]
	want := inventory.Record{
		ID:       "i-1",
		Type:     aws.TypeInstance,
		Platform: aws.Platform,
		Name:     "web",
		Tier:     inventory.TierPublic,
		Refs: []inventory.Ref{
			{Relation: inventory.RelContainedIn, Type: aws.TypeSubnet, ID: "subnet-a"},
			{Relation: inventory.RelContainedIn, Type: aws.TypeVPC, ID: "vpc-1"},
			{Relation: inventory.RelMemberOf, Type: aws.TypeSecurityGroup, ID: "sg-1"},
		},
		Attrs: map[string]string{
			"instance_type":    "t3.micro",
			"platform_details": "Linux/UNIX",
			"private_ip":       "10.0.0.5",
			"public_ip":        "203.0.113.10",
		},
	}
	if !reflect.DeepEqual(inst, want) {
		t.Errorf("instance = %+v, want %+v", inst, want)
	}

	subnet := regions[1].Records[0]
	if subnet.Name != "subnet-a" || subnet.Attrs["public"] != "true" {
		t.Errorf("subnet = %+v, want the ID as name (NULL title) and public", subnet)
	}

	sg := regions[0].Records[2]
	var targets []string
	for _, r := range sg.Rules {
		targets = append(targets, r.TargetType+":"+r.Target)
		if r.TargetType != "cidr" && !r.Egress {
			t.Errorf("rule %+v should be egress", r)
		}
	}
	if want := []string{"cidr:0.0.0.0/0", "cidr6:::/0", "security_group:sg-2", "prefix_list:pl-1"}; !slices.Equal(
		targets, want) {
		t.Errorf("sg rules = %v, want %v", targets, want)
	}
}

func TestCollectQueries(t *testing.T) {
	src := testSource()
	cfg := cfgWith([]string{"us-east-1"}, ec2{
		"instances": {IDs: []string{"i-1"}},
	})
	if _, err := Collect(context.Background(), src, withAccount(cfg, "111111111111")); err != nil {
		t.Fatal(err)
	}

	inst := src.table("aws_ec2_instance")
	if inst == nil {
		t.Fatal("no aws_ec2_instance query")
	}
	wantArgs := []any{"111111111111", []string{"us-east-1"}, []string{"i-1"}}
	if !reflect.DeepEqual(inst.args, wantArgs) {
		t.Errorf("args = %v, want %v", inst.args, wantArgs)
	}
	if !strings.Contains(inst.sql, "instance_id = ANY($3)") || !strings.Contains(inst.sql, "_deleted_at IS NULL") {
		t.Errorf("sql = %s, want an id match and the soft-delete filter", inst.sql)
	}
	if vpc := src.table("aws_vpc"); vpc == nil || len(vpc.args) != 2 {
		t.Errorf("aws_vpc query = %+v, want account and regions only", vpc)
	}

	for _, c := range src.calls {
		if strings.Contains(c.sql, "*") || strings.Contains(c.sql, "user_data") {
			t.Errorf("query selects * or user_data: %s", c.sql)
		}
	}
}

// TestCollectSkipsPrimary checks that instances are read only when named,
// while containers are always read.
func TestCollectSkipsPrimary(t *testing.T) {
	src := testSource()
	if _, err := Collect(
		context.Background(),
		src,
		withAccount(cfgWith([]string{"us-east-1"}, nil), "111111111111"),
	); err != nil {
		t.Fatal(err)
	}
	if src.table("aws_ec2_instance") != nil {
		t.Error("instances were read without being named")
	}
	if src.table("aws_vpc") == nil || src.table("aws_vpc_subnet") == nil || src.table("aws_vpc_security_group") == nil {
		t.Error("a container table was not read")
	}
}

func TestCollectErrors(t *testing.T) {
	ctx := context.Background()
	regions := []string{"us-east-1"}
	tests := []struct {
		name    string
		src     *fakeSource
		cfg     *config.AWSAccount
		account string
		want    string
	}{
		{"no account", testSource(), cfgWith(regions, nil), "", "account ID"},
		{
			"filters", testSource(),
			cfgWith(regions, ec2{"vpcs": {
				Filters: []config.Filter{{Name: "tag:env", Values: []string{"prod"}}},
			}}),
			"111111111111", "resources.ec2.vpcs: filters",
		},
		{"query fails", &fakeSource{err: errors.New("boom")}, cfgWith(regions, nil), "111111111111", "boom"},
		{
			"bad json",
			&fakeSource{rows: map[string][]steampipe.Row{"aws_vpc_security_group": {
				row("region", "us-east-1", "group_id", "sg-1", "ip_permissions", "{not json"),
			}}},
			cfgWith(regions, nil), "111111111111", "security group sg-1: decode ip_permissions",
		},
		{
			"bad egress json",
			&fakeSource{rows: map[string][]steampipe.Row{"aws_vpc_security_group": {
				row("region", "us-east-1", "group_id", "sg-1", "ip_permissions_egress", "{not json"),
			}}},
			cfgWith(regions, nil), "111111111111", "decode ip_permissions_egress",
		},
		{
			"bad instance json",
			&fakeSource{rows: map[string][]steampipe.Row{"aws_ec2_instance": {
				row("region", "us-east-1", "instance_id", "i-1", "security_groups", "[1]"),
			}}},
			cfgWith(regions, ec2{"instances": {}}),
			"111111111111", "instance i-1: decode security_groups",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Collect(ctx, tt.src, withAccount(tt.cfg, tt.account))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestMissing(t *testing.T) {
	q := &config.ResourceQuery{}
	r := &config.Resources{Services: map[string]map[string]*config.ResourceQuery{
		"ec2":            {"nat_gateways": q, "prefix_lists": q, "vpcs": q},
		"kms":            {"keys": q},
		"secretsmanager": {"secrets": q},
		"ssm":            {"parameters": q},
	}}
	want := []string{"ec2.nat_gateways", "ec2.prefix_lists", "kms.keys", "secretsmanager.secrets", "ssm.parameters"}
	if got := missing(r); !slices.Equal(got, want) {
		t.Errorf("missing = %v, want %v", got, want)
	}
	if got := missing(&config.Resources{}); got != nil {
		t.Errorf("missing(empty) = %v, want none", got)
	}
}

// Every table reads a registered type, so the registry decides when it is
// read and what the logs call it.
func TestTablesRegistered(t *testing.T) {
	for _, tb := range tables {
		if _, ok := aws.Lookup(tb.typ); !ok {
			t.Errorf("table %s reads %q, which is not in aws.Registry", tb.Name, tb.typ)
		}
	}
}
