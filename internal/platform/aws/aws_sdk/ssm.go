package aws_sdk

import (
	"context"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// collectSSMParameters lists parameters with DescribeParameters, which
// returns metadata only. Config filters pass through as ParameterFilters
// (keys such as "Name", "Path", or "tag:Env"); q.IDs become an exact Name
// filter.
func collectSSMParameters(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &ssm.DescribeParametersInput{}
	if len(q.IDs) > 0 {
		input.ParameterFilters = append(
			input.ParameterFilters,
			ssmtypes.ParameterStringFilter{Key: awssdk.String("Name"), Option: awssdk.String("Equals"), Values: q.IDs},
		)
	}
	for _, f := range q.Filters {
		input.ParameterFilters = append(
			input.ParameterFilters,
			ssmtypes.ParameterStringFilter{Key: awssdk.String(f.Name), Values: f.Values},
		)
	}

	var params []inventory.Record
	pages := ssm.NewDescribeParametersPaginator(api.ssm, input)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		for _, p := range page.Parameters {
			params = append(params, aws.Parameter(awssdk.ToString(p.ARN), awssdk.ToString(p.Name)))
		}
	}
	region.Parameters = params
	return len(params), nil
}
