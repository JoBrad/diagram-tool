package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	cc "github.com/DataDog/cloudcraft-go"

	"diagram/internal/render/cloudcraft"
	"diagram/internal/render/drawio"
	"diagram/internal/steampipe"
)

// testKey has the 44-character length the Cloudcraft SDK requires of a key.
var testKey = strings.Repeat("k", 44)

// writeConfig writes a config file into a temp directory and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeSteampipe serves canned rows by the table a query reads, and records
// whether it was closed.
type fakeSteampipe struct {
	rows     map[string][]steampipe.Row
	queryErr error
	closed   bool
}

var fromTable = regexp.MustCompile(`FROM (\w+)`)

func (f *fakeSteampipe) Query(_ context.Context, sql string, _ ...any) ([]steampipe.Row, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows[fromTable.FindStringSubmatch(sql)[1]], nil
}

func (f *fakeSteampipe) Close(context.Context) error {
	f.closed = true
	return nil
}

// useSteampipe makes read open db, or fail with openErr, for one test.
func useSteampipe(t *testing.T, db *fakeSteampipe, openErr error) {
	t.Helper()
	orig := openSteampipe
	t.Cleanup(func() { openSteampipe = orig })
	openSteampipe = func(context.Context) (steampipeDB, error) {
		if openErr != nil {
			return nil, openErr
		}
		return db, nil
	}
}

// row builds a steampipe.Row from column/value pairs.
func row(pairs ...string) steampipe.Row {
	r := steampipe.Row{}
	for i := 0; i < len(pairs); i += 2 {
		v := pairs[i+1]
		r[pairs[i]] = &v
	}
	return r
}

const steampipeConfig = `{"cloud": {"aws": {"source": "steampipe", "account_id": "111111111111",
	"regions": ["us-east-1"], "resources": {"ec2": {"instances": {}, "vpcs": {}, "subnets": {}}}}}%s}`

func TestRun(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, "")
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no command", nil, 2, "", "Usage: diagram"},
		{"help", []string{"help"}, 0, "Usage: diagram", ""},
		{"--help", []string{"--help"}, 0, "Usage: diagram", ""},
		{"unknown", []string{"draw"}, 2, "", `unknown command "draw"`},
		{"create fails", []string{"create"}, 1, "", "error: " + cc.EnvAPIKey + " is not set"},
		{"c alias", []string{"c"}, 1, "", "error: " + cc.EnvAPIKey + " is not set"},
		{"export fails", []string{"export"}, 1, "", "error: --export-path is required"},
		{"e alias", []string{"e"}, 1, "", "error: --export-path is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) || tt.wantStdout == "" && stdout.Len() > 0 {
				t.Errorf("stdout = %q, want it to hold %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) || tt.wantStderr == "" && stderr.Len() > 0 {
				t.Errorf("stderr = %q, want it to hold %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunCreateErrors(t *testing.T) {
	useSteampipe(t, nil, errors.New("no Steampipe database"))
	out := t.TempDir()
	unnamed := writeConfig(t, strings.Replace(steampipeConfig, "%s", "", 1))
	named := writeConfig(t, strings.Replace(steampipeConfig, "%s", `, "diagram": {"name": "Prod"}`, 1))
	// The unknown type fails validation before the database is opened.
	typo := writeConfig(t, `{"cloud": {"aws": {"regions": ["us-east-1"],
		"resources": {"ec2": {"instance": {}}}}}, "diagram": {"name": "Prod"}}`)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"draw.io without a path", []string{"--draw.io"}, "--draw.io: "},
		{"no config", []string{"--draw.io", "-p", out}, "--config is required"},
		{"missing config", []string{"--draw.io", "-p", out, "-c", filepath.Join(out, "nope.json")}, "nope.json"},
		{"no name", []string{"--draw.io", "-p", out, "-c", unnamed}, "no diagram name"},
		{
			"unknown resource type", []string{"--draw.io", "-p", out, "-c", typo},
			"configuration: cloud.aws.resources.ec2.instance: unknown resource type",
		},
		// Each of these has a name, so it gets as far as reading the account.
		{"name from config", []string{"--draw.io", "-p", out, "-c", named}, "no Steampipe database"},
		{"name flag", []string{"--draw.io", "-p", out, "-c", unnamed, "-n", "Prod"}, "no Steampipe database"},
		{
			"name from export file",
			[]string{"--draw.io", "-p", filepath.Join(out, "Prod.drawio"), "-c", unnamed},
			"no Steampipe database",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := runCreate(tt.args); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want one holding %q", err, tt.wantErr)
			}
		})
	}
}

// TestRunCreate draws a diagram from Steampipe rows, end to end: config,
// collection, model, and the draw.io file.
func TestRunCreate(t *testing.T) {
	db := &fakeSteampipe{rows: map[string][]steampipe.Row{
		"aws_vpc": {row("region", "us-east-1", "vpc_id", "vpc-1", "title", "main", "cidr_block", "10.0.0.0/16")},
		"aws_vpc_subnet": {row("region", "us-east-1", "subnet_id", "subnet-a", "title", "app", "vpc_id", "vpc-1",
			"cidr_block", "10.0.0.0/24", "availability_zone", "us-east-1a", "map_public_ip_on_launch", "false")},
		"aws_ec2_instance": {row("region", "us-east-1", "instance_id", "i-1", "title", "web",
			"instance_state", "running", "instance_type", "t3.micro", "subnet_id", "subnet-a", "vpc_id", "vpc-1")},
	}}
	useSteampipe(t, db, nil)
	out := t.TempDir()
	cfg := writeConfig(t, strings.Replace(steampipeConfig, "%s", `, "diagram": {"name": "Prod"}`, 1))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"create", "--draw.io", "-p", out, "-c", cfg}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr %q; want 0", code, stderr.String())
	}
	if !db.closed {
		t.Error("create left the Steampipe connection open")
	}
	got, err := os.ReadFile(filepath.Join(out, "Prod.drawio"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"vpc-1", "subnet-a", "i-1"} {
		if !strings.Contains(string(got), `id="`+id+`"`) {
			t.Errorf("draw.io file has no cell for %s", id)
		}
	}

	// The file now exists, so a second run succeeds only if --overwrite
	// reaches the output.
	if err := runCreate([]string{"--draw.io", "-p", out, "-c", cfg, "--overwrite"}); err != nil {
		t.Errorf("second create with --overwrite: %v", err)
	}
}

func TestRunCreateQueryError(t *testing.T) {
	boom := errors.New("boom")
	db := &fakeSteampipe{queryErr: boom}
	useSteampipe(t, db, nil)
	cfg := writeConfig(t, strings.Replace(steampipeConfig, "%s", `, "diagram": {"name": "Prod"}`, 1))
	if err := runCreate([]string{"--draw.io", "-p", t.TempDir(), "-c", cfg}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the query error", err)
	}
	if !db.closed {
		t.Error("a failed read left the Steampipe connection open")
	}
}

func TestBuildOutputs(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, testKey)
	dir := t.TempDir()

	outputs, err := buildOutputs(dir, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("no output chosen: got %d outputs, want Cloudcraft alone", len(outputs))
	}
	if _, ok := outputs[0].(*cloudcraft.Output); !ok {
		t.Errorf("no output chosen: got %T, want *cloudcraft.Output", outputs[0])
	}

	outputs, err = buildOutputs(dir, false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 {
		t.Fatalf("both chosen: got %d outputs, want 2", len(outputs))
	}
	_, isDrawio := outputs[0].(*drawio.Output)
	_, isCloudcraft := outputs[1].(*cloudcraft.Output)
	if !isDrawio || !isCloudcraft {
		t.Errorf("both chosen: got %T, %T; want draw.io, then Cloudcraft", outputs[0], outputs[1])
	}

	outputs, err = buildOutputs(dir, false, false, true)
	if err != nil || len(outputs) != 1 {
		t.Errorf("draw.io alone: got %d outputs, %v; want one", len(outputs), err)
	}
}

func TestRunExportCloudcraftErrors(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, "")
	out := t.TempDir()
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no path", []string{"--format", "json"}, "--export-path is required"},
		{"no format", []string{"-p", out}, "--format is required"},
		{"no key", []string{"-p", out, "--format", "json"}, cc.EnvAPIKey + " is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := runExportCloudcraft(tt.args); err == nil || err.Error() != tt.wantErr {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// fakeExporter records what it was built and called with, and reports one
// saved file and one failure.
type fakeExporter struct {
	dir       string
	overwrite bool
	name      string
	formats   []string
}

func (f *fakeExporter) ExportAll(_ context.Context, name string, formats []string, report func(string, error)) error {
	f.name, f.formats = name, formats
	report(filepath.Join(f.dir, "Prod.json"), nil)
	report("", errors.New("blueprint bp-2: no name"))
	return errors.New("1 of 2 exports failed")
}

func TestRunExportCloudcraft(t *testing.T) {
	exp := &fakeExporter{}
	orig := newExporter
	t.Cleanup(func() { newExporter = orig })
	newExporter = func(dir string, overwrite bool) (exporter, error) {
		exp.dir, exp.overwrite = dir, overwrite
		return exp, nil
	}
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	err := runExportCloudcraft([]string{"-p", "out", "-n", "Prod", "--format", "json,png", "--overwrite"})
	if err == nil || err.Error() != "1 of 2 exports failed" {
		t.Errorf("err = %v, want ExportAll's error", err)
	}
	if exp.dir != "out" || !exp.overwrite || exp.name != "Prod" || !slices.Equal(exp.formats, []string{"json", "png"}) {
		t.Errorf("exporter got %+v, want the flags passed through", exp)
	}
	for _, want := range []string{"Saved " + filepath.Join("out", "Prod.json"), "Failed: blueprint bp-2: no name"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q does not hold %q", logs.String(), want)
		}
	}
}

const azureConfig = `{"cloud": {"azure": {"subscription_id": "00000000-0000-0000-0000-000000000001",
	"regions": ["eastus"], "resources": {"compute": {"virtual_machines": {}}}}}, "diagram": {"name": "Az"}}`

// TestRunCreateAzure draws an Azure subscription from Steampipe rows, end to
// end, and refuses Cloudcraft output for it.
func TestRunCreateAzure(t *testing.T) {
	const rg = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG/providers/"
	const vnet = rg + "Microsoft.Network/virtualNetworks/VNet"
	nic := rg + "Microsoft.Network/networkInterfaces/nic"
	db := &fakeSteampipe{rows: map[string][]steampipe.Row{
		"azure_virtual_network": {
			row(
				"region",
				"eastus",
				"id",
				vnet,
				"name",
				"VNet",
				"address_prefixes",
				`["10.0.0.0/16"]`,
				"subnets",
				`[{"id": "`+vnet+`/subnets/App", "name": "App", "properties": {"addressPrefix": "10.0.1.0/24"}}]`,
			),
		},
		"azure_network_interface": {
			row(
				"region",
				"eastus",
				"id",
				nic,
				"name",
				"nic",
				"ip_configurations",
				`[{"properties": {"privateIPAddress": "10.0.1.4", "subnet": {"id": "`+vnet+`/subnets/App"}}}]`,
			),
		},
		"azure_compute_virtual_machine": {row("region", "eastus", "id", rg+"Microsoft.Compute/virtualMachines/VM",
			"name", "VM", "size", "Standard_B2s", "network_interfaces", `[{"id": "`+nic+`"}]`)},
	}}
	useSteampipe(t, db, nil)
	out := t.TempDir()
	cfg := writeConfig(t, azureConfig)

	if err := runCreate([]string{"--draw.io", "-p", out, "-c", cfg}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "Az.drawio"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="` + strings.ToLower(vnet) + `"`,
		`id="` + strings.ToLower(vnet) + `/subnets/app"`,
		`id="` + strings.ToLower(rg) + `microsoft.compute/virtualmachines/vm"`,
		"img/lib/azure2/compute/Virtual_Machine.svg",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("draw.io file does not hold %s", want)
		}
	}
	if strings.Contains(string(got), "networkinterfaces/nic") {
		t.Error("draw.io file draws the network interface")
	}

	t.Setenv(cc.EnvAPIKey, testKey)
	if err := runCreate(
		[]string{"-c", cfg},
	); err == nil ||
		!strings.Contains(err.Error(), "cloudcraft output draws AWS only") {
		t.Errorf("Cloudcraft for Azure: err = %v, want a refusal", err)
	}
}
