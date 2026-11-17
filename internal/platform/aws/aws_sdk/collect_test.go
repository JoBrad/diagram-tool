package aws_sdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"diagram/internal/config"
)

var errFake = errors.New("fake failure")

// allResources names every resource type that is collected only when named.
func allResources() config.Resources {
	return config.Resources{
		EC2: &config.EC2Resources{Instances: &config.ResourceQuery{}, VPNConnections: &config.ResourceQuery{}},
		KMS: &config.KMSResources{Keys: &config.ResourceQuery{}},
		SecretsManager: &config.SecretsManagerResources{
			Secrets: &config.ResourceQuery{},
		},
		SSM: &config.SSMResources{Parameters: &config.ResourceQuery{}},
	}
}

func TestCollectAll(t *testing.T) {
	acct := &config.AWSAccount{Regions: []string{"us-east-1", "us-west-2"}, Resources: allResources()}
	var built []string
	newFake := func(cfg awssdk.Config) *clients {
		built = append(built, cfg.Region)
		return newFakes().clients()
	}

	regions, err := collectAll(context.Background(), acct, awssdk.Config{}, newFake)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(built, acct.Regions) {
		t.Errorf("built clients for regions %v, want %v", built, acct.Regions)
	}
	want := testCollected()
	want.KMSKeys, want.Secrets, want.Parameters = fakeRecords()
	if len(regions) != len(acct.Regions) {
		t.Fatalf("got %d regions, want %d", len(regions), len(acct.Regions))
	}
	for i, name := range acct.Regions {
		if regions[i].Name != name {
			t.Errorf("region %d is %q, want %q", i, regions[i].Name, name)
		}
		if !reflect.DeepEqual(regions[i].Records, want.records(name)) {
			t.Errorf("%s records =\n%+v\nwant\n%+v", name, regions[i].Records, want.records(name))
		}
	}
}

// A type the config does not name is skipped, unless it is a container type.
func TestCollectAllSkipsUnnamedTypes(t *testing.T) {
	acct := &config.AWSAccount{Regions: []string{"us-east-1"}}
	f := newFakes()

	if _, err := collectAll(
		context.Background(), acct, awssdk.Config{}, func(awssdk.Config) *clients { return f.clients() },
	); err != nil {
		t.Fatal(err)
	}
	if n := len(recorded[*ec2.DescribeInstancesInput](&f.ec2.fakeCalls)); n != 0 {
		t.Errorf("DescribeInstances called %d times, want 0", n)
	}
	if n := len(recorded[*ec2.DescribeVpnConnectionsInput](&f.ec2.fakeCalls)); n != 0 {
		t.Errorf("DescribeVpnConnections called %d times, want 0", n)
	}
	for name, calls := range map[string]*fakeCalls{
		"kms": &f.kms.fakeCalls, "secretsmanager": &f.secretsManager.fakeCalls, "ssm": &f.ssm.fakeCalls,
	} {
		if len(calls.inputs) != 0 {
			t.Errorf("%s called %d times, want 0", name, len(calls.inputs))
		}
	}
	if n := len(recorded[*ec2.DescribeVpcsInput](&f.ec2.fakeCalls)); n != 1 {
		t.Errorf("DescribeVpcs called %d times, want 1", n)
	}
}

func TestCollectAllNamesFailingCollector(t *testing.T) {
	acct := &config.AWSAccount{Regions: []string{"us-east-1"}, Resources: allResources()}
	f := newFakes()
	f.kms.err = errFake

	_, err := collectAll(
		context.Background(), acct, awssdk.Config{}, func(awssdk.Config) *clients { return f.clients() },
	)
	if !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want %v", err, errFake)
	}
	if want := "collect kms.keys in us-east-1"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
}

// isolateAWSConfig points the SDK at a shared config file holding one
// profile, "example", so tests never read the user's own AWS config.
func isolateAWSConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("[profile example]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	for _, v := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(v, "")
	}
}

func TestLoadAWSConfig(t *testing.T) {
	isolateAWSConfig(t)
	ctx := context.Background()

	if _, err := loadAWSConfig(ctx, ""); err != nil {
		t.Errorf("default chain: %v", err)
	}
	cfg, err := loadAWSConfig(ctx, "example")
	if err != nil {
		t.Fatalf("profile example: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("profile example region = %q, want us-east-1", cfg.Region)
	}
	if _, err := loadAWSConfig(ctx, "absent"); err == nil || !strings.Contains(err.Error(), `"absent"`) {
		t.Errorf("profile absent: err = %v, want one naming the profile", err)
	}
}

func TestCollect(t *testing.T) {
	isolateAWSConfig(t)
	ctx := context.Background()

	if _, err := Collect(ctx, &config.AWSAccount{SSOProfileName: "absent"}); err == nil {
		t.Error("Collect with an absent profile succeeded, want an error")
	}
	// No regions means no SDK calls, so this runs offline.
	regions, err := Collect(ctx, &config.AWSAccount{SSOProfileName: "example"})
	if err != nil || len(regions) != 0 {
		t.Errorf("Collect with no regions = %v, %v; want no regions and no error", regions, err)
	}
}

func TestNewClients(t *testing.T) {
	c := newClients(awssdk.Config{Region: "us-east-1"})
	if _, ok := c.ec2.(*ec2.Client); !ok {
		t.Errorf("ec2 is %T, want *ec2.Client", c.ec2)
	}
	if _, ok := c.kms.(*kms.Client); !ok {
		t.Errorf("kms is %T, want *kms.Client", c.kms)
	}
	if _, ok := c.secretsManager.(*secretsmanager.Client); !ok {
		t.Errorf("secretsManager is %T, want *secretsmanager.Client", c.secretsManager)
	}
	if _, ok := c.ssm.(*ssm.Client); !ok {
		t.Errorf("ssm is %T, want *ssm.Client", c.ssm)
	}
}

// Every collector assigns its own field of collected and no other, which is
// what keeps concurrent collection free of locks.
func TestCollectors(t *testing.T) {
	want := testCollected()
	want.KMSKeys, want.Secrets, want.Parameters = fakeRecords()
	vpn := &want.VPNConnections[0]
	vpn.CustomerGatewayConfiguration = nil
	vpn.Options.TunnelOptions[0].PreSharedKey = nil
	wv := reflect.ValueOf(want).Elem()

	for _, c := range collectors {
		t.Run(c.name, func(t *testing.T) {
			got := &collected{}
			n, err := c.collect(context.Background(), newFakes().clients(), &config.ResourceQuery{}, got)
			if err != nil {
				t.Fatal(err)
			}
			gv := reflect.ValueOf(got).Elem()
			var set []string
			for i := range gv.NumField() {
				field := gv.Field(i)
				if field.IsZero() {
					continue
				}
				name := gv.Type().Field(i).Name
				set = append(set, name)
				if n != field.Len() {
					t.Errorf("returned %d, but %s holds %d", n, name, field.Len())
				}
				if !reflect.DeepEqual(field.Interface(), wv.Field(i).Interface()) {
					t.Errorf("%s =\n%+v\nwant\n%+v", name, field.Interface(), wv.Field(i).Interface())
				}
			}
			if len(set) != 1 {
				t.Errorf("set fields %v, want exactly one", set)
			}
		})
	}
}

func TestCollectorsReturnErrors(t *testing.T) {
	for _, c := range collectors {
		t.Run(c.name, func(t *testing.T) {
			f := newFakes()
			f.fail(errFake)
			got := &collected{}
			n, err := c.collect(context.Background(), f.clients(), &config.ResourceQuery{}, got)
			if !errors.Is(err, errFake) || n != 0 {
				t.Errorf("collect = %d, %v; want 0, %v", n, err, errFake)
			}
			if !reflect.ValueOf(*got).IsZero() {
				t.Errorf("a failed collect assigned %+v", got)
			}
		})
	}
}

// collectorNamed returns the registered collector for a config path.
func collectorNamed(t *testing.T, name string) collector {
	t.Helper()
	i := slices.IndexFunc(collectors, func(c collector) bool { return c.name == name })
	if i < 0 {
		t.Fatalf("no collector named %q", name)
	}
	return collectors[i]
}

// first returns the first input of type In a fake received.
func first[In any](t *testing.T, f *fakeCalls) In {
	t.Helper()
	ins := recorded[In](f)
	if len(ins) == 0 {
		var zero In
		t.Fatalf("no %T call recorded", zero)
	}
	return ins[0]
}
