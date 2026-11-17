package config

import (
	"os"
	"path/filepath"
)

// ConfigDir holds the app-level configuration: $XDG_CONFIG_HOME/diagramming
// when XDG_CONFIG_HOME is an absolute path, so a user who set it is respected
// on every OS, else the language's default, os.UserConfigDir()/diagramming
// (~/.config on Linux, ~/Library/Application Support on macOS). Empty when
// neither is available.
var ConfigDir = configDir(os.Getenv("XDG_CONFIG_HOME"), os.UserConfigDir)

// AppConfigPath is the app-level configuration: the options every diagram
// starts from. It is optional, and empty when there is no config directory.
var AppConfigPath = appConfigPath(ConfigDir)

func appConfigPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// configDir picks the config directory from XDG_CONFIG_HOME and the OS
// default. A relative XDG_CONFIG_HOME is ignored, as the XDG spec requires.
func configDir(xdg string, osDefault func() (string, error)) string {
	if filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "diagramming")
	}
	dir, err := osDefault()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "diagramming")
}
