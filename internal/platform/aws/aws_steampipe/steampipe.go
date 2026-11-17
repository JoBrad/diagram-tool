// Package aws_steampipe reads AWS resources from Steampipe's AWS tables in
// a PostgreSQL database. Each row
// is decoded into the SDK type the AWS API returns and handed to package
// aws's record builders, so records match the SDK source's.
//
// Steampipe tables carry secret-bearing columns (aws_ec2_instance.user_data
// holds decoded user data), so every table here lists the columns it reads.
package aws_steampipe

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/steampipe"
)

// table is one Steampipe table the reader knows how to turn into records.
type table struct {
	steampipe.Table
	// resource is the type's path in the config file, used in logs and
	// errors.
	resource string
	query    func(*config.Resources) *config.ResourceQuery
	// always reads the table even when the config does not name it. Set for
	// container types, as the SDK collectors do.
	always bool
	record func(steampipe.Row) (inventory.Record, bool, error)
}

// scope returns the rows of acct's account in its regions, leaving out rows
// marked deleted.
func scope(acct *config.AWSAccount) steampipe.Scope {
	return steampipe.Scope{
		AccountColumn: "account_id", Account: acct.AccountID,
		RegionColumn: "region", Regions: acct.Regions,
		DeletedColumn: "_deleted_at",
	}
}

// Collect reads the account's configured resource types from every
// configured region. Types the database does not hold yet are logged and
// skipped. With include_all, every other network-placed AWS table in the
// database is read as well.
func Collect(ctx context.Context, src steampipe.Source, acct *config.AWSAccount) ([]*inventory.Region, error) {
	if acct.AccountID == "" {
		return nil, errors.New("the steampipe source needs an account ID")
	}
	for _, name := range missing(&acct.Resources) {
		log.Printf("WARN %s: not in the Steampipe database yet, skipped", name)
	}

	regions := make([]*inventory.Region, len(acct.Regions))
	byName := map[string]*inventory.Region{}
	for i, name := range acct.Regions {
		regions[i] = &inventory.Region{Name: name}
		byName[name] = regions[i]
	}

	for _, t := range tables {
		q := t.query(&acct.Resources)
		if q == nil {
			if !t.always && !acct.Resources.IncludeAll {
				continue
			}
			q = &config.ResourceQuery{}
		}
		if len(q.Filters) > 0 {
			return nil, fmt.Errorf(
				"resources.%s: filters are EC2 API filters, which the steampipe source does not support; use ids",
				t.resource,
			)
		}
		if err := read(ctx, src, t, acct, q.IDs, byName); err != nil {
			return nil, err
		}
	}

	if acct.Resources.IncludeAll {
		generic, err := genericTables(ctx, src)
		if err != nil {
			return nil, err
		}
		for _, t := range generic {
			if err := read(ctx, src, t, acct, nil, byName); err != nil {
				return nil, err
			}
		}
	}
	return regions, nil
}

// read queries one table and adds its records to their regions. Rows from
// regions the config does not name are dropped.
func read(
	ctx context.Context, src steampipe.Source, t table, acct *config.AWSAccount, ids []string,
	byName map[string]*inventory.Region,
) error {
	rows, err := steampipe.Read(ctx, src, t.Table, scope(acct), ids)
	if err != nil {
		return fmt.Errorf("%s: %w", t.resource, err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		region := byName[steampipe.Text(row, "region")]
		if region == nil {
			continue
		}
		rec, ok, err := t.record(row)
		if err != nil {
			return fmt.Errorf("read %s from the Steampipe database: %w", t.resource, err)
		}
		if !ok {
			continue
		}
		region.Records = append(region.Records, rec)
		counts[region.Name]++
	}
	for _, name := range acct.Regions {
		log.Printf("Found %d %s in %s", counts[name], t.resource, name)
	}
	return nil
}

// missing returns the config's named resource types that no Steampipe table
// serves yet.
func missing(r *config.Resources) []string {
	var out []string
	if r.EC2 != nil {
		for name, q := range map[string]*config.ResourceQuery{
			"ec2.vpn_connections":   r.EC2.VPNConnections,
			"ec2.internet_gateways": r.EC2.InternetGateways,
			"ec2.nat_gateways":      r.EC2.NATGateways,
			"ec2.vpn_gateways":      r.EC2.VPNGateways,
			"ec2.transit_gateways":  r.EC2.TransitGateways,
			"ec2.customer_gateways": r.EC2.CustomerGateways,
			"ec2.prefix_lists":      r.EC2.PrefixLists,
		} {
			if q != nil {
				out = append(out, name)
			}
		}
	}
	if r.KMS != nil && r.KMS.Keys != nil {
		out = append(out, "kms.keys")
	}
	if r.SecretsManager != nil && r.SecretsManager.Secrets != nil {
		out = append(out, "secretsmanager.secrets")
	}
	if r.SSM != nil && r.SSM.Parameters != nil {
		out = append(out, "ssm.parameters")
	}
	slices.Sort(out)
	return out
}
