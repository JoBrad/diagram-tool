package azure_steampipe

import (
	"context"
	"fmt"
	"log"

	"diagram/internal/inventory"
	"diagram/internal/platform/azure"
	"diagram/internal/steampipe"
)

// discovery finds the Azure tables include_all can read: every table with an
// ID, a name, a resource group, a region, and the scoping columns. Tables without a
// region hold subscription-wide settings (role assignments, policies,
// Defender settings), which have no place in a regional diagram.
var discovery = steampipe.Discovery{
	Prefix:   "azure_",
	Required: []string{"id", "name", "resource_group", "region", "subscription_id", "_deleted_at"},
	Optional: []string{"type", "agent_pool_profiles"},
}

// neverRead are tables include_all must not read even an ID from: they list
// what a key vault holds.
var neverRead = map[string]bool{
	"azure_key_vault_key":         true,
	"azure_key_vault_key_version": true,
	"azure_key_vault_secret":      true,
	"azure_key_vault_certificate": true,
}

// skipTables are discovered tables include_all leaves out, with the reason.
var skipTables = map[string]string{
	"azure_network_watcher":            "Azure creates a network watcher in every region; it is not drawn",
	"azure_network_watcher_flow_log":   "flow log settings are not drawn",
	"azure_log_alert":                  "alert rules are not drawn",
	"azure_log_profile":                "log profiles are not drawn",
	"azure_security_center_automation": "Defender automations are not drawn",
}

// genericTables returns a table for every discovered Azure table that has no
// hand-written mapping. Each reads only its ID, name, type, and the subnets
// an AKS cluster's node pools sit in, never the rest of the row.
func genericTables(ctx context.Context, src steampipe.Source) ([]table, error) {
	found, err := steampipe.Discover(ctx, src, discovery)
	if err != nil {
		return nil, err
	}
	mapped := map[string]bool{}
	for _, t := range tables {
		mapped[t.Name] = true
	}

	var out []table
	for _, d := range found {
		if mapped[d.Table] || neverRead[d.Table] {
			continue
		}
		if reason, ok := skipTables[d.Table]; ok {
			log.Printf("Skipping %s: %s", d.Table, reason)
			continue
		}
		var columns []string
		for _, c := range discovery.Optional {
			if d.Columns[c] {
				columns = append(columns, c)
			}
		}
		if !d.Columns["type"] {
			log.Printf("WARN %s: no type column, using the table name", d.Table)
		}
		out = append(out, table{
			Table:    newTable(d.Table, columns...),
			resource: d.Table,
			record:   genericRecord(d.Table),
		})
	}
	return out, nil
}

// genericRecord returns a record reader for a discovered table. A row's type
// column gives its resource type; without one, the table name stands in.
func genericRecord(tableName string) func(steampipe.Row) ([]inventory.Record, error) {
	return func(row steampipe.Row) ([]inventory.Record, error) {
		rt := tableName
		if t := steampipe.Text(row, "type"); t != "" {
			rt = azure.ResourceType(t)
		}
		subnets, err := steampipe.JSONIDs(row, "agent_pool_profiles", []string{"vnetSubnetID"})
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", rt, steampipe.Text(row, "id"), err)
		}
		return one(azure.Generic(rt, steampipe.Text(row, "id"), steampipe.Text(row, "name"), subnets)), nil
	}
}
