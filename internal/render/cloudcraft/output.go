package cloudcraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	cc "github.com/DataDog/cloudcraft-go"

	"diagram/internal/config"
	"diagram/internal/model"
	"diagram/internal/render"
)

// Output uploads diagrams as Cloudcraft blueprints, and also writes the
// blueprint JSON locally when it has an export directory.
type Output struct {
	api       blueprintAPI
	exportDir string
	overwrite bool
}

// blueprintAPI is the part of the SDK's blueprint service this package uses.
// *cc.BlueprintService implements it; tests swap in a fake so they exercise
// this package's logic without the SDK or the network.
type blueprintAPI interface {
	List(ctx context.Context) ([]*cc.Blueprint, *cc.Response, error)
	Get(ctx context.Context, id string) (*cc.Blueprint, *cc.Response, error)
	Create(ctx context.Context, blueprint *cc.Blueprint) (*cc.Blueprint, *cc.Response, error)
	Update(ctx context.Context, blueprint *cc.Blueprint, etag string) (*cc.Response, error)
	ExportImage(ctx context.Context, id, format string, params *cc.ImageExportParams) ([]byte, *cc.Response, error)
}

var _ render.Output = (*Output)(nil)

// New builds an Output from the CLOUDCRAFT_* environment variables. It fails
// when CLOUDCRAFT_API_KEY is not set, so a caller can check before doing any
// slow work. exportDir may be empty. overwrite replaces an existing blueprint
// or file of the same name.
func New(exportDir string, overwrite bool) (*Output, error) {
	client, err := newClient()
	if err != nil {
		return nil, err
	}
	return &Output{api: client.Blueprint, exportDir: exportDir, overwrite: overwrite}, nil
}

// newClient builds a Cloudcraft client from the CLOUDCRAFT_* environment
// variables. It checks the API key itself because the SDK's own error for a
// missing key does not name the variable to set.
func newClient() (*cc.Client, error) {
	if os.Getenv(cc.EnvAPIKey) == "" {
		return nil, errors.New(cc.EnvAPIKey + " is not set")
	}
	client, err := cc.NewClient(cc.NewConfigFromEnv())
	if err != nil {
		return nil, fmt.Errorf("create Cloudcraft client: %w", err)
	}
	return client, nil
}

// Save renders the blueprint, writes it locally when there is an export
// directory, and uploads it. It returns the file path, if any, then the
// blueprint URL.
func (o *Output) Save(ctx context.Context, name string, cfg *config.Config, m *model.Diagram) ([]string, error) {
	data := Build(name, cfg, m)
	var saved []string
	if o.exportDir != "" {
		out, err := Encode(data)
		if err != nil {
			return nil, err
		}
		path, err := render.WriteFile(o.exportDir, FileName(name), out, o.overwrite)
		if err != nil {
			return nil, err
		}
		saved = append(saved, path)
	}
	url, err := o.publish(ctx, data)
	if err != nil {
		return saved, err
	}
	return append(saved, url), nil
}

// FileName returns the export file name for a diagram. Some diagram names
// carry stray whitespace, which does not belong in a file name.
func FileName(name string) string {
	return strings.TrimSpace(name) + ".json"
}

// Encode returns blueprint data as the JSON document Cloudcraft's API and
// exports use: {"data": ...}.
func Encode(data *cc.BlueprintData) ([]byte, error) {
	out, err := json.MarshalIndent(map[string]any{"data": data}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode blueprint: %w", err)
	}
	return out, nil
}

// publish creates the blueprint, or replaces the one with the same name when
// overwrite is set, and returns its URL.
func (o *Output) publish(ctx context.Context, data *cc.BlueprintData) (string, error) {
	existing, err := o.find(ctx, data.Name)
	if err != nil {
		return "", err
	}

	id := ""
	if existing != nil {
		if !o.overwrite {
			return "", fmt.Errorf("a blueprint with the name '%s' already exists", data.Name)
		}
		if _, err := o.api.Update(ctx, &cc.Blueprint{ID: existing.ID, Data: data}, ""); err != nil {
			return "", fmt.Errorf("update blueprint: %w", err)
		}
		id = existing.ID
	} else {
		created, _, err := o.api.Create(ctx, &cc.Blueprint{Data: data})
		if err != nil {
			return "", fmt.Errorf("create blueprint: %w", err)
		}
		id = created.ID
	}
	return "https://app.cloudcraft.co/blueprint/" + id, nil
}

// find returns the blueprint with the given name, or nil if there is none.
func (o *Output) find(ctx context.Context, name string) (*cc.Blueprint, error) {
	blueprints, _, err := o.api.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list blueprints: %w", err)
	}
	for _, bpt := range blueprints {
		if bpt.Name == name {
			return bpt, nil
		}
	}
	return nil, nil
}
