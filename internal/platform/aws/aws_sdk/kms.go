package aws_sdk

import (
	"context"
	"slices"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// collectKMSKeys lists keys and their aliases. KMS list calls take no
// filters, so q.IDs is matched here against the key ID, ARN, or any alias.
func collectKMSKeys(
	ctx context.Context,
	api *clients,
	q *config.ResourceQuery,
	region *collected,
) (int, error) {
	aliases := map[string][]string{}
	aliasPages := kms.NewListAliasesPaginator(api.kms, &kms.ListAliasesInput{})
	for aliasPages.HasMorePages() {
		page, err := aliasPages.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		for _, a := range page.Aliases {
			if a.TargetKeyId != nil {
				aliases[*a.TargetKeyId] = append(aliases[*a.TargetKeyId], awssdk.ToString(a.AliasName))
			}
		}
	}

	var keys []inventory.Record
	keyPages := kms.NewListKeysPaginator(api.kms, &kms.ListKeysInput{})
	for keyPages.HasMorePages() {
		page, err := keyPages.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		for _, k := range page.Keys {
			id, arn := awssdk.ToString(k.KeyId), awssdk.ToString(k.KeyArn)
			names := aliases[id]
			if len(q.IDs) > 0 && !slices.ContainsFunc(
				append([]string{id, arn}, names...),
				func(n string) bool { return slices.Contains(q.IDs, n) },
			) {
				continue
			}
			// An alias names a key better than its ID does.
			name := id
			if len(names) > 0 {
				name = names[0]
			}
			keys = append(keys, aws.KMSKey(id, name))
		}
	}
	region.KMSKeys = keys
	return len(keys), nil
}
