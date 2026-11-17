package aws_sdk

import (
	"context"
	"slices"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// collectSecrets lists secrets with ListSecrets, which returns metadata
// only. Config filters pass through as ListSecrets filters (keys such as
// "name" or "tag-key"). The API's name filter matches prefixes, so q.IDs is
// matched exactly here against the name or ARN.
func collectSecrets(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	input := &secretsmanager.ListSecretsInput{}
	for _, f := range q.Filters {
		input.Filters = append(
			input.Filters,
			smtypes.Filter{Key: smtypes.FilterNameStringType(f.Name), Values: f.Values},
		)
	}

	var secrets []inventory.Record
	pages := secretsmanager.NewListSecretsPaginator(api.secretsManager, input)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		for _, s := range page.SecretList {
			name, arn := awssdk.ToString(s.Name), awssdk.ToString(s.ARN)
			if len(q.IDs) == 0 || slices.Contains(q.IDs, name) || slices.Contains(q.IDs, arn) {
				secrets = append(secrets, aws.Secret(arn, name))
			}
		}
	}
	region.Secrets = secrets
	return len(secrets), nil
}
