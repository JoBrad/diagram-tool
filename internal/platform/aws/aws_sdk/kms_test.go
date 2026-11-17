package aws_sdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

// KMS list calls take no filters, so the collector matches q.IDs itself.
func TestCollectKMSKeysMatchesIDs(t *testing.T) {
	key1 := aws.KMSKey("key-1", "alias/app")
	key2 := aws.KMSKey("key-2", "key-2")
	tests := []struct {
		name string
		ids  []string
		want []inventory.Record
	}{
		{"none", nil, []inventory.Record{key1, key2}},
		{"key ID", []string{"key-2"}, []inventory.Record{key2}},
		{"key ARN", []string{testKeyARN("key-1")}, []inventory.Record{key1}},
		{"second alias", []string{"alias/app-old"}, []inventory.Record{key1}},
		{"no match", []string{"alias/absent"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region := &collected{}
			n, err := collectKMSKeys(
				context.Background(), newFakes().clients(), &config.ResourceQuery{IDs: tt.ids}, region,
			)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(tt.want) || !reflect.DeepEqual(region.KMSKeys, tt.want) {
				t.Errorf("collect = %d, %+v; want %+v", n, region.KMSKeys, tt.want)
			}
		})
	}
}

func TestCollectKMSKeysListKeysError(t *testing.T) {
	f := newFakes()
	f.kms.keysErr = errFake
	region := &collected{}
	if _, err := collectKMSKeys(context.Background(), f.clients(), &config.ResourceQuery{}, region); !errors.Is(
		err, errFake,
	) {
		t.Errorf("err = %v, want %v", err, errFake)
	}
	if region.KMSKeys != nil {
		t.Errorf("a failed collect assigned %+v", region.KMSKeys)
	}
}
