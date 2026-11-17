// Command diagram draws diagrams of AWS and Azure resources, as Cloudcraft
// blueprints (AWS only), draw.io files, or both, and exports existing
// Cloudcraft blueprints to local files.
//
// Usage:
//
//	diagram create --config FILE [--name NAME] [--export-path DIR] [--overwrite] [--cloudcraft] [--draw.io]
//	diagram export --export-path DIR --format FORMATS [--name NAME] [--overwrite]
//
// Cloudcraft output and export read the API key from the CLOUDCRAFT_API_KEY
// environment variable. An account or subscription with source "steampipe"
// reads the Steampipe database connection from the standard PG* environment variables.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
	"diagram/internal/platform/aws/aws_sdk"
	"diagram/internal/platform/aws/aws_steampipe"
	"diagram/internal/platform/azure"
	"diagram/internal/platform/azure/azure_steampipe"
	"diagram/internal/render"
	"diagram/internal/render/cloudcraft"
	"diagram/internal/render/drawio"
	"diagram/internal/steampipe"
)

const usage = `Usage: diagram <command> [flags]

Commands:
  create, c    Create a diagram of the specified AWS or Azure resources
  export, e    Export Cloudcraft blueprints as svg, json, png, or pdf files

Run "diagram <command> -h" for command flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a command and returns the process exit code. It is split
// from main so tests can run commands without exiting.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}

	var err error
	switch args[0] {
	case "create", "c":
		err = runCreate(args[1:])
	case "export", "e":
		err = runExportCloudcraft(args[1:])
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// runCreate collects the configured resources and saves them in each
// requested format.
func runCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	var configPath, name, exportPath string
	var overwrite, toCloudcraft, toDrawio bool
	fs.StringVar(&configPath, "config", "", "Path to the diagram configuration file. (required)")
	fs.StringVar(&configPath, "c", "", "Shorthand for --config.")
	fs.StringVar(&name, "name", "", "Override the diagram name in the configuration file.")
	fs.StringVar(&name, "n", "", "Shorthand for --name.")
	fs.StringVar(
		&exportPath,
		"export-path",
		"",
		"Local directory for output files, or a file path whose stem becomes the diagram name. "+
			"Required for --draw.io; for --cloudcraft, also writes the blueprint JSON there.",
	)
	fs.StringVar(&exportPath, "p", "", "Shorthand for --export-path.")
	fs.BoolVar(
		&overwrite,
		"overwrite",
		false,
		"Replace an existing Cloudcraft blueprint or output file of the same name.",
	)
	fs.BoolVar(
		&toCloudcraft,
		"cloudcraft",
		false,
		"Upload a Cloudcraft blueprint. This is the default when no output is chosen.",
	)
	fs.BoolVar(&toDrawio, "draw.io", false, "Write a draw.io file to --export-path.")
	_ = fs.Parse(args)

	// A file path names the diagram after its stem, unless --name is given.
	exportPath, stem := render.SplitExportPath(exportPath)
	name = cmp.Or(name, stem)

	// Build the outputs first: each checks its own requirements, such as the
	// Cloudcraft API key, before collecting, which is slow.
	outputs, err := buildOutputs(exportPath, overwrite, toCloudcraft, toDrawio)
	if err != nil {
		return err
	}

	if configPath == "" {
		return errors.New("--config is required")
	}
	cfg, err := config.Read(configPath)
	if err != nil {
		return err
	}
	if name == "" {
		name = cfg.Diagram.Name
	}
	if name == "" {
		return errors.New("no diagram name: set \"diagram.name\" in the configuration file or pass --name")
	}
	if _, sub := cfg.Subscription(); sub != nil && slices.ContainsFunc(outputs, isCloudcraft) {
		return errors.New("cloudcraft output draws AWS only; pass --draw.io for an Azure subscription")
	}

	ctx := context.Background()
	regions, err := collect(ctx, cfg)
	if err != nil {
		return err
	}
	m := model.Build(regions)

	for _, out := range outputs {
		saved, err := out.Save(ctx, name, cfg, m)
		for _, where := range saved {
			log.Printf("Saved %s", where)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// buildOutputs returns the requested outputs, Cloudcraft when none is
// requested.
func buildOutputs(exportPath string, overwrite, toCloudcraft, toDrawio bool) ([]render.Output, error) {
	if !toCloudcraft && !toDrawio {
		toCloudcraft = true
	}
	var outputs []render.Output
	if toDrawio {
		out, err := drawio.New(exportPath, overwrite)
		if err != nil {
			return nil, fmt.Errorf("--draw.io: %w", err)
		}
		outputs = append(outputs, out)
	}
	if toCloudcraft {
		out, err := cloudcraft.New(exportPath, overwrite)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, out)
	}
	return outputs, nil
}

// isCloudcraft reports whether out uploads a Cloudcraft blueprint.
func isCloudcraft(out render.Output) bool {
	_, ok := out.(*cloudcraft.Output)
	return ok
}

// collect reads the configured account or subscription from its source,
// live AWS calls or Steampipe tables, and has its platform finish
// the records.
func collect(ctx context.Context, cfg *config.Config) ([]*inventory.Region, error) {
	regions, err := read(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, sub := cfg.Subscription(); sub != nil {
		azure.Finish(cfg, regions)
	} else {
		aws.Finish(cfg, regions)
	}
	return regions, nil
}

// steampipeDB is the part of *steampipe.DB that read uses.
type steampipeDB interface {
	steampipe.Source
	Close(ctx context.Context) error
}

// openSteampipe opens the Steampipe database connection. Tests replace it with a fake, so
// they run a create without a database.
var openSteampipe = func(ctx context.Context) (steampipeDB, error) {
	return steampipe.Connect(ctx)
}

// read reads the configured account or subscription from its source.
// Azure is read only from Steampipe.
func read(ctx context.Context, cfg *config.Config) ([]*inventory.Region, error) {
	_, acct := cfg.Account()
	if acct != nil && acct.Source != config.SourceSteampipe {
		return aws_sdk.Collect(ctx, acct)
	}
	db, err := openSteampipe(ctx)
	if err != nil {
		return nil, err
	}
	// The connection only read, so a close error is moot.
	defer func() { _ = db.Close(ctx) }()
	if acct != nil {
		return aws_steampipe.Collect(ctx, db, acct)
	}
	_, sub := cfg.Subscription()
	return azure_steampipe.Collect(ctx, db, sub)
}

// exporter is the part of *cloudcraft.Exporter that runExportCloudcraft uses.
type exporter interface {
	ExportAll(ctx context.Context, name string, formats []string, report func(path string, err error)) error
}

// newExporter builds the Cloudcraft exporter. Tests replace it with a fake,
// so they run an export without the Cloudcraft API.
var newExporter = func(exportDir string, overwrite bool) (exporter, error) {
	return cloudcraft.NewExporter(exportDir, overwrite)
}

// runExportCloudcraft saves every requested format of each selected blueprint. A
// failed file does not stop the others, so one bad blueprint cannot block an
// account-wide export.
func runExportCloudcraft(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	var name, exportPath string
	var overwrite bool
	var formats cloudcraft.Formats
	fs.StringVar(&name, "name", "", "Export only the blueprint with this exact name.")
	fs.StringVar(&name, "n", "", "Shorthand for --name.")
	fs.StringVar(&exportPath, "export-path", "", "Local directory for output files. (required)")
	fs.StringVar(&exportPath, "p", "", "Shorthand for --export-path.")
	fs.BoolVar(&overwrite, "overwrite", false, "Replace existing output files.")
	fs.Var(&formats, "format", "Formats to export: "+strings.Join(cloudcraft.ExportFormats, ", ")+
		". Takes a comma-separated list or repeats. (required)")
	_ = fs.Parse(args)

	if exportPath == "" {
		return errors.New("--export-path is required")
	}
	if len(formats) == 0 {
		return errors.New("--format is required")
	}
	exp, err := newExporter(exportPath, overwrite)
	if err != nil {
		return err
	}

	return exp.ExportAll(context.Background(), name, formats, func(path string, err error) {
		if err != nil {
			log.Printf("Failed: %v", err)
			return
		}
		log.Printf("Saved %s", path)
	})
}
