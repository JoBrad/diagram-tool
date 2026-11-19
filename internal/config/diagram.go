// Package config reads the configuration: the optional app-level file in
// ConfigDir, and the diagram configuration file, whose options override it.
package config

import (
	"bytes"
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

// Resources holds the AWS resource queries, as service -> type -> query:
// "ec2" -> "instances" -> query. The file keeps the service and type keys as
// nested objects beside include_all. Config reads no platform, so it does not
// know which keys exist; the AWS platform's registry checks them before any
// AWS call.
type Resources struct {
	// IncludeAll reads every resource type the source can supply, without
	// naming each one. Per-type queries still narrow or override their type.
	// Only the steampipe source supports it.
	IncludeAll bool

	// Services maps a service key to its types' queries. A type the file
	// names with an empty object has a non-nil, empty query.
	Services map[string]map[string]*ResourceQuery
}

// Query returns the config's query for one resource type, or nil when the
// file does not name it.
func (r *Resources) Query(service, typ string) *ResourceQuery {
	return r.Services[service][typ]
}

// UnmarshalJSON reads include_all and the service sections. Every query is
// decoded strictly, so a misspelled query field still fails the read.
func (r *Resources) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = Resources{}
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		if key == "include_all" {
			if err := json.Unmarshal(raw[key], &r.IncludeAll); err != nil {
				return fmt.Errorf("resources.include_all: %w", err)
			}
			continue
		}
		var types map[string]json.RawMessage
		if err := json.Unmarshal(raw[key], &types); err != nil {
			return fmt.Errorf("resources.%s: %w", key, err)
		}
		queries := map[string]*ResourceQuery{}
		for _, typ := range slices.Sorted(maps.Keys(types)) {
			q := &ResourceQuery{}
			dec := json.NewDecoder(bytes.NewReader(types[typ]))
			dec.DisallowUnknownFields()
			if err := dec.Decode(q); err != nil {
				return fmt.Errorf("resources.%s.%s: %w", key, typ, err)
			}
			queries[typ] = q
		}
		if r.Services == nil {
			r.Services = map[string]map[string]*ResourceQuery{}
		}
		r.Services[key] = queries
	}
	return nil
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

// validate checks every resource query. Which keys exist is the platform's
// check, not this one.
func (r Resources) validate() error {
	for _, service := range slices.Sorted(maps.Keys(r.Services)) {
		types := r.Services[service]
		for _, typ := range slices.Sorted(maps.Keys(types)) {
			q := types[typ]
			if q != nil && len(q.IDs) > 0 && q.ShowUnconnectedResources != nil {
				return fmt.Errorf(
					"resources.%s.%s: ids and show_unconnected_resources cannot be combined; "+
						"ids always draws the listed resources",
					service, typ,
				)
			}
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
