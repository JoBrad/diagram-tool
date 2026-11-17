package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const diagramBody = `{"cloud": {"aws": {"regions": ["us-east-1"]}}`

func TestReadApp(t *testing.T) {
	if o, err := readApp(""); err != nil || !reflect.DeepEqual(o, Options{}) {
		t.Errorf("readApp of an empty path = %+v, %v; want empty options", o, err)
	}
	if o, err := readApp(filepath.Join(t.TempDir(), "absent.json")); err != nil || !reflect.DeepEqual(o, Options{}) {
		t.Errorf("missing file = %+v, %v; want empty options", o, err)
	}
	for body, want := range map[string]string{
		`{`:             "parse configuration file",
		`{"cloud": {}}`: `unknown field "cloud"`,
		`{"options": {"colors": {"public": ["green"]}}}`: `app configuration file`,
	} {
		if _, err := readApp(write(t, body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("readApp(%s) err = %v, want %q", body, err, want)
		}
	}
}

func TestReadWithApp(t *testing.T) {
	app := write(t, `{"options": {"show_unconnected_resources": true, "colors": {
		"public": ["#111111"], "private": ["#222222"], "resources": {"subnet-1": "#333333", "sg-1": "#444444"}}}}`)
	tests := []struct {
		name     string
		diagram  string
		show     bool
		public   []string
		private  []string
		subnet1  string
		resource int
	}{
		{"app options apply", diagramBody + `}`, true, []string{"#111111"}, []string{"#222222"}, "#333333", 2},
		{
			"diagram options win",
			diagramBody + `, "options": {"show_unconnected_resources": false, "colors": {
				"private": ["#555555", "#666666"], "resources": {"subnet-1": "#777777", "vpc-1": "#888888"}}}}`,
			false, []string{"#111111"}, []string{"#555555", "#666666"}, "#777777", 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := read(write(t, tt.diagram), app)
			if err != nil {
				t.Fatal(err)
			}
			c := cfg.Options.Colors
			if *cfg.Options.ShowUnconnectedResources != tt.show || !reflect.DeepEqual(c.Public, tt.public) ||
				!reflect.DeepEqual(c.Private, tt.private) || c.Resources["subnet-1"] != tt.subnet1 ||
				len(c.Resources) != tt.resource {
				t.Errorf("options = %+v, show %v", c, *cfg.Options.ShowUnconnectedResources)
			}
		})
	}

	// Neither file sets show_unconnected_resources: it defaults to false.
	cfg, err := read(write(t, diagramBody+`}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Options.ShowUnconnectedResources == nil || *cfg.Options.ShowUnconnectedResources {
		t.Errorf("show_unconnected_resources = %v, want false by default", cfg.Options.ShowUnconnectedResources)
	}

	if _, err := read(write(t, diagramBody+`}`), write(t, `{"bogus": 1}`)); err == nil {
		t.Error("a bad app config did not fail the read")
	}
}

func TestConfigDir(t *testing.T) {
	osDefault := func() (string, error) { return "/os/default", nil }
	noDefault := func() (string, error) { return "", errors.New("no home") }
	tests := []struct {
		xdg       string
		osDefault func() (string, error)
		want      string
	}{
		{"/xdg", osDefault, "/xdg/diagramming"},
		{"relative/xdg", osDefault, "/os/default/diagramming"}, // ignored, as the XDG spec requires
		{"", osDefault, "/os/default/diagramming"},
		{"", noDefault, ""},
	}
	for _, tt := range tests {
		if got := configDir(tt.xdg, tt.osDefault); got != tt.want {
			t.Errorf("configDir(%q) = %q, want %q", tt.xdg, got, tt.want)
		}
	}
	if appConfigPath("") != "" || appConfigPath("/c") != "/c/config.json" {
		t.Errorf("appConfigPath = %q, %q", appConfigPath(""), appConfigPath("/c"))
	}
}
