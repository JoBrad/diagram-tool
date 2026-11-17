// Package config reads the configuration: the optional app-level file in
// ConfigDir, and the diagram configuration file, whose options override it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
)

type Theme string

const (
	ThemeNameStateLight Theme = "light"
	ThemeNameStateDark  Theme = "dark"
)

type Projection string

const (
	ProjectionNameState2D        Projection = "2d"
	ProjectionNameStateIsometric Projection = "isometric"
)

type DiagramConfig struct {
	Name       string     `json:"name"`
	Projection Projection `json:"projection,omitempty"`
	Theme      Theme      `json:"theme,omitempty"`
	ShowGrid   *bool      `json:"showGrid,omitempty"`
}

// Config is the diagram configuration file read by create.
type Config struct {
	Cloud   Clouds        `json:"cloud"`
	Diagram DiagramConfig `json:"diagram"`
	Options Options       `json:"options"`
}

// Clouds holds one entry per cloud. Read accepts exactly one entry for now:
// the model has no account level yet, so a diagram covers one account.
type Clouds struct {
	AWS      *AWSAccount        `json:"aws,omitempty"`
	AWSGov   *AWSAccount        `json:"aws_gov,omitempty"`
	Azure    *AzureSubscription `json:"azure,omitempty"`
	AzureGov *AzureSubscription `json:"azure_gov,omitempty"`
}

// Source is where an account's resources are read from.
type Source string

const (
	// SourceSDK calls the AWS APIs live. It is the default.
	SourceSDK Source = "sdk"
	// SourceSteampipe reads Steampipe's last daily drain.
	SourceSteampipe Source = "steampipe"
)

// AWSAccount is one AWS account to draw, in the commercial or GovCloud
// partition.
type AWSAccount struct {
	Source Source `json:"source,omitempty"`
	// AccountID is required for SourceSteampipe, which holds every account. With
	// SourceSDK, the credentials decide the account.
	AccountID string `json:"account_id,omitempty"`
	// SSOProfileName is the shared-config profile SourceSDK loads
	// credentials from. Empty uses the SDK's default credential chain.
	SSOProfileName string    `json:"sso_profile_name,omitempty"`
	Regions        []string  `json:"regions"`
	Resources      Resources `json:"resources"`
}

// Account returns the configured AWS account and its key under cloud, or nil
// when the config names an Azure subscription instead. Read guarantees there
// is exactly one account or subscription.
func (c *Config) Account() (string, *AWSAccount) {
	if c.Cloud.AWSGov != nil {
		return "aws_gov", c.Cloud.AWSGov
	}
	return "aws", c.Cloud.AWS
}

// Resources holds one field per AWS service.
type Resources struct {
	// IncludeAll reads every resource type the source can supply, without
	// naming each one. Per-type queries still narrow or override their type.
	// Only the steampipe source supports it.
	IncludeAll bool `json:"include_all,omitempty"`

	EC2            *EC2Resources            `json:"ec2,omitempty"`
	KMS            *KMSResources            `json:"kms,omitempty"`
	SecretsManager *SecretsManagerResources `json:"secretsmanager,omitempty"`
	SSM            *SSMResources            `json:"ssm,omitempty"`
}

// KMSResources holds the KMS resource types. Keys are collected only when
// named. KMS list calls take no filters, so only ids apply.
type KMSResources struct {
	Keys *ResourceQuery `json:"keys,omitempty"`
}

// SecretsManagerResources holds the Secrets Manager resource types. Secrets
// are collected only when named.
type SecretsManagerResources struct {
	Secrets *ResourceQuery `json:"secrets,omitempty"`
}

// SSMResources holds the Systems Manager resource types. Parameters are
// collected only when named.
type SSMResources struct {
	Parameters *ResourceQuery `json:"parameters,omitempty"`
}

// EC2Resources holds one field per EC2 describe call.
//
// Primary resources are queried only when the config names them. Container
// resources are always queried, and their query here only narrows the
// results.
type EC2Resources struct {
	// Primary resources.
	Instances      *ResourceQuery `json:"instances,omitempty"`
	VPNConnections *ResourceQuery `json:"vpn_connections,omitempty"`

	// Container resources.
	InternetGateways *ResourceQuery `json:"internet_gateways,omitempty"`
	NATGateways      *ResourceQuery `json:"nat_gateways,omitempty"`
	CustomerGateways *ResourceQuery `json:"customer_gateways,omitempty"`
	SecurityGroups   *ResourceQuery `json:"security_groups,omitempty"`
	Subnets          *ResourceQuery `json:"subnets,omitempty"`
	TransitGateways  *ResourceQuery `json:"transit_gateways,omitempty"`
	PrefixLists      *ResourceQuery `json:"prefix_lists,omitempty"`
	VPCs             *ResourceQuery `json:"vpcs,omitempty"`
	VPNGateways      *ResourceQuery `json:"vpn_gateways,omitempty"`
}

// EC2Query adapts a field picker on EC2Resources to a lookup on Resources,
// for code that holds per-type queries as functions. It returns nil when the
// config has no ec2 section.
func EC2Query(pick func(*EC2Resources) *ResourceQuery) func(*Resources) *ResourceQuery {
	return func(r *Resources) *ResourceQuery {
		if r.EC2 == nil {
			return nil
		}
		return pick(r.EC2)
	}
}

// ResourceQuery selects resources by ID, by filter, or both. AWS ANDs the
// conditions, so a resource must match the IDs and every filter. A query
// with neither selects every resource of its type.
type ResourceQuery struct {
	IDs     []string `json:"ids,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
	// ShowUnconnectedResources overrides Options.ShowUnconnectedResources
	// for this type. It cannot be combined with IDs, which always draw the
	// listed resources.
	ShowUnconnectedResources *bool `json:"show_unconnected_resources,omitempty"`
	// Hide leaves this type's resources out of the diagram. A hidden
	// container's contents are drawn in its own container instead: a hidden
	// subnet's resources sit in the VPC.
	Hide bool `json:"hide,omitempty"`
	// HideChildren leaves out everything inside this type's resources: a
	// VPC's or subnet's contents, or a security group's members.
	HideChildren bool `json:"hide_children,omitempty"`
}

// Filter mirrors the AWS API Filter shape.
type Filter struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Read reads and validates the diagram configuration file. Its options are
// laid over the app-level options at AppConfigPath, when that file exists.
func Read(path string) (*Config, error) {
	return read(path, AppConfigPath)
}

// read is Read with the app-level config at appPath, so tests can point it
// away from the user's own.
func read(path, appPath string) (*Config, error) {
	cfg := &Config{}
	if err := decodeFile(path, cfg); err != nil {
		return nil, err
	}
	app, err := readApp(appPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.Cloud.validate(); err != nil {
		return nil, fmt.Errorf("configuration file %s: %w", path, err)
	}
	if !slices.Contains([]Projection{"", ProjectionNameState2D, ProjectionNameStateIsometric}, cfg.Diagram.Projection) {
		return nil, fmt.Errorf(
			"configuration file %s: projection must be \"2d\" or \"isometric\", not %q",
			path,
			cfg.Diagram.Projection,
		)
	}
	if err := cfg.Options.validate(); err != nil {
		return nil, fmt.Errorf("configuration file %s: %w", path, err)
	}
	cfg.Options = app.override(cfg.Options)
	if cfg.Options.ShowUnconnectedResources == nil {
		hide := false
		cfg.Options.ShowUnconnectedResources = &hide
	}
	if !slices.Contains([]Theme{"", ThemeNameStateLight, ThemeNameStateDark}, cfg.Diagram.Theme) {
		return nil, fmt.Errorf(
			"configuration file %s: theme must be \"light\" or \"dark\", not %q",
			path,
			cfg.Diagram.Theme,
		)
	}
	return cfg, nil
}

// accountID matches a 12-digit AWS account ID.
var accountID = regexp.MustCompile(`^[0-9]{12}$`)

// validate checks that exactly one account or subscription is configured,
// and checks it.
func (c Clouds) validate() error {
	var keys []string
	for key, set := range map[string]bool{
		"aws": c.AWS != nil, "aws_gov": c.AWSGov != nil, "azure": c.Azure != nil, "azure_gov": c.AzureGov != nil,
	} {
		if set {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	switch {
	case len(keys) == 0:
		return errors.New("cloud: no account configured; set one of aws, aws_gov, azure, or azure_gov")
	case len(keys) > 1:
		return fmt.Errorf("cloud: set only one of %s; a diagram covers one account for now", strings.Join(keys, ", "))
	}

	var err error
	switch keys[0] {
	case "aws":
		err = c.AWS.validate()
	case "aws_gov":
		err = c.AWSGov.validate()
	case "azure":
		err = c.Azure.validate()
	case "azure_gov":
		err = c.AzureGov.validate()
	}
	if err != nil {
		return fmt.Errorf("cloud.%s.%w", keys[0], err)
	}
	return nil
}

// validate checks one account. Errors start with the failing key, so the
// caller can prefix the account's path.
func (a *AWSAccount) validate() error {
	if a.Source == "" {
		a.Source = SourceSDK
	}
	if a.Source != SourceSDK && a.Source != SourceSteampipe {
		return fmt.Errorf("source: must be \"sdk\" or \"steampipe\", not %q", a.Source)
	}
	if a.AccountID != "" && !accountID.MatchString(a.AccountID) {
		return fmt.Errorf("account_id: %q is not a 12-digit AWS account ID", a.AccountID)
	}
	if a.Source == SourceSteampipe && a.AccountID == "" {
		return errors.New("account_id: required when source is \"steampipe\"")
	}
	if len(a.Regions) == 0 {
		return errors.New("regions: empty")
	}
	if a.Resources.IncludeAll && a.Source != SourceSteampipe {
		return errors.New("resources.include_all: only supported when source is \"steampipe\"")
	}
	return a.Resources.validate()
}

// validate checks every resource query.
func (r Resources) validate() error {
	queries := map[string]*ResourceQuery{}
	if r.EC2 != nil {
		for name, q := range map[string]*ResourceQuery{
			"instances":         r.EC2.Instances,
			"vpn_connections":   r.EC2.VPNConnections,
			"internet_gateways": r.EC2.InternetGateways,
			"nat_gateways":      r.EC2.NATGateways,
			"vpcs":              r.EC2.VPCs,
			"subnets":           r.EC2.Subnets,
			"security_groups":   r.EC2.SecurityGroups,
			"vpn_gateways":      r.EC2.VPNGateways,
			"transit_gateways":  r.EC2.TransitGateways,
			"customer_gateways": r.EC2.CustomerGateways,
			"prefix_lists":      r.EC2.PrefixLists,
		} {
			queries["ec2."+name] = q
		}
	}
	if r.KMS != nil {
		queries["kms.keys"] = r.KMS.Keys
		if r.KMS.Keys != nil && len(r.KMS.Keys.Filters) > 0 {
			return errors.New("resources.kms.keys: filters are not supported; KMS list calls take none, so use ids")
		}
	}
	if r.SecretsManager != nil {
		queries["secretsmanager.secrets"] = r.SecretsManager.Secrets
	}
	if r.SSM != nil {
		queries["ssm.parameters"] = r.SSM.Parameters
	}
	for _, name := range slices.Sorted(maps.Keys(queries)) {
		q := queries[name]
		if q != nil && len(q.IDs) > 0 && q.ShowUnconnectedResources != nil {
			return fmt.Errorf(
				"resources.%s: ids and show_unconnected_resources cannot be combined; ids always draws the listed resources",
				name,
			)
		}
	}
	return nil
}

// decodeFile decodes the JSON file at path into v, rejecting unknown keys so
// a typo fails loudly instead of producing an empty diagram.
func decodeFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read configuration file: %w", err)
	}
	// A read-only file has nothing to flush, so a close error is moot.
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parse configuration file %s: %w", path, err)
	}
	return nil
}
