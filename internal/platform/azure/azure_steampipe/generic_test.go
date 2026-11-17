package azure_steampipe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"diagram/internal/platform/azure"
	"diagram/internal/steampipe"
)

func TestIncludeAll(t *testing.T) {
	rows := testRows()
	rows["information_schema"] = []steampipe.Row{
		row("table_name", "azure_kubernetes_cluster", "columns", "type,agent_pool_profiles"),
		row("table_name", "azure_custom_thing", "columns", ""),
		row("table_name", "azure_key_vault_key", "columns", "type"),
		row("table_name", "azure_network_watcher", "columns", "type"),
		row("table_name", "azure_virtual_network", "columns", "type"),
	}
	aks := rg + "Microsoft.ContainerService/managedClusters/AKS"
	rows["azure_kubernetes_cluster"] = []steampipe.Row{
		base(aks, "AKS", "type", "Microsoft.ContainerService/ManagedClusters",
			"agent_pool_profiles", `[{"name": "system", "vnetSubnetID": "`+subnetA+`"}]`),
	}
	rows["azure_custom_thing"] = []steampipe.Row{base(rg+"Custom/things/T", "T")}
	src := &fakeSource{rows: rows}
	sub := everything()
	sub.Resources.IncludeAll = true

	regions, err := Collect(context.Background(), src, sub)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, r := range regions[0].Records {
		byName[r.Name] = r.Type
		if r.Name == "AKS" && (len(r.Refs) != 1 || r.Refs[0].ID != strings.ToLower(subnetA)) {
			t.Errorf("AKS refs = %+v, want its node pool's subnet", r.Refs)
		}
	}
	if byName["AKS"] != azure.TypeManagedCluster || byName["T"] != "azure_custom_thing" {
		t.Errorf("generic types = %v", byName)
	}
	for _, table := range []string{"azure_key_vault_key", "azure_network_watcher"} {
		if src.table(table) != nil {
			t.Errorf("include_all read %s", table)
		}
	}
	if c := src.table("azure_kubernetes_cluster"); !strings.Contains(c.sql, "agent_pool_profiles::text") ||
		strings.Contains(c.sql, "tags") {
		t.Errorf("AKS query = %s, want only the generic columns", c.sql)
	}
	// The mapped table is read once, by its own mapping.
	n := 0
	for _, c := range src.calls {
		if strings.Contains(c.sql, "FROM azure_virtual_network ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("azure_virtual_network read %d times, want 1", n)
	}
}

func TestIncludeAllErrors(t *testing.T) {
	sub := everything()
	sub.Resources.IncludeAll = true

	boom := errors.New("boom")
	failing := &discoverFails{fakeSource: fakeSource{rows: testRows()}, err: boom}
	if _, err := Collect(context.Background(), failing, sub); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the discovery error", err)
	}

	rows := testRows()
	rows["information_schema"] = []steampipe.Row{
		row("table_name", "azure_kubernetes_cluster", "columns", "agent_pool_profiles"),
	}
	rows["azure_kubernetes_cluster"] = []steampipe.Row{base("aks", "AKS", "agent_pool_profiles", "{bad")}
	if _, err := Collect(context.Background(), &fakeSource{rows: rows}, sub); err == nil {
		t.Error("bad node pools did not fail")
	}
}

// discoverFails fails discovery only.
type discoverFails struct {
	fakeSource
	err error
}

func (d *discoverFails) Query(ctx context.Context, sql string, args ...any) ([]steampipe.Row, error) {
	if strings.Contains(sql, "information_schema") {
		return nil, d.err
	}
	return d.fakeSource.Query(ctx, sql, args...)
}
