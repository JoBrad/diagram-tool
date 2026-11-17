package config

import (
	"strings"
	"testing"
)

const subID = "00000000-0000-0000-0000-000000000001"

func TestReadAzure(t *testing.T) {
	cfg, err := readNoApp(write(t, `{"cloud": {"azure_gov": {"subscription_id": "`+subID+`",
		"regions": ["usgovvirginia"], "resources": {"include_all": true,
		"compute": {"virtual_machines": {"resource_groups": ["rg-app"], "tags": {"env": "prod"}}},
		"network": {"subnets": {"hide": true}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if key, acct := cfg.Account(); acct != nil {
		t.Errorf("Account() = %s, %+v; want nil for an Azure config", key, acct)
	}
	key, sub := cfg.Subscription()
	if key != "azure_gov" || sub.Source != SourceSteampipe || sub.SubscriptionID != subID ||
		!sub.Resources.IncludeAll {
		t.Fatalf("Subscription() = %s, %+v", key, sub)
	}
	vms := AzureQuery(AzureCompute, func(c *AzureComputeResources) *AzureResourceQuery { return c.VirtualMachines })
	if q := vms(&sub.Resources); q == nil || q.ResourceGroups[0] != "rg-app" || q.Tags["env"] != "prod" {
		t.Errorf("virtual_machines = %+v", q)
	}
	subnets := AzureQuery(AzureNetwork, func(n *AzureNetworkResources) *AzureResourceQuery { return n.Subnets })
	if q := subnets(&sub.Resources); q == nil || !q.Hide {
		t.Errorf("subnets = %+v, want hidden", q)
	}
	vaults := AzureQuery(AzureKeyVault, func(k *AzureKeyVaultResources) *AzureResourceQuery { return k.Vaults })
	stores := AzureQuery(AzureAppConfig, func(a *AzureAppConfigurationResources) *AzureResourceQuery {
		return a.Stores
	})
	if vaults(&sub.Resources) != nil || stores(&sub.Resources) != nil {
		t.Error("absent services gave a query")
	}

	cfg, err = readNoApp(write(t, `{"cloud": {"azure": {"subscription_id": "`+subID+`", "regions": ["eastus"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if key, _ := cfg.Subscription(); key != "azure" {
		t.Errorf("Subscription() key = %s, want azure", key)
	}
}

func TestReadAzureErrors(t *testing.T) {
	azure := func(fields string) string {
		return `{"cloud": {"azure": {"subscription_id": "` + subID + `", "regions": ["eastus"]` + fields + `}}}`
	}
	tests := []struct {
		name, body, want string
	}{
		{
			"aws and azure",
			`{"cloud": {"aws": {"regions": ["us-east-1"]}, "azure_gov": {"subscription_id": "` + subID +
				`", "regions": ["usgovtexas"]}}}`,
			"cloud: set only one of aws, azure_gov;",
		},
		{"sdk source", azure(`, "source": "sdk"`), `cloud.azure.source: Azure is read only through "steampipe"`},
		{
			"bad subscription",
			`{"cloud": {"azure": {"subscription_id": "x", "regions": ["eastus"]}}}`,
			`cloud.azure.subscription_id: "x" is not a subscription ID`,
		},
		{"bad tenant", azure(`, "tenant_id": "t"`), `cloud.azure.tenant_id: "t" is not a tenant ID`},
		{
			"no regions",
			`{"cloud": {"azure": {"subscription_id": "` + subID + `", "regions": []}}}`,
			"cloud.azure.regions: empty",
		},
		{
			"ids with show",
			azure(`, "resources": {"network": {"virtual_networks":
				{"ids": ["/subscriptions/x"], "show_unconnected_resources": true}}}`),
			"cloud.azure.resources.network.virtual_networks: ids and show_unconnected_resources cannot be combined",
		},
		{
			"bad id",
			azure(`, "resources": {"compute": {"disks": {"ids": ["disk-1"]}}}`),
			`cloud.azure.resources.compute.disks.ids: "disk-1" is not an ARM resource ID`,
		},
		{
			"key vault keys",
			azure(`, "resources": {"keyvault": {"keys": {}}}`),
			`unknown field "keys"`,
		},
		{
			"app configuration",
			azure(`, "resources": {"appconfiguration": {"stores": {"ids": ["s"]}}}`),
			`appconfiguration.stores.ids: "s" is not an ARM resource ID`,
		},
		{
			"key vault",
			azure(`, "resources": {"keyvault": {"vaults": {"ids": ["v"]}}}`),
			`keyvault.vaults.ids: "v" is not an ARM resource ID`,
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
