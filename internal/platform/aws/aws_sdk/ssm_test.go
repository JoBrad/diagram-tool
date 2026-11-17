package aws_sdk

import (
	"context"
	"reflect"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"diagram/internal/config"
)

// q.IDs become an exact Name filter ahead of the query's own filters.
func TestCollectSSMParametersSendsFilters(t *testing.T) {
	tests := []struct {
		name string
		q    *config.ResourceQuery
		want []ssmtypes.ParameterStringFilter
	}{
		{"none", &config.ResourceQuery{}, nil},
		{
			"IDs and filters",
			&config.ResourceQuery{
				IDs:     []string{"/app/db/host"},
				Filters: []config.Filter{{Name: "Path", Values: []string{"/app"}}},
			},
			[]ssmtypes.ParameterStringFilter{
				{Key: awssdk.String("Name"), Option: awssdk.String("Equals"), Values: []string{"/app/db/host"}},
				{Key: awssdk.String("Path"), Values: []string{"/app"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()
			if _, err := collectSSMParameters(context.Background(), f.clients(), tt.q, &collected{}); err != nil {
				t.Fatal(err)
			}
			got := first[*ssm.DescribeParametersInput](t, &f.ssm.fakeCalls).ParameterFilters
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filters = %+v, want %+v", got, tt.want)
			}
		})
	}
}
