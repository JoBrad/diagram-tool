package drawio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// macAppPath is where the draw.io desktop app installs on macOS.
const macAppPath = "/Applications/draw.io.app/Contents/MacOS/draw.io"

// routeTimeout bounds one routing run. A large diagram routes in seconds.
const routeTimeout = 2 * time.Minute

// router routes a diagram's edges around shapes with the draw.io desktop
// app's obstacle-avoiding router (libavoid), the same one as Arrange >
// Layout > Orthogonal Routing. The layout cannot do this itself: D2 routes
// every link that touches a grid as a straight line.
type router struct {
	// find returns the desktop app's path, or "" when it is not installed.
	find func() string
	// run runs the app with args.
	run func(ctx context.Context, bin string, args ...string) error
}

var defaultRouter = router{find: findDrawio, run: runDrawio}

// route returns the diagram with its edges routed. Routing is best effort:
// when the app is missing or fails, it logs why and returns xml unchanged,
// whose edges draw.io still routes once a connected shape moves.
func (r router) route(ctx context.Context, xml []byte) []byte {
	bin := r.find()
	if bin == "" {
		log.Printf("WARN draw.io desktop app not found, so edges are not routed around shapes; " +
			"set DRAWIO_BIN or use Arrange > Layout > Orthogonal Routing in draw.io")
		return xml
	}
	routed, err := r.routeWith(ctx, bin, xml)
	if err != nil {
		log.Printf("WARN draw.io edge routing skipped: %v", err)
		return xml
	}
	log.Printf("Routed edges with %s", bin)
	return routed
}

// routeWith runs the app's export with the libavoid layout on a copy of xml.
func (r router) routeWith(ctx context.Context, bin string, xml []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "drawio-route-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	in, out := filepath.Join(dir, "in.drawio"), filepath.Join(dir, "out.drawio")
	if err := os.WriteFile(in, xml, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	// -u keeps the XML uncompressed, so the file stays readable and diffable.
	if err := r.run(ctx, bin, "-x", "-f", "xml", "-u", "--layout", "orthogonalEdge", "-o", out, in); err != nil {
		return nil, err
	}
	routed, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read routed diagram: %w", err)
	}
	if !bytes.Contains(routed, []byte("<mxfile")) {
		return nil, errors.New("routed diagram is not a draw.io file")
	}
	return routed, nil
}

// findDrawio returns the draw.io desktop app: DRAWIO_BIN when set, else the
// first of drawio or draw.io on PATH, else the macOS install location.
func findDrawio() string {
	if bin := os.Getenv("DRAWIO_BIN"); bin != "" {
		return bin
	}
	for _, name := range []string{"drawio", "draw.io"} {
		if bin, err := exec.LookPath(name); err == nil {
			return bin
		}
	}
	if _, err := os.Stat(macAppPath); err == nil {
		return macAppPath
	}
	return ""
}

// runDrawio runs the app, reporting its output when it fails.
func runDrawio(ctx context.Context, bin string, args ...string) error {
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", bin, err, bytes.TrimSpace(out))
	}
	return nil
}
