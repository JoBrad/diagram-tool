# AGENTS.md

Go CLI that collects AWS resources and draws them as a Cloudcraft blueprint, a draw.io file,
or both, and bulk-exports existing Cloudcraft blueprints. [README.md](README.md) covers usage,
the config format, commands, and the package layout. Read it first; this file adds what the
README does not: invariants, API quirks, and how to verify a change. Design decisions and
their reasons are in [docs/DECISIONS.md](docs/DECISIONS.md).


## Commands

Go and golangci-lint come from devbox. Run from a devbox shell or prefix with `devbox run`:

- `devbox run build`, `devbox run test`, `devbox run lint`
- `devbox run coverage` - thresholds 80% per file, 70% per package ([.testcoverage.yml](.testcoverage.yml))
- `devbox run deadcode`

Before calling a change done: `gofmt -l .` prints nothing, and `go vet ./...`,
`devbox run lint`, and `devbox run test` pass. Lint limits ([.golangci.yml](.golangci.yml)):
120-character lines, functions of 80 lines or 60 statements (tests exempt), files of 500
lines. golines and goimports format the code.

## Documentation

These documents should be considered sources of truth regarding this project.

- [docs/DESIGN.md](docs/DESIGN.md) describes the system as built: components, contracts,
  flows, and design choices. It is maintained by hand; update it in the same change as
  the code it describes.
- [docs/DECISIONS.md](docs/DECISIONS.md) records the reason behind each design decision
  the code reflects. It holds current decisions only; git keeps the history.
- [docs/ROADMAP.md](docs/ROADMAP.md) lists the stages the tool is heading through, with
  links to the projects in `.agents/projects/` that carry each one.
- Changes to layout, shapes, labels, or colors follow the design principles at the top
  of [docs/DECISIONS.md](docs/DECISIONS.md#principles).

## Architecture invariants

A run flows `config` -> source -> platform -> `model` -> `render`. Sources and the platform
hand the model `inventory.Record` values.

- **`model` is tool-neutral and platform-neutral.** It knows nothing about Cloudcraft,
  draw.io, or AWS, and reads no config. The platform's `Finish`
  ([internal/platform/aws/finish.go](internal/platform/aws/finish.go)) resolves selection
  (`hide`, `show_unconnected_resources`) into record fields. `model` applies them once, by
  hiding and pruning, before any renderer runs. Renderers draw whatever the model
  contains.
- **Cloudcraft-only workarounds stay in `internal/render/cloudcraft`:** placeholder nodes in
  empty groups (Cloudcraft does not render an empty group), prefix lists drawn as stand-in
  `sg` groups (Cloudcraft rules can target a group but not a prefix list), labels anchored
  with `mapPos.relTo` so they move with their node.
- **Component IDs are AWS resource IDs** in both renderers, as in Cloudcraft's own scans.
  References (group members, rule targets, edge ends) are plain IDs, and rebuilds reproduce
  the same IDs.
- **Layout differs per renderer.** Cloudcraft keeps its own fixed grid
  (`render/cloudcraft/layout.go`), because Cloudcraft recomputes group boxes and draws edges
  as straight lines, so an engine layout does not carry over. ELK (`internal/layout`, through
  D2's Go library) is used for draw.io only. Do not tune Cloudcraft layout; that was
  deliberately ruled out.
- **Each output implements `render.Output`.** `Save` returns where the result went (file
  paths, blueprint URL). `main.go` only parses flags, builds outputs, and logs; Cloudcraft
  client, upload, and export logic live in `render/cloudcraft`.
- **SDK collectors run concurrently** (`errgroup`, `maxConcurrentCalls = 8`, in
  [aws_sdk/collect.go](internal/platform/aws/aws_sdk/collect.go)). Each collector writes
  only its own field of `collected` (`aws_sdk/records.go`), which is what keeps this
  lock-free; records are built once all collectors finish. Keep that rule when adding one.

## Selection rules

`show_unconnected_resources` resolves per resource type: listed `ids` (never filtered), else
the type's own setting, else the global `options` value, which `config.Read` defaults to
`false`. `config.Read` rejects a query that sets both `ids` and `show_unconnected_resources`.

AWS rules live in `Finish` ([internal/platform/aws/finish.go](internal/platform/aws/finish.go)),
which sets `KeepIf`, `Attached`, and `Prunable` on each record. Pruning
([internal/model/prune.go](internal/model/prune.go)) repeats until nothing changes.
Non-obvious rules:

- A container is connected only by what it holds; an empty subnet is not connected by its VPC.
- A gateway attached to a VPC does not connect the VPC (every default VPC has an internet
  gateway, and the pair would keep each other). A VPN gateway connects its VPC only when a
  kept VPN connection uses it.
- A NAT gateway counts as connected when its VPC holds a kept instance. This is a heuristic:
  route tables are not collected.
- Instances are always kept, and so are types outside the selection rules (`include_all`
  types, KMS keys, secrets, and SSM parameters).

[selection_test.go](internal/platform/aws/selection_test.go) and
[finish_test.go](internal/platform/aws/finish_test.go) cover these, with
[prune_test.go](internal/model/prune_test.go) and [hide_test.go](internal/model/hide_test.go)
for the model's side. Update them with any rule change.

## Security

- **Never collect secret values.** `scrubVPNSecrets` (`aws_sdk/ec2.go`) clears each VPN
  connection's `CustomerGatewayConfiguration` and tunnel `PreSharedKey` at collection time;
  both carry the pre-shared keys. Keep that for any new VPN field use.
- **KMS, Secrets Manager, and SSM collectors keep identifying fields only.** They build
  records with `aws.KMSKey`, `aws.Secret`, and `aws.Parameter`, which take plain strings
  (ID or ARN, and name), never SDK types. They use only list/describe calls (`ListKeys`,
  `ListAliases`, `ListSecrets`, `DescribeParameters`). Do not add `GetSecretValue`,
  `GetParameter`, or SDK types there.
- Tests use hand-built data only. Never commit real account data (IDs, IPs, CIDRs, names) to
  tests or fixtures.

## Config and schema

- `config.Read` rejects unknown keys, so a typo fails at load. A `$schema` key in a config
  file is also rejected.
- [schema/config.schema.json](schema/config.schema.json) is maintained by hand. Any change to
  the config structs needs the matching schema change; nothing checks the two agree.
- Keys are snake_case, except `diagram.showGrid`.

## API quirks

AWS:
- `DescribeNatGateways` takes `Filter`, not the `Filters` every other EC2 describe call takes.
- Internet gateway attachments report state `"available"`, which the SDK has no constant for.
- EC2 query IDs are passed as an ID filter (`ec2Filters`), not the call's IDs parameter,
  because the IDs parameter fails the whole call when one ID is not in the region.

Cloudcraft:
- Upload matches blueprints by name; `--overwrite` updates the existing one in place.
- `export` JSON is the full blueprint object, re-indented from the raw response body.
  Decoding into the SDK struct would drop fields the SDK does not model.
- `ExportImage` gets empty `ImageExportParams`, not nil; nil makes the SDK force 1920x1080.
- Rate limit is 20 requests per 10 seconds; `export` paces calls at `requestInterval` (500ms).
- A text label anchored to a placeholder node made Cloudcraft crash on click. Prefix-list
  placeholders are unlabeled for that reason.

## draw.io

- Shapes come from the `aws4` library, which draw.io titles "AWS <current year>". 3D (`aws3d`)
  shapes were ruled out: too few of the drawn kinds exist there.
- Instance icons: [instance_shapes.go](internal/render/drawio/instance_shapes.go) maps each EC2
  family to its shape. draw.io names them inconsistently (`t3_instance`, `ec2_m6i_instance`,
  bare `c5a`), so the map comes from the compute palette in draw.io's
  `src/main/webapp/js/diagramly/sidebar/Sidebar-AWS4.js`. `sharedInstanceShape` covers group
  icons (Mac, high memory, Trainium, C-family `optimized_instance`); anything else falls back
  to `instance2`. An unknown shape name draws an empty box, so never guess a name.
- Azure, if added, uses image paths (`image=img/lib/azure2/...svg`), not stencil names.
- To check output visually, export a PNG with the desktop app:
  `/Applications/draw.io.app/Contents/MacOS/draw.io -x -f png -s 2 -o out.png file.drawio`.

## Testing approach

- Test this project's code up to the SDK boundary; do not test the Cloudcraft or AWS SDKs.
  `blueprintAPI` ([render/cloudcraft/output.go](internal/render/cloudcraft/output.go)) is the
  seam: production uses the SDK's blueprint service, tests use the fake in `fake_test.go`.
  `clients` ([aws_sdk/collect.go](internal/platform/aws/aws_sdk/collect.go)) is the AWS seam:
  one interface per SDK client, holding only the methods the collectors call. Tests use the
  fakes in that package's `fake_test.go`, which serve one example of each supported type.
  A new collector adds its SDK method to the matching interface and to the fakes.
- `main.go` is kept thin so the logic worth testing lives in packages.

## Local directories

- `exports/` holds Cloudcraft exports and generated output. It is git-ignored and not part
  of the build.
- Shape names are checked against draw.io's source,
  [github.com/jgraph/drawio](https://github.com/jgraph/drawio).
- Put generated output (renders, sample blueprints, comparison images) under
  `exports/claude/<topic>/`.
