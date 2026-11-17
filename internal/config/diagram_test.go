package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write saves body as a config file and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRead(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		key        string
		source     Source
		account    string
		regions    int
		hasEC2Inst bool
	}{
		{
			name:   "sdk default",
			body:   `{"cloud": {"aws": {"sso_profile_name": "dev", "regions": ["us-east-1"]}}}`,
			key:    "aws",
			source: SourceSDK, regions: 1,
		},
		{
			name: "steampipe",
			body: `{"cloud": {"aws": {"source": "steampipe", "account_id": "111111111111",
				"regions": ["us-east-1", "us-west-2"], "resources": {"include_all": true, "ec2": {"instances": {}}}}},
				"diagram": {"name": "d"}}`,
			key:    "aws",
			source: SourceSteampipe, account: "111111111111", regions: 2, hasEC2Inst: true,
		},
		{
			name:   "govcloud",
			body:   `{"cloud": {"aws_gov": {"regions": ["us-gov-west-1"]}}}`,
			key:    "aws_gov",
			source: SourceSDK, regions: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := readNoApp(write(t, tt.body))
			if err != nil {
				t.Fatal(err)
			}
			key, acct := cfg.Account()
			if key != tt.key || acct.Source != tt.source || acct.AccountID != tt.account ||
				len(acct.Regions) != tt.regions {
				t.Errorf("Account() = %s, %+v", key, acct)
			}
			if got := acct.Resources.EC2 != nil && acct.Resources.EC2.Instances != nil; got != tt.hasEC2Inst {
				t.Errorf("instances query present = %v, want %v", got, tt.hasEC2Inst)
			}
		})
	}
}

func TestReadErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"old flat shape", `{"regions": ["us-east-1"]}`, `unknown field "regions"`},
		{"no account", `{"cloud": {}}`, "cloud: no account configured"},
		{
			"both partitions",
			`{"cloud": {"aws": {"regions": ["us-east-1"]}, "aws_gov": {"regions": ["us-gov-west-1"]}}}`,
			"cloud: set only one of aws, aws_gov;",
		},
		{
			"bad source", `{"cloud": {"aws": {"source": "cmdb", "regions": ["us-east-1"]}}}`,
			`cloud.aws.source: must be "sdk" or "steampipe", not "cmdb"`,
		},
		{
			"steampipe without account", `{"cloud": {"aws": {"source": "steampipe", "regions": ["us-east-1"]}}}`,
			`cloud.aws.account_id: required when source is "steampipe"`,
		},
		{
			"bad account id", `{"cloud": {"aws_gov": {"account_id": "1234", "regions": ["us-gov-west-1"]}}}`,
			`cloud.aws_gov.account_id: "1234" is not a 12-digit AWS account ID`,
		},
		{"no regions", `{"cloud": {"aws": {"regions": []}}}`, "cloud.aws.regions: empty"},
		{
			"include_all with sdk",
			`{"cloud": {"aws": {"regions": ["us-east-1"], "resources": {"include_all": true}}}}`,
			`cloud.aws.resources.include_all: only supported when source is "steampipe"`,
		},
		{
			"ids with show",
			`{"cloud": {"aws": {"regions": ["us-east-1"], "resources": {"ec2": {"vpcs":
				{"ids": ["vpc-1"], "show_unconnected_resources": false}}}}}}`,
			"cloud.aws.resources.ec2.vpcs: ids and show_unconnected_resources cannot be combined",
		},
		{
			"kms filters",
			`{"cloud": {"aws": {"regions": ["us-east-1"], "resources": {"kms": {"keys":
				{"filters": [{"name": "x", "values": ["y"]}]}}}}}}`,
			"cloud.aws.resources.kms.keys: filters are not supported",
		},
		{
			"bad projection", `{"cloud": {"aws": {"regions": ["us-east-1"]}}, "diagram": {"projection": "3d"}}`,
			`projection must be "2d" or "isometric"`,
		},
		{
			"bad theme", `{"cloud": {"aws": {"regions": ["us-east-1"]}}, "diagram": {"theme": "blue"}}`,
			`theme must be "light" or "dark"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readNoApp(write(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := readNoApp(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("Read of a missing file succeeded")
	}
}

func TestReadHide(t *testing.T) {
	cfg, err := readNoApp(write(t, `{"cloud": {"aws": {"regions": ["us-east-1"], "resources": {"ec2": {
		"subnets": {"hide": true}, "vpcs": {"hide_children": true}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, acct := cfg.Account()
	if !acct.Resources.EC2.Subnets.Hide || !acct.Resources.EC2.VPCs.HideChildren {
		t.Errorf("ec2 = %+v %+v, want subnets hidden and VPC children hidden",
			acct.Resources.EC2.Subnets, acct.Resources.EC2.VPCs)
	}
}

func TestReadColors(t *testing.T) {
	cfg, err := readNoApp(write(t, `{"cloud": {"aws": {"regions": ["us-east-1"]}}, "options": {"colors": {
		"public": ["#7AA116"], "isolated": ["#8C4FFF", "#c925d1"], "resources": {"subnet-1": "#E7157B"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c := cfg.Options.Colors; len(c.Isolated) != 2 || c.Resources["subnet-1"] != "#E7157B" {
		t.Errorf("colors = %+v", c)
	}
	for body, want := range map[string]string{
		`{"public": ["green"]}`:               `options.colors.public: "green" is not a #RRGGBB color`,
		`{"resources": {"sg-1": "#12345"}}`:   `options.colors.resources.sg-1: "#12345" is not a #RRGGBB color`,
		`{"private": ["#00A4A6", "#GGGGGG"]}`: `options.colors.private: "#GGGGGG"`,
	} {
		_, err := readNoApp(write(t, `{"cloud": {"aws": {"regions": ["us-east-1"]}}, "options": {"colors": `+body+`}}`))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("colors %s: err = %v, want %q", body, err, want)
		}
	}
}

// readNoApp reads a diagram config with no app-level config, so the user's
// own ~/.config/diagramming/config.json cannot change a test's result.
func readNoApp(path string) (*Config, error) {
	return read(path, "")
}

func TestEC2Query(t *testing.T) {
	vpcs := EC2Query(func(e *EC2Resources) *ResourceQuery { return e.VPCs })
	if q := vpcs(&Resources{}); q != nil {
		t.Errorf("no ec2 section: %+v, want nil", q)
	}
	want := &ResourceQuery{IDs: []string{"vpc-1"}}
	if q := vpcs(&Resources{EC2: &EC2Resources{VPCs: want}}); q != want {
		t.Errorf("query = %+v, want %+v", q, want)
	}
}
