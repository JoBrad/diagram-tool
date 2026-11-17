// Package render defines what every output format provides, so the command
// can treat them alike. Each format lives in its own subpackage.
package render

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"diagram/internal/config"
	"diagram/internal/model"
)

// Output renders a diagram in one format and stores it wherever that format
// goes: uploaded, written to a file, or both.
type Output interface {
	// Save renders m and stores it. It returns where the result went, such as
	// a URL or a file path, one entry per place.
	Save(ctx context.Context, name string, cfg *config.Config, m *model.Diagram) ([]string, error)
}

// WriteFile writes data to dir/fileName, refusing to replace an existing
// file unless overwrite is set, and returns the file path.
func WriteFile(dir, fileName string, data []byte, overwrite bool) (string, error) {
	path := filepath.Join(dir, fileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		if !overwrite {
			return "", fmt.Errorf("file already exists: %s", path)
		}
		log.Printf("Replacing existing file: %s", path)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// SplitExportPath reads an export path that may name a file rather than a
// directory. A path to an existing file, or a path that does not exist and
// has an extension, is a file: it returns the file's directory and its stem
// (the name without the extension). Any other path is a directory, returned
// with an empty stem.
func SplitExportPath(path string) (dir, stem string) {
	info, err := os.Stat(path)
	isFile := (err == nil && info.Mode().IsRegular()) || (os.IsNotExist(err) && filepath.Ext(path) != "")
	if !isFile {
		return path, ""
	}
	base := filepath.Base(path)
	return filepath.Dir(path), strings.TrimSuffix(base, filepath.Ext(base))
}
