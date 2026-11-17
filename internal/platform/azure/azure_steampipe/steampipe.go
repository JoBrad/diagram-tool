// Package azure_steampipe reads Azure resources from Steampipe's Azure
// tables in a PostgreSQL database.
// Each row is decoded into the Azure SDK type the ARM API returns and handed
// to package azure's record builders.
//
// Steampipe's Azure tables carry secret-bearing columns (a virtual machine's
// secrets and SSH keys, a key vault's access policies), so every table here
// lists the columns it reads. The Key Vault key, secret, and certificate
// tables are never read.
package azure_steampipe

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/azure"
	"diagram/internal/steampipe"
)

// table is one Steampipe table the reader knows how to turn into records.
type table struct {
	steampipe.Table
	// resource is the type's path in the config file, used in logs and
	// errors.
	resource string
	query    func(*config.AzureResources) *config.AzureResourceQuery
	// always reads the table even when the config does not name it. Set for
	// container types, and for the network interfaces and public IPs that
	// place and address virtual machines.
	always bool
	// record turns a row into its records: one, or a virtual network and
	// its subnets.
	record func(steampipe.Row) ([]inventory.Record, error)
}

// scope returns the rows of sub's subscription in its regions, leaving out
// rows marked deleted. Steampipe stores subscription IDs
// lowercased.
func scope(sub *config.AzureSubscription) steampipe.Scope {
	return steampipe.Scope{
		AccountColumn: "subscription_id", Account: strings.ToLower(sub.SubscriptionID),
		RegionColumn: "region", Regions: sub.Regions,
		DeletedColumn: "_deleted_at",
	}
}

// Collect reads the subscription's configured resource types from every
// configured region. Types the database does not hold are logged and skipped.
// With include_all, every other regional Azure table in the database is read as
// well.
func Collect(ctx context.Context, src steampipe.Source, sub *config.AzureSubscription) ([]*inventory.Region, error) {
	if sub.SubscriptionID == "" {
		return nil, errors.New("the steampipe source needs a subscription ID")
	}
	for _, name := range missing(&sub.Resources) {
		log.Printf("WARN %s: not in the Steampipe database yet, skipped", name)
	}

	regions := make([]*inventory.Region, len(sub.Regions))
	byName := map[string]*inventory.Region{}
	for i, name := range sub.Regions {
		regions[i] = &inventory.Region{Name: name}
		byName[name] = regions[i]
	}

	for _, t := range tables {
		q := t.query(&sub.Resources)
		if q == nil {
			if !t.always && !sub.Resources.IncludeAll {
				continue
			}
			q = &config.AzureResourceQuery{}
		}
		if err := read(ctx, src, t, sub, q, byName); err != nil {
			return nil, err
		}
	}
	narrowSubnets(regions, subnetQuery(&sub.Resources))

	if sub.Resources.IncludeAll {
		generic, err := genericTables(ctx, src)
		if err != nil {
			return nil, err
		}
		for _, t := range generic {
			if err := read(ctx, src, t, sub, &config.AzureResourceQuery{}, byName); err != nil {
				return nil, err
			}
		}
	}
	return regions, nil
}

// read queries one table and adds the records of the rows q selects to their
// regions. IDs are matched in the query; resource groups and tags are
// matched here. Rows from regions the config does not name are dropped.
func read(
	ctx context.Context, src steampipe.Source, t table, sub *config.AzureSubscription, q *config.AzureResourceQuery,
	byName map[string]*inventory.Region,
) error {
	st := t.Table
	if len(q.Tags) > 0 {
		st.Columns = append(slices.Clone(st.Columns), "tags")
	}
	rows, err := steampipe.Read(ctx, src, st, scope(sub), q.IDs)
	if err != nil {
		return fmt.Errorf("%s: %w", t.resource, err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		region := byName[steampipe.Text(row, "region")]
		if region == nil {
			continue
		}
		ok, err := selects(q, row)
		if err != nil {
			return fmt.Errorf("read %s from the Steampipe database: %w", t.resource, err)
		}
		if !ok {
			continue
		}
		recs, err := t.record(row)
		if err != nil {
			return fmt.Errorf("read %s from the Steampipe database: %w", t.resource, err)
		}
		region.Records = append(region.Records, recs...)
		counts[region.Name]++
	}
	for _, name := range sub.Regions {
		log.Printf("Found %d %s in %s", counts[name], t.resource, name)
	}
	return nil
}

// selects reports whether a row is in one of q's resource groups and has
// every one of its tags.
func selects(q *config.AzureResourceQuery, row steampipe.Row) (bool, error) {
	if len(q.ResourceGroups) > 0 {
		rg := steampipe.Text(row, "resource_group")
		if !slices.ContainsFunc(q.ResourceGroups, func(g string) bool { return strings.EqualFold(g, rg) }) {
			return false, nil
		}
	}
	if len(q.Tags) == 0 {
		return true, nil
	}
	var tags map[string]string
	if err := steampipe.Decode(row, "tags", &tags); err != nil {
		return false, err
	}
	for _, k := range slices.Sorted(maps.Keys(q.Tags)) {
		if v, ok := tags[k]; !ok || v != q.Tags[k] {
			return false, nil
		}
	}
	return true, nil
}

// subnetQuery returns the config's subnet query, or nil.
func subnetQuery(r *config.AzureResources) *config.AzureResourceQuery {
	if r.Network == nil {
		return nil
	}
	return r.Network.Subnets
}

// narrowSubnets drops the subnet records q does not select. Subnets are read
// inside their virtual network's row, so their query applies here: by ID and
// resource group. Subnets carry no tags of their own, so a tag condition
// selects none.
func narrowSubnets(regions []*inventory.Region, q *config.AzureResourceQuery) {
	if q == nil {
		return
	}
	keep := func(rec inventory.Record) bool {
		if len(q.IDs) > 0 &&
			!slices.ContainsFunc(q.IDs, func(id string) bool { return strings.EqualFold(id, rec.ID) }) {
			return false
		}
		rg := rec.Attrs[azure.AttrResourceGroup]
		if len(q.ResourceGroups) > 0 &&
			!slices.ContainsFunc(q.ResourceGroups, func(g string) bool { return strings.EqualFold(g, rg) }) {
			return false
		}
		return len(q.Tags) == 0
	}
	for _, r := range regions {
		r.Records = slices.DeleteFunc(r.Records, func(rec inventory.Record) bool {
			return rec.Type == azure.TypeSubnet && !keep(rec)
		})
	}
}

// missing returns the config's named resource types that no Steampipe table
// serves yet.
func missing(r *config.AzureResources) []string {
	var out []string
	if r.Network != nil && r.Network.NATGateways != nil {
		out = append(out, "network.nat_gateways")
	}
	if r.AppConfiguration != nil && r.AppConfiguration.Stores != nil {
		out = append(out, "appconfiguration.stores")
	}
	return out
}
