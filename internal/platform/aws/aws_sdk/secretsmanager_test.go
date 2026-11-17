package aws_sdk

import (
	"context"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

func TestCollectSecretsSendsFilters(t *testing.T) {
	f := newFakes()
	q := &config.ResourceQuery{Filters: []config.Filter{{Name: "name", Values: []string{"app/"}}}}
	if _, err := collectSecrets(context.Background(), f.clients(), q, &collected{}); err != nil {
		t.Fatal(err)
	}
	want := []smtypes.Filter{{Key: smtypes.FilterNameStringTypeName, Values: []string{"app/"}}}
	if got := first[*secretsmanager.ListSecretsInput](t, &f.secretsManager.fakeCalls).Filters; !reflect.DeepEqual(
		got, want,
	) {
		t.Errorf("filters = %+v, want %+v", got, want)
	}
}

// The API's name filter matches prefixes, so the collector matches q.IDs
// exactly itself.
func TestCollectSecretsMatchesIDs(t *testing.T) {
	_, all, _ := fakeRecords()
	db, api := all[0], all[1]
	tests := []struct {
		name string
		ids  []string
		want []inventory.Record
	}{
		{"none", nil, all},
		{"name", []string{"app/api"}, []inventory.Record{api}},
		{"ARN", []string{testSecretARN("app/db")}, []inventory.Record{db}},
		{"prefix", []string{"app"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region := &collected{}
			n, err := collectSecrets(
				context.Background(), newFakes().clients(), &config.ResourceQuery{IDs: tt.ids}, region,
			)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(tt.want) || !reflect.DeepEqual(region.Secrets, tt.want) {
				t.Errorf("collect = %d, %+v; want %+v", n, region.Secrets, tt.want)
			}
		})
	}
}
