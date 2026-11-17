package cloudcraft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	cc "github.com/DataDog/cloudcraft-go"

	"diagram/internal/render"
)

// ExportFormats lists the formats Exporter can write, in the order help text
// shows them.
var ExportFormats = []string{"svg", "json", "png", "pdf"}

// requestInterval spaces API calls to stay under Cloudcraft's rate limit of
// 20 requests per 10 seconds. The SDK retries a 429, but pacing avoids
// spending retries on a long export.
const requestInterval = 500 * time.Millisecond

// Formats is a flag.Value that collects export formats. It accepts both a
// comma-separated list and repeated flags, and ignores duplicates.
type Formats []string

// String returns the formats as a comma-separated list.
func (f *Formats) String() string {
	return strings.Join(*f, ",")
}

// Set adds each comma-separated format in value, rejecting unknown ones.
func (f *Formats) Set(value string) error {
	for _, v := range strings.Split(value, ",") {
		v = strings.ToLower(strings.TrimSpace(v))
		if !slices.Contains(ExportFormats, v) {
			return fmt.Errorf("unknown format %q; valid formats are %s", v, strings.Join(ExportFormats, ", "))
		}
		if !slices.Contains(*f, v) {
			*f = append(*f, v)
		}
	}
	return nil
}

// Exporter downloads existing Cloudcraft blueprints to local files.
type Exporter struct {
	api       blueprintAPI
	exportDir string
	overwrite bool
	// interval is the minimum time between API calls; tests set it to 0.
	interval time.Duration
	last     time.Time
}

// NewExporter builds an Exporter from the CLOUDCRAFT_* environment
// variables. Like New, it fails when CLOUDCRAFT_API_KEY is not set. overwrite
// replaces existing files.
func NewExporter(exportDir string, overwrite bool) (*Exporter, error) {
	client, err := newClient()
	if err != nil {
		return nil, err
	}
	return &Exporter{api: client.Blueprint, exportDir: exportDir, overwrite: overwrite, interval: requestInterval}, nil
}

// ExportAll exports every format of each blueprint Blueprints selects for
// name, calling report once per file with its path or error. A failed file
// does not stop the rest; ExportAll returns an error at the end if any
// failed, or straight away if the blueprints cannot be listed.
func (e *Exporter) ExportAll(
	ctx context.Context,
	name string,
	formats []string,
	report func(path string, err error),
) error {
	blueprints, err := e.Blueprints(ctx, name)
	if err != nil {
		return err
	}
	failed := 0
	for _, bpt := range blueprints {
		for _, format := range formats {
			path, err := e.Export(ctx, bpt, format)
			if err != nil {
				failed++
			}
			report(path, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d exports failed", failed, len(blueprints)*len(formats))
	}
	return nil
}

// Blueprints lists the account's blueprints, or only those named name when it
// is not empty. Naming a blueprint that does not exist is an error.
func (e *Exporter) Blueprints(ctx context.Context, name string) ([]*cc.Blueprint, error) {
	e.pace()
	all, _, err := e.api.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list blueprints: %w", err)
	}
	if name == "" {
		return all, nil
	}
	var matched []*cc.Blueprint
	for _, bpt := range all {
		if bpt.Name == name {
			matched = append(matched, bpt)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no blueprint named '%s'", name)
	}
	return matched, nil
}

// Export writes bpt in format to the export directory and returns the file
// path.
func (e *Exporter) Export(ctx context.Context, bpt *cc.Blueprint, format string) (string, error) {
	fileName, err := exportFileName(bpt.Name, format)
	if err != nil {
		return "", fmt.Errorf("blueprint %s: %w", bpt.ID, err)
	}
	// Check before downloading so an existing file does not cost an API call.
	if !e.overwrite {
		path := filepath.Join(e.exportDir, fileName)
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("file already exists: %s", path)
		}
	}

	var data []byte
	if format == "json" {
		data, err = e.blueprintJSON(ctx, bpt.ID)
	} else {
		e.pace()
		// Empty params leave size and styling to the API's defaults, as the
		// Python export did; nil would make the SDK force 1920x1080.
		data, _, err = e.api.ExportImage(ctx, bpt.ID, format, &cc.ImageExportParams{})
	}
	if err != nil {
		return "", fmt.Errorf("export '%s' as %s: %w", bpt.Name, format, err)
	}
	return render.WriteFile(e.exportDir, fileName, data, e.overwrite)
}

// blueprintJSON returns the blueprint as the API sent it, indented. It uses
// the raw body because decoding into cc.Blueprint would drop fields the SDK
// does not model and the null fields earlier exports kept.
func (e *Exporter) blueprintJSON(ctx context.Context, id string) ([]byte, error) {
	e.pace()
	_, resp, err := e.api.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, resp.Body, "", "  "); err != nil {
		return nil, fmt.Errorf("indent blueprint JSON: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// pace waits until requestInterval has passed since the previous API call.
func (e *Exporter) pace() {
	if wait := time.Until(e.last.Add(e.interval)); wait > 0 {
		time.Sleep(wait)
	}
	e.last = time.Now()
}

// exportFileName returns the file name for a blueprint export. Blueprint
// names can carry stray whitespace, and a path separator would put the file
// outside the export directory, so both are cleaned up.
func exportFileName(name, format string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("blueprint has no name")
	}
	name = strings.NewReplacer("/", "-", `\`, "-", "\x00", "").Replace(name)
	return name + "." + format, nil
}
