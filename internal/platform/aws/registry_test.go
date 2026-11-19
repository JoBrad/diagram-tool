package aws

import (
	"strings"
	"testing"

	"diagram/internal/config"
)

// queries maps a type's key to its query, for one service's section.
type queries = map[string]*config.ResourceQuery

// ec2Resources returns resources with only an ec2 section.
func ec2Resources(q queries) *config.Resources {
	return &config.Resources{Services: map[string]map[string]*config.ResourceQuery{"ec2": q}}
}

func TestRegistry(t *testing.T) {
	names := map[string]bool{}
	for _, rt := range Registry {
		if rt.Service == "" || rt.Key == "" || rt.Type == "" {
			t.Errorf("incomplete entry %+v", rt)
		}
		if names[rt.Name()] {
			t.Errorf("%s is registered twice", rt.Name())
		}
		names[rt.Name()] = true
		if got, ok := Lookup(rt.Type); !ok || got != rt {
			t.Errorf("Lookup(%s) = %+v, %v; want %+v", rt.Type, got, ok, rt)
		}
	}
	if _, ok := Lookup("AWS::Nope::Nope"); ok {
		t.Error("Lookup of an unregistered type succeeded")
	}
}

func TestResourceTypeQuery(t *testing.T) {
	want := &config.ResourceQuery{IDs: []string{"vpc-1"}}
	vpcs, _ := Lookup(TypeVPC)
	if got := vpcs.Query(ec2Resources(queries{"vpcs": want})); got != want {
		t.Errorf("Query = %+v, want %+v", got, want)
	}
	if got := vpcs.Query(&config.Resources{}); got != nil {
		t.Errorf("Query with no ec2 section = %+v, want nil", got)
	}
}

func TestValidate(t *testing.T) {
	filters := []config.Filter{{Name: "x", Values: []string{"y"}}}
	tests := []struct {
		name     string
		services map[string]map[string]*config.ResourceQuery
		want     string
	}{
		{"none", nil, ""},
		{"known types", map[string]map[string]*config.ResourceQuery{
			"ec2": {"instances": {Filters: filters}, "vpcs": {}},
			"kms": {"keys": {IDs: []string{"key-1"}}},
		}, ""},
		{
			"unknown service",
			map[string]map[string]*config.ResourceQuery{"ec3": {"instances": {}}},
			"resources.ec3: unknown service; supported: ec2, kms, secretsmanager, ssm",
		},
		{
			"unknown type",
			map[string]map[string]*config.ResourceQuery{"ec2": {"instance": {}}},
			"resources.ec2.instance: unknown resource type; supported: customer_gateways, instances,",
		},
		{
			"kms filters",
			map[string]map[string]*config.ResourceQuery{"kms": {"keys": {Filters: filters}}},
			"resources.kms.keys: filters are not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&config.Resources{Services: tt.services})
			if tt.want == "" {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
