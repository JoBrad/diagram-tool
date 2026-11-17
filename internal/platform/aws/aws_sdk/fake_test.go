package aws_sdk

import (
	"context"
	"strconv"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// fakeCalls records the inputs a fake receives, and fails every call with
// err when it is set. Collect runs collectors concurrently against one
// region's fakes, so recording is locked.
type fakeCalls struct {
	err    error
	mu     sync.Mutex
	inputs []any
}

func (f *fakeCalls) record(in any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	return f.err
}

// recorded returns every input of type In the fake received, in call order.
func recorded[In any](f *fakeCalls) []In {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []In
	for _, in := range f.inputs {
		if v, ok := in.(In); ok {
			out = append(out, v)
		}
	}
	return out
}

// paged serves items one per page. The page token is the next item's index,
// so every paginated fake call with more than one item exercises paging.
func paged[T any](items []T, token *string) ([]T, *string) {
	i := 0
	if token != nil {
		i, _ = strconv.Atoi(*token)
	}
	if i >= len(items) {
		return nil, nil
	}
	var next *string
	if i+1 < len(items) {
		next = awssdk.String(strconv.Itoa(i + 1))
	}
	return items[i : i+1], next
}

// fakes holds one region's fake clients.
type fakes struct {
	ec2            *fakeEC2
	kms            *fakeKMS
	secretsManager *fakeSecretsManager
	ssm            *fakeSSM
}

// newFakes returns fakes that serve one example of each supported resource
// type: the EC2 resources in testCollected, and the KMS keys, secrets, and
// parameters below.
func newFakes() *fakes {
	s := awssdk.String
	return &fakes{
		ec2: &fakeEC2{data: testCollected()},
		kms: &fakeKMS{
			keys: []kmstypes.KeyListEntry{
				{KeyId: s("key-1"), KeyArn: s(testKeyARN("key-1"))},
				{KeyId: s("key-2"), KeyArn: s(testKeyARN("key-2"))},
			},
			aliases: []kmstypes.AliasListEntry{
				{AliasName: s("alias/app"), TargetKeyId: s("key-1")},
				{AliasName: s("alias/app-old"), TargetKeyId: s("key-1")},
				// An AWS managed alias whose key has not been created yet.
				{AliasName: s("alias/aws/ebs")},
			},
		},
		secretsManager: &fakeSecretsManager{secrets: []smtypes.SecretListEntry{
			{Name: s("app/db"), ARN: s(testSecretARN("app/db"))},
			{Name: s("app/api"), ARN: s(testSecretARN("app/api"))},
		}},
		ssm: &fakeSSM{params: []ssmtypes.ParameterMetadata{
			{Name: s("/app/db/host"), ARN: s(testParameterARN("/app/db/host"))},
			{Name: s("/app/db/port"), ARN: s(testParameterARN("/app/db/port"))},
		}},
	}
}

func testKeyARN(id string) string { return "arn:aws:kms:us-east-1:111122223333:key/" + id }

func testSecretARN(name string) string {
	return "arn:aws:secretsmanager:us-east-1:111122223333:secret:" + name + "-AbCdEf"
}

func testParameterARN(name string) string {
	return "arn:aws:ssm:us-east-1:111122223333:parameter" + name
}

// fakeRecords returns the KMS, Secrets Manager, and SSM records the
// collectors build from newFakes, with no query narrowing them.
func fakeRecords() (keys, secrets, params []inventory.Record) {
	keys = []inventory.Record{
		aws.KMSKey("key-1", "alias/app"),
		aws.KMSKey("key-2", "key-2"),
	}
	secrets = []inventory.Record{
		aws.Secret(testSecretARN("app/db"), "app/db"),
		aws.Secret(testSecretARN("app/api"), "app/api"),
	}
	params = []inventory.Record{
		aws.Parameter(testParameterARN("/app/db/host"), "/app/db/host"),
		aws.Parameter(testParameterARN("/app/db/port"), "/app/db/port"),
	}
	return keys, secrets, params
}

func (f *fakes) clients() *clients {
	return &clients{ec2: f.ec2, kms: f.kms, secretsManager: f.secretsManager, ssm: f.ssm}
}

// fail makes every call to every fake return err.
func (f *fakes) fail(err error) {
	for _, c := range []*fakeCalls{&f.ec2.fakeCalls, &f.kms.fakeCalls, &f.secretsManager.fakeCalls, &f.ssm.fakeCalls} {
		c.err = err
	}
}

// fakeEC2 serves the resources in data.
type fakeEC2 struct {
	fakeCalls
	data *collected
}

var _ ec2API = (*fakeEC2)(nil)

func (f *fakeEC2) DescribeInstances(
	_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options),
) (*ec2.DescribeInstancesOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.Instances, in.NextToken)
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: page}}, NextToken: next,
	}, nil
}

func (f *fakeEC2) DescribeVpnConnections(
	_ context.Context, in *ec2.DescribeVpnConnectionsInput, _ ...func(*ec2.Options),
) (*ec2.DescribeVpnConnectionsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	return &ec2.DescribeVpnConnectionsOutput{VpnConnections: f.data.VPNConnections}, nil
}

func (f *fakeEC2) DescribeVpcs(
	_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options),
) (*ec2.DescribeVpcsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.VPCs, in.NextToken)
	return &ec2.DescribeVpcsOutput{Vpcs: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeSubnets(
	_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options),
) (*ec2.DescribeSubnetsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.Subnets, in.NextToken)
	return &ec2.DescribeSubnetsOutput{Subnets: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeSecurityGroups(
	_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options),
) (*ec2.DescribeSecurityGroupsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.SecurityGroups, in.NextToken)
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeInternetGateways(
	_ context.Context, in *ec2.DescribeInternetGatewaysInput, _ ...func(*ec2.Options),
) (*ec2.DescribeInternetGatewaysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.InternetGateways, in.NextToken)
	return &ec2.DescribeInternetGatewaysOutput{InternetGateways: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeNatGateways(
	_ context.Context, in *ec2.DescribeNatGatewaysInput, _ ...func(*ec2.Options),
) (*ec2.DescribeNatGatewaysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.NATGateways, in.NextToken)
	return &ec2.DescribeNatGatewaysOutput{NatGateways: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeVpnGateways(
	_ context.Context, in *ec2.DescribeVpnGatewaysInput, _ ...func(*ec2.Options),
) (*ec2.DescribeVpnGatewaysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	return &ec2.DescribeVpnGatewaysOutput{VpnGateways: f.data.VPNGateways}, nil
}

func (f *fakeEC2) DescribeTransitGateways(
	_ context.Context, in *ec2.DescribeTransitGatewaysInput, _ ...func(*ec2.Options),
) (*ec2.DescribeTransitGatewaysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.TransitGateways, in.NextToken)
	return &ec2.DescribeTransitGatewaysOutput{TransitGateways: page, NextToken: next}, nil
}

func (f *fakeEC2) DescribeCustomerGateways(
	_ context.Context, in *ec2.DescribeCustomerGatewaysInput, _ ...func(*ec2.Options),
) (*ec2.DescribeCustomerGatewaysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	return &ec2.DescribeCustomerGatewaysOutput{CustomerGateways: f.data.CustomerGateways}, nil
}

func (f *fakeEC2) DescribeManagedPrefixLists(
	_ context.Context, in *ec2.DescribeManagedPrefixListsInput, _ ...func(*ec2.Options),
) (*ec2.DescribeManagedPrefixListsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.data.PrefixLists, in.NextToken)
	return &ec2.DescribeManagedPrefixListsOutput{PrefixLists: page, NextToken: next}, nil
}

type fakeKMS struct {
	fakeCalls
	keys    []kmstypes.KeyListEntry
	aliases []kmstypes.AliasListEntry
	// keysErr fails ListKeys alone, after ListAliases has succeeded.
	keysErr error
}

var _ kmsAPI = (*fakeKMS)(nil)

func (f *fakeKMS) ListAliases(
	_ context.Context, in *kms.ListAliasesInput, _ ...func(*kms.Options),
) (*kms.ListAliasesOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.aliases, in.Marker)
	return &kms.ListAliasesOutput{Aliases: page, NextMarker: next, Truncated: next != nil}, nil
}

func (f *fakeKMS) ListKeys(
	_ context.Context, in *kms.ListKeysInput, _ ...func(*kms.Options),
) (*kms.ListKeysOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	page, next := paged(f.keys, in.Marker)
	return &kms.ListKeysOutput{Keys: page, NextMarker: next, Truncated: next != nil}, nil
}

type fakeSecretsManager struct {
	fakeCalls
	secrets []smtypes.SecretListEntry
}

var _ secretsmanager.ListSecretsAPIClient = (*fakeSecretsManager)(nil)

func (f *fakeSecretsManager) ListSecrets(
	_ context.Context, in *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options),
) (*secretsmanager.ListSecretsOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.secrets, in.NextToken)
	return &secretsmanager.ListSecretsOutput{SecretList: page, NextToken: next}, nil
}

type fakeSSM struct {
	fakeCalls
	params []ssmtypes.ParameterMetadata
}

var _ ssm.DescribeParametersAPIClient = (*fakeSSM)(nil)

func (f *fakeSSM) DescribeParameters(
	_ context.Context, in *ssm.DescribeParametersInput, _ ...func(*ssm.Options),
) (*ssm.DescribeParametersOutput, error) {
	if err := f.record(in); err != nil {
		return nil, err
	}
	page, next := paged(f.params, in.NextToken)
	return &ssm.DescribeParametersOutput{Parameters: page, NextToken: next}, nil
}
