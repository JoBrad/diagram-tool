package drawio

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// fakeApp stands in for the draw.io desktop app. It writes routed to the
// -o path, or fails with err.
type fakeApp struct {
	routed string
	err    error
	args   []string
}

func (f *fakeApp) run(_ context.Context, _ string, args ...string) error {
	f.args = args
	if f.err != nil {
		return f.err
	}
	if f.routed == "" {
		return nil
	}
	out := args[slices.Index(args, "-o")+1]
	return os.WriteFile(out, []byte(f.routed), 0o600)
}

func TestRoute(t *testing.T) {
	const in = `<mxfile host="cloudcraft"></mxfile>`
	const routed = `<mxfile host="Electron"><routed/></mxfile>`
	found := func() string { return "/fake/draw.io" }
	tests := []struct {
		name string
		find func() string
		app  *fakeApp
		want string
	}{
		{"routed", found, &fakeApp{routed: routed}, routed},
		{"app missing", func() string { return "" }, &fakeApp{routed: routed}, in},
		{"app fails", found, &fakeApp{err: errors.New("boom")}, in},
		{"no output", found, &fakeApp{}, in},
		{"not draw.io", found, &fakeApp{routed: "error page"}, in},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := router{find: tt.find, run: tt.app.run}
			if got := string(r.route(context.Background(), []byte(in))); got != tt.want {
				t.Errorf("route = %q, want %q", got, tt.want)
			}
		})
	}

	app := &fakeApp{routed: routed}
	router{find: found, run: app.run}.route(context.Background(), []byte(in))
	for _, want := range []string{"-x", "xml", "-u", "--layout", "orthogonalEdge"} {
		if !slices.Contains(app.args, want) {
			t.Errorf("args %v lack %q", app.args, want)
		}
	}
}

func TestSaveRoutes(t *testing.T) {
	const routed = `<mxfile host="Electron"><routed/></mxfile>`
	o := &Output{
		dir:    t.TempDir(),
		router: router{find: func() string { return "/fake/draw.io" }, run: (&fakeApp{routed: routed}).run},
	}
	paths, err := o.Save(context.Background(), "test", nil, testModel())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != routed {
		t.Errorf("saved %q, want the routed diagram", got)
	}
}

func TestFindDrawio(t *testing.T) {
	t.Setenv("DRAWIO_BIN", "/custom/draw.io")
	if got := findDrawio(); got != "/custom/draw.io" {
		t.Errorf("findDrawio = %q, want DRAWIO_BIN", got)
	}
	t.Setenv("DRAWIO_BIN", "")
	t.Setenv("PATH", t.TempDir())
	want := ""
	if _, err := os.Stat(macAppPath); err == nil {
		want = macAppPath
	}
	if got := findDrawio(); got != want {
		t.Errorf("findDrawio = %q, want %q", got, want)
	}
}

func TestRunDrawio(t *testing.T) {
	if err := runDrawio(context.Background(), "sh", "-c", "exit 0"); err != nil {
		t.Errorf("runDrawio succeeded command = %v", err)
	}
	err := runDrawio(context.Background(), "sh", "-c", "echo boom; exit 3")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want it to carry the command's output", err)
	}
}

func TestNew(t *testing.T) {
	if _, err := New("", false); err == nil {
		t.Error("New without a directory succeeded")
	}
	o, err := New(t.TempDir(), true)
	if err != nil || !o.overwrite || o.router.find == nil || o.router.run == nil {
		t.Errorf("New = %+v, %v; want an overwriting output with the default router", o, err)
	}
}

func TestSaveExisting(t *testing.T) {
	skip := router{find: func() string { return "" }}
	o := &Output{dir: t.TempDir(), router: skip}
	if _, err := o.Save(context.Background(), "test", nil, testModel()); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Save(context.Background(), "test", nil, testModel()); err == nil {
		t.Error("second Save without overwrite succeeded")
	}
}
