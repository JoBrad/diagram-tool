package cloudcraft

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cc "github.com/DataDog/cloudcraft-go"

	"diagram/internal/config"
	"diagram/internal/model"
)

func TestNewClientNeedsKey(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, "")
	if _, err := newClient(); err == nil || err.Error() != cc.EnvAPIKey+" is not set" {
		t.Errorf("err = %v, want one naming %s", err, cc.EnvAPIKey)
	}
}

func TestPublish(t *testing.T) {
	data := &cc.BlueprintData{Name: "Prod"}
	tests := []struct {
		name      string
		existing  []*cc.Blueprint
		overwrite bool
		wantCalls []string
		wantURL   string
		wantErr   string
	}{
		{
			"new blueprint",
			[]*cc.Blueprint{{ID: "x", Name: "Dev"}},
			false,
			[]string{"list", "create"},
			"https://app.cloudcraft.co/blueprint/new-id",
			"",
		},
		{
			"name taken",
			[]*cc.Blueprint{{ID: "old", Name: "Prod"}},
			false,
			[]string{"list"},
			"",
			"a blueprint with the name 'Prod' already exists",
		},
		{
			"overwrite",
			[]*cc.Blueprint{{ID: "old", Name: "Prod"}},
			true,
			[]string{"list", "update old"},
			"https://app.cloudcraft.co/blueprint/old",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{blueprints: tt.existing, createdID: "new-id"}
			o := &Output{api: api, overwrite: tt.overwrite}
			url, err := o.publish(context.Background(), data)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil || url != tt.wantURL {
				t.Errorf("publish = %q, %v, want %q", url, err, tt.wantURL)
			}
			if !slices.Equal(api.calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", api.calls, tt.wantCalls)
			}
			// Whichever call ran must carry the rendered data.
			if api.created != nil && api.created.Data != data || api.updated != nil && api.updated.Data != data {
				t.Error("uploaded blueprint does not carry the rendered data")
			}
		})
	}
}

func TestSave(t *testing.T) {
	cfg := &config.Config{}
	m := &model.Diagram{Regions: []*model.Region{{Name: "us-east-1"}}}

	dir := t.TempDir()
	o := &Output{api: &fakeAPI{createdID: "new-id"}, exportDir: dir}
	saved, err := o.Save(context.Background(), " Prod ", cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Prod.json")
	if want := []string{path, "https://app.cloudcraft.co/blueprint/new-id"}; !slices.Equal(saved, want) {
		t.Errorf("saved = %v, want %v", saved, want)
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "{\n  \"data\": {") {
		t.Errorf("export file does not hold the {\"data\": ...} document: %.40q", got)
	}

	// Without an export directory, Save only uploads.
	o = &Output{api: &fakeAPI{createdID: "new-id"}}
	if saved, err := o.Save(context.Background(), "Prod", cfg, m); err != nil || len(saved) != 1 {
		t.Errorf("no export dir: saved = %v, %v, want only the URL", saved, err)
	}

	// A failed upload still reports the file that was written.
	o = &Output{api: &fakeAPI{blueprints: []*cc.Blueprint{{ID: "old", Name: "Taken"}}}, exportDir: t.TempDir()}
	saved, err = o.Save(context.Background(), "Taken", cfg, m)
	if err == nil || len(saved) != 1 {
		t.Errorf("upload failure: saved = %v, %v, want the file path and an error", saved, err)
	}
}

// testKey has the 44-character length the SDK requires of an API key.
var testKey = strings.Repeat("k", 44)

func TestNew(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, testKey)
	o, err := New("out", true)
	if err != nil {
		t.Fatal(err)
	}
	if o.api == nil || o.exportDir != "out" || !o.overwrite {
		t.Errorf("New = %+v, want an API client, export dir \"out\", and overwrite", o)
	}

	t.Setenv(cc.EnvAPIKey, "")
	if _, err := New("out", false); err == nil {
		t.Error("New without an API key: want an error")
	}
}

func TestNewClientRejectsMalformedKey(t *testing.T) {
	t.Setenv(cc.EnvAPIKey, "short")
	if _, err := newClient(); err == nil || !strings.HasPrefix(err.Error(), "create Cloudcraft client: ") {
		t.Errorf("err = %v, want the SDK's validation error, wrapped", err)
	}
}

func TestPublishAPIErrors(t *testing.T) {
	data := &cc.BlueprintData{Name: "Prod"}
	boom := errors.New("boom")
	taken := []*cc.Blueprint{{ID: "old", Name: "Prod"}}
	tests := []struct {
		name    string
		api     *fakeAPI
		wantErr string
	}{
		{"list", &fakeAPI{listErr: boom}, "list blueprints: boom"},
		{"create", &fakeAPI{createErr: boom}, "create blueprint: boom"},
		{"update", &fakeAPI{blueprints: taken, updateErr: boom}, "update blueprint: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Output{api: tt.api, overwrite: true}
			url, err := o.publish(context.Background(), data)
			if err == nil || err.Error() != tt.wantErr || !errors.Is(err, boom) {
				t.Errorf("err = %v, want %q wrapping the API error", err, tt.wantErr)
			}
			if url != "" {
				t.Errorf("url = %q on failure, want none", url)
			}
		})
	}
}

func TestSaveWriteFailure(t *testing.T) {
	// A regular file where the export directory should be fails the write,
	// which must stop Save before it uploads anything.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{createdID: "new-id"}
	o := &Output{api: api, exportDir: filepath.Join(file, "out")}
	saved, err := o.Save(context.Background(), "Prod", &config.Config{}, &model.Diagram{})
	if err == nil || saved != nil {
		t.Errorf("Save = %v, %v, want no results and an error", saved, err)
	}
	if len(api.calls) != 0 {
		t.Errorf("calls = %v after a failed write, want none", api.calls)
	}
}

func TestEncodeError(t *testing.T) {
	data := &cc.BlueprintData{Nodes: []map[string]any{{"x": math.NaN()}}}
	if _, err := Encode(data); err == nil || !strings.HasPrefix(err.Error(), "encode blueprint: ") {
		t.Errorf("err = %v, want an encode error", err)
	}
}
