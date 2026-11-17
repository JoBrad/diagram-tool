package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
)

// Options are the settings every diagram starts from. The app-level config
// (AppConfigPath) sets them for all diagrams, and a diagram's own options
// override them.
type Options struct {
	// ShowUnconnectedResources is the default for every resource type:
	// false leaves out resources with no connection to anything else in the
	// diagram. A resource type's own setting overrides it. Defaults to false.
	ShowUnconnectedResources *bool  `json:"show_unconnected_resources,omitempty"`
	Colors                   Colors `json:"colors"`
}

// Colors is the diagram's color scheme. Each tier lists one or more colors,
// rotated across that tier's subnets so neighbors differ; an empty list keeps
// the default. Resources colors single resources by ID, such as a subnet, a
// VPC, or a security group, and wins over the tier.
type Colors struct {
	Public    []string          `json:"public,omitempty"`
	Private   []string          `json:"private,omitempty"`
	Isolated  []string          `json:"isolated,omitempty"`
	Resources map[string]string `json:"resources,omitempty"`
}

// AppConfig is the app-level configuration file.
type AppConfig struct {
	Options Options `json:"options"`
}

// readApp reads the app-level options at path. A missing file, or an empty
// path, gives empty options: the app-level config is optional.
func readApp(path string) (Options, error) {
	if path == "" {
		return Options{}, nil
	}
	app := &AppConfig{}
	if err := decodeFile(path, app); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Options{}, nil
		}
		return Options{}, err
	}
	if err := app.Options.validate(); err != nil {
		return Options{}, fmt.Errorf("app configuration file %s: %w", path, err)
	}
	return app.Options, nil
}

// override returns o with every setting over sets on top. A tier's colors are
// replaced as a list; resource colors merge by ID, with over winning.
func (o Options) override(over Options) Options {
	out := o
	if over.ShowUnconnectedResources != nil {
		out.ShowUnconnectedResources = over.ShowUnconnectedResources
	}
	for _, tier := range []struct {
		dst *[]string
		src []string
	}{
		{&out.Colors.Public, over.Colors.Public},
		{&out.Colors.Private, over.Colors.Private},
		{&out.Colors.Isolated, over.Colors.Isolated},
	} {
		if len(tier.src) > 0 {
			*tier.dst = tier.src
		}
	}
	if len(o.Colors.Resources)+len(over.Colors.Resources) > 0 {
		out.Colors.Resources = maps.Clone(o.Colors.Resources)
		if out.Colors.Resources == nil {
			out.Colors.Resources = map[string]string{}
		}
		maps.Copy(out.Colors.Resources, over.Colors.Resources)
	}
	return out
}

// validate checks the options' colors.
func (o Options) validate() error {
	return o.Colors.validate()
}

// hexColor matches a #RRGGBB color, the form draw.io styles take.
var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// validate checks that every color is #RRGGBB.
func (c Colors) validate() error {
	for name, list := range map[string][]string{"public": c.Public, "private": c.Private, "isolated": c.Isolated} {
		for _, color := range list {
			if !hexColor.MatchString(color) {
				return fmt.Errorf("options.colors.%s: %q is not a #RRGGBB color", name, color)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.Resources)) {
		if !hexColor.MatchString(c.Resources[id]) {
			return fmt.Errorf("options.colors.resources.%s: %q is not a #RRGGBB color", id, c.Resources[id])
		}
	}
	return nil
}
