package drawio

import (
	"context"
	"errors"
	"strings"

	"diagram/internal/config"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/render"
)

// Output writes diagrams as draw.io files, laid out with ELK, with edges
// routed around shapes by the draw.io desktop app when it is installed.
type Output struct {
	dir       string
	overwrite bool
	router    router
}

var _ render.Output = (*Output)(nil)

// New returns an Output that writes to dir. A draw.io diagram is only a
// file, so dir is required. overwrite replaces an existing file.
func New(dir string, overwrite bool) (*Output, error) {
	if dir == "" {
		return nil, errors.New("draw.io output writes a file, so it needs an export directory")
	}
	return &Output{dir: dir, overwrite: overwrite, router: defaultRouter}, nil
}

// Save renders the diagram and writes it, returning the file path.
func (o *Output) Save(ctx context.Context, name string, cfg *config.Config, m *model.Diagram) ([]string, error) {
	var colors config.Colors
	if cfg != nil {
		colors = cfg.Options.Colors
	}
	out, err := Render(ctx, name, m, layout.ELK{}, colors)
	if err != nil {
		return nil, err
	}
	out = o.router.route(ctx, out)
	path, err := render.WriteFile(o.dir, FileName(name), out, o.overwrite)
	if err != nil {
		return nil, err
	}
	return []string{path}, nil
}

// FileName returns the output file name for a diagram. Some diagram names
// carry stray whitespace, which does not belong in a file name.
func FileName(name string) string {
	return strings.TrimSpace(name) + ".drawio"
}
