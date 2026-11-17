package cloudcraft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	cc "github.com/DataDog/cloudcraft-go"
)

func TestFormatsSet(t *testing.T) {
	var f Formats
	for _, v := range []string{"svg,png", "svg", " PDF , json"} {
		if err := f.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	want := []string{"svg", "png", "pdf", "json"}
	if !slices.Equal(f, want) {
		t.Errorf("formats = %v, want %v", f, want)
	}
}

func TestFormatsSetUnknown(t *testing.T) {
	for _, v := range []string{"gif", "svg,gif", "svg,", "mxGraph"} {
		var f Formats
		err := f.Set(v)
		if err == nil {
			t.Errorf("Set(%q) succeeded, want error", v)
			continue
		}
		if !strings.Contains(err.Error(), "svg, json, png, pdf") {
			t.Errorf("Set(%q) error %q does not list the valid formats", v, err)
		}
	}
}

func TestExportFileName(t *testing.T) {
	tests := []struct{ name, want string }{
		{"AWS - ACS", "AWS - ACS.svg"},
		{"  AWS - ACS \n", "AWS - ACS.svg"},
		{"Prod/DR", "Prod-DR.svg"},
		{`Prod\DR`, "Prod-DR.svg"},
	}
	for _, tt := range tests {
		got, err := exportFileName(tt.name, "svg")
		if err != nil || got != tt.want {
			t.Errorf("exportFileName(%q) = %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
	if _, err := exportFileName(" ", "svg"); err == nil {
		t.Error("exportFileName of a blank name succeeded, want error")
	}
}

// newTestExporter returns an Exporter over api, writing to a temporary
// directory, with no pacing.
func newTestExporter(t *testing.T, api *fakeAPI, overwrite bool) *Exporter {
	t.Helper()
	return &Exporter{api: api, exportDir: t.TempDir(), overwrite: overwrite}
}

func TestBlueprints(t *testing.T) {
	api := &fakeAPI{
		blueprints: []*cc.Blueprint{{ID: "a", Name: "Prod"}, {ID: "b", Name: "Dev"}, {ID: "c", Name: "Prod"}},
	}
	e := newTestExporter(t, api, false)

	all, err := e.Blueprints(context.Background(), "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %d blueprints, %v; want 3", len(all), err)
	}
	prod, err := e.Blueprints(context.Background(), "Prod")
	if err != nil || len(prod) != 2 || prod[0].ID != "a" || prod[1].ID != "c" {
		t.Errorf("Prod: %v, %v; want a and c", ids(prod), err)
	}
	if _, err := e.Blueprints(
		context.Background(),
		"Missing",
	); err == nil ||
		!strings.Contains(err.Error(), "no blueprint named 'Missing'") {
		t.Errorf("Missing: err = %v, want a no-match error", err)
	}

	api.listErr = errors.New("boom")
	if _, err := e.Blueprints(
		context.Background(),
		"",
	); err == nil ||
		!strings.Contains(err.Error(), "list blueprints: boom") {
		t.Errorf("list failure: err = %v, want it wrapped", err)
	}
}

func TestExportJSON(t *testing.T) {
	api := &fakeAPI{
		bodies: map[string]string{"a": `{"id":"a","name":"Prod","readAccess":null,"data":{"grid":"standard"}}`},
	}
	e := newTestExporter(t, api, false)

	path, err := e.Export(context.Background(), &cc.Blueprint{ID: "a", Name: " Prod "}, "json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(e.exportDir, "Prod.json"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	got, _ := os.ReadFile(path)
	// The raw body is kept field for field, null included, and re-indented
	// with two spaces and a trailing newline.
	want := "{\n  \"id\": \"a\",\n  \"name\": \"Prod\",\n  \"readAccess\": null,\n" +
		"  \"data\": {\n    \"grid\": \"standard\"\n  }\n}\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestExportImage(t *testing.T) {
	api := &fakeAPI{images: map[string][]byte{"svg": []byte("<svg/>")}}
	e := newTestExporter(t, api, false)

	path, err := e.Export(context.Background(), &cc.Blueprint{ID: "a", Name: "Prod"}, "svg")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "<svg/>" {
		t.Errorf("file = %q, want the exported bytes", got)
	}
	if !slices.Equal(api.calls, []string{"image a svg"}) {
		t.Errorf("calls = %v", api.calls)
	}
	// nil params would make the SDK force a fixed size.
	if api.params == nil {
		t.Error("ExportImage got nil params, want empty params")
	}
}

func TestExportExistingFile(t *testing.T) {
	api := &fakeAPI{images: map[string][]byte{"png": []byte("new")}}
	bpt := &cc.Blueprint{ID: "a", Name: "Prod"}

	e := newTestExporter(t, api, false)
	existing := filepath.Join(e.exportDir, "Prod.png")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(
		context.Background(),
		bpt,
		"png",
	); err == nil ||
		!strings.Contains(err.Error(), "file already exists") {
		t.Errorf("err = %v, want file already exists", err)
	}
	if len(api.calls) != 0 {
		t.Errorf("calls = %v, want no download for a file that will not be written", api.calls)
	}

	e.overwrite = true
	if _, err := e.Export(context.Background(), bpt, "png"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "new" {
		t.Errorf("file = %q, want it replaced", got)
	}
}

func TestExportBlankName(t *testing.T) {
	api := &fakeAPI{}
	e := newTestExporter(t, api, false)
	if _, err := e.Export(context.Background(), &cc.Blueprint{ID: "a", Name: "  "}, "svg"); err == nil {
		t.Error("blank name: want error")
	}
	if len(api.calls) != 0 {
		t.Errorf("calls = %v, want none", api.calls)
	}
}

func TestExportAll(t *testing.T) {
	api := &fakeAPI{
		blueprints: []*cc.Blueprint{{ID: "a", Name: "Prod"}, {ID: "b", Name: "Dev"}},
		bodies:     map[string]string{"a": `{"id":"a"}`, "b": `{"id":"b"}`},
		images:     map[string][]byte{"svg": []byte("<svg/>")},
	}
	e := newTestExporter(t, api, false)
	// Dev.svg already exists, so that one export fails and the rest go on.
	if err := os.WriteFile(filepath.Join(e.exportDir, "Dev.svg"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	var saved, failed []string
	err := e.ExportAll(context.Background(), "", []string{"json", "svg"}, func(path string, err error) {
		if err != nil {
			failed = append(failed, err.Error())
			return
		}
		saved = append(saved, filepath.Base(path))
	})
	if err == nil || err.Error() != "1 of 4 exports failed" {
		t.Errorf("err = %v, want 1 of 4 exports failed", err)
	}
	if want := []string{"Prod.json", "Prod.svg", "Dev.json"}; !slices.Equal(saved, want) {
		t.Errorf("saved = %v, want %v", saved, want)
	}
	if len(failed) != 1 || !strings.Contains(failed[0], "Dev.svg") {
		t.Errorf("failed = %v, want the Dev.svg error", failed)
	}

	api.listErr = errors.New("boom")
	if err := e.ExportAll(
		context.Background(),
		"",
		[]string{"json"},
		func(string, error) { t.Error("report called after a list failure") },
	); err == nil {
		t.Error("list failure: want error")
	}
}

func TestPace(t *testing.T) {
	e := &Exporter{interval: 20 * time.Millisecond}
	start := time.Now()
	for range 3 {
		e.pace()
	}
	// The first call goes straight through; the next two each wait.
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("3 calls took %v, want at least 40ms", elapsed)
	}
}

func ids(bpts []*cc.Blueprint) []string {
	var out []string
	for _, b := range bpts {
		out = append(out, b.ID)
	}
	return out
}
