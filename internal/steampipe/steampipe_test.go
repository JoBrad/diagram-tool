package steampipe

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeSource returns rows and records the last query.
type fakeSource struct {
	rows []Row
	err  error
	sql  string
	args []any
}

func (f *fakeSource) Query(_ context.Context, sql string, args ...any) ([]Row, error) {
	f.sql, f.args = sql, args
	return f.rows, f.err
}

// row builds a Row from column/value pairs. A "NULL" value reads as SQL NULL.
func row(pairs ...string) Row {
	r := Row{}
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

var testScope = Scope{
	AccountColumn: "account_id", Account: "111111111111",
	RegionColumn: "region", Regions: []string{"us-east-1"},
	DeletedColumn: "_deleted_at",
}

func TestRead(t *testing.T) {
	src := &fakeSource{rows: []Row{row("region", "us-east-1", "vpc_id", "vpc-1")}}
	table := Table{Name: "aws_vpc", IDColumn: "vpc_id", Columns: []string{"vpc_id", "title"}}

	rows, err := Read(context.Background(), src, table, testScope, []string{"vpc-1"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("Read = %v, %v; want one row", rows, err)
	}
	want := "SELECT region::text AS region, vpc_id::text AS vpc_id, title::text AS title FROM aws_vpc " +
		"WHERE _deleted_at IS NULL AND account_id = $1 AND region = ANY($2) AND vpc_id = ANY($3) ORDER BY vpc_id"
	if src.sql != want {
		t.Errorf("sql =\n%s\nwant\n%s", src.sql, want)
	}
	wantArgs := []any{"111111111111", []string{"us-east-1"}, []string{"vpc-1"}}
	if !reflect.DeepEqual(src.args, wantArgs) {
		t.Errorf("args = %v, want %v", src.args, wantArgs)
	}

	// Without IDs or a deleted column, neither condition is written.
	live := testScope
	live.DeletedColumn = ""
	if _, err := Read(context.Background(), src, table, live, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src.sql, "_deleted_at") || strings.Contains(src.sql, "$3") || len(src.args) != 2 {
		t.Errorf("sql = %s, args = %v; want account and regions only", src.sql, src.args)
	}

	// A table with case-insensitive IDs compares them lowered.
	folded := Table{Name: "azure_virtual_network", IDColumn: "id", Columns: []string{"id"}, FoldIDCase: true}
	if _, err := Read(context.Background(), src, folded, testScope, []string{"/Subscriptions/A/VNet"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src.sql, "AND lower(id) = ANY($3) ORDER BY id") ||
		!reflect.DeepEqual(src.args[2], []string{"/subscriptions/a/vnet"}) {
		t.Errorf("sql = %s, args = %v; want lowered IDs compared to lower(id)", src.sql, src.args)
	}
}

func TestReadErrors(t *testing.T) {
	ok := Table{Name: "aws_vpc", IDColumn: "vpc_id", Columns: []string{"vpc_id"}}
	badTable := ok
	badTable.Name = "aws_vpc; drop table x"
	badColumn := ok
	badColumn.Columns = []string{"vpc_id", "title)"}
	badScope := testScope
	badScope.AccountColumn = "account id"

	tests := []struct {
		name  string
		src   *fakeSource
		table Table
		scope Scope
		want  string
	}{
		{"query fails", &fakeSource{err: errors.New("boom")}, ok, testScope, "read aws_vpc: boom"},
		{"unsafe table", &fakeSource{}, badTable, testScope, "unsafe table name"},
		{"unsafe column", &fakeSource{}, badColumn, testScope, `unsafe column name "title)"`},
		{"unsafe scope column", &fakeSource{}, ok, badScope, `unsafe column name "account id"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(context.Background(), tt.src, tt.table, tt.scope, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
			if tt.src.err == nil && tt.src.sql != "" {
				t.Errorf("unsafe query was sent: %s", tt.src.sql)
			}
		})
	}
}

func TestTextAndDecode(t *testing.T) {
	r := row("a", "x", "n", "NULL", "j", `{"k":1}`, "bad", "{")
	if Text(r, "a") != "x" || Text(r, "n") != "" || Text(r, "missing") != "" {
		t.Errorf("Text read %q, %q, %q", Text(r, "a"), Text(r, "n"), Text(r, "missing"))
	}
	var v map[string]int
	if err := Decode(r, "j", &v); err != nil || v["k"] != 1 {
		t.Errorf("Decode = %v, %v", v, err)
	}
	unchanged := map[string]int{"kept": 1}
	if err := Decode(r, "n", &unchanged); err != nil || unchanged["kept"] != 1 {
		t.Errorf("Decode(NULL) = %v, %v; want v unchanged", unchanged, err)
	}
	if err := Decode(r, "bad", &v); err == nil || !strings.Contains(err.Error(), "decode bad") {
		t.Errorf("Decode(bad) err = %v", err)
	}
}

func TestJSONIDs(t *testing.T) {
	sgKeys := []string{"GroupId", "VpcSecurityGroupId"}
	subnetKeys := []string{"SubnetId", "SubnetIdentifier"}
	tests := []struct {
		value string
		keys  []string
		want  []string
	}{
		{`["sg-1","sg-2"]`, sgKeys, []string{"sg-1", "sg-2"}},
		{`[{"GroupId":"sg-1"},{"VpcSecurityGroupId":"sg-2"},{"Other":"x"}]`, sgKeys, []string{"sg-1", "sg-2"}},
		{`[{"SubnetId":"subnet-1","ZoneName":"a"},{"SubnetIdentifier":"subnet-2"}]`, subnetKeys,
			[]string{"subnet-1", "subnet-2"}},
		{`[{"SubnetId":""}]`, subnetKeys, nil},
		{"NULL", sgKeys, nil},
	}
	for _, tt := range tests {
		got, err := JSONIDs(row("c", tt.value), "c", tt.keys)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("JSONIDs(%s) = %v, %v; want %v", tt.value, got, err, tt.want)
		}
	}
	for _, bad := range []string{`{`, `[1]`} {
		if _, err := JSONIDs(row("c", bad), "c", sgKeys); err == nil {
			t.Errorf("JSONIDs(%s) succeeded, want an error", bad)
		}
	}
}

func TestDiscover(t *testing.T) {
	src := &fakeSource{rows: []Row{
		row("table_name", "aws_lambda_function", "columns", "vpc_subnet_ids,security_groups"),
		row("table_name", "aws_vpc", "columns", ""),
		row("table_name", "aws_bad;name", "columns", ""),
		row("table_name", "azure_vnet", "columns", ""),
	}}
	d := Discovery{Prefix: "aws_", Required: []string{"arn"}, Optional: []string{"vpc_subnet_ids", "security_groups"}}
	got, err := Discover(context.Background(), src, d)
	if err != nil {
		t.Fatal(err)
	}
	want := []Discovered{
		{Table: "aws_lambda_function", Columns: map[string]bool{"vpc_subnet_ids": true, "security_groups": true}},
		{Table: "aws_vpc", Columns: map[string]bool{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %+v, want %+v", got, want)
	}
	wantArgs := []any{`aws\_%`, d.Optional, d.Required}
	if !reflect.DeepEqual(src.args, wantArgs) {
		t.Errorf("args = %v, want %v", src.args, wantArgs)
	}

	if _, err := Discover(context.Background(), &fakeSource{err: errors.New("boom")}, d); err == nil ||
		!strings.Contains(err.Error(), "discover Steampipe tables: boom") {
		t.Errorf("err = %v, want a discovery error", err)
	}
}
