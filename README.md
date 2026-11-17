# Diagramming tool

`diagram` reads AWS resources from an account and draws them as a draw.io file, a
Cloudcraft blueprint, or both. It was built to diagram GovCloud accounts. It reads resources
live from the AWS APIs, so it works in GovCloud and commercial regions alike, or from a
Steampipe database.

It also bulk-exports existing Cloudcraft blueprints to SVG, PNG, PDF, or JSON.

## Goals

The tool draws diagrams for people: useful, and pleasing to read. Resources sit in
consistent places, connections between them are drawn, and labels, colors, and shapes
show the system's organization at a glance, with each resource's details kept in its
shape data. [docs/DECISIONS.md](docs/DECISIONS.md#principles) lists the design principles.

## Prerequisites

- [devbox](https://www.jetify.com/devbox), which provides Go and golangci-lint at the pinned
  versions. With direnv installed, `.envrc` loads the devbox shell on `cd`.
- AWS credentials for the target account. The config file's `sso_profile_name` names a
  profile in `~/.aws/config`. Log in first with `aws sso login --profile <name>`.
- For Cloudcraft output or export, a Cloudcraft API key in `CLOUDCRAFT_API_KEY`. draw.io
  output does not need one.
- Optional: the [draw.io desktop app](https://www.drawio.com/), version 31.5 or later, for
  draw.io output. When it is installed, the tool runs its obstacle-avoiding router so
  connectors go around shapes in the saved file. It looks for `DRAWIO_BIN`, then `drawio` or
  `draw.io` on `PATH`, then the macOS install location. Without it, the file is saved
  unrouted, and draw.io routes a connector once a shape it touches moves, or all of them
  with Arrange > Layout > Orthogonal Routing.
- For an account with `"source": "steampipe"`, a read-only login to the Steampipe database in the standard `PG*` variables
  (`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`) or `~/.pgpass`,
  and a network path to the database (the VPN). No AWS credentials are needed.

## Quick start

```sh
export CLOUDCRAFT_API_KEY=...            # only for Cloudcraft output
aws sso login --profile my-sso-profile.role

# Upload a Cloudcraft blueprint (the default output)
go run . create --config config/myprofile.json

# Write a draw.io file instead, with no Cloudcraft account needed
go run . create --config config/myprofile.json --draw.io --export-path out/

# Both, replacing earlier output of the same name
go run . create -c config/myprofile.json --cloudcraft --draw.io -p out/ --overwrite

# A config whose account has "source": "steampipe" reads Steampipe instead of calling AWS
go run . create -c config/myprofile.json --draw.io -p out/
```

A successful run logs how many resources it found per type and region, then each place it
saved the diagram: a file path, a blueprint URL, or both.

## Commands

### `create`

Collects the resources named in a config file and saves the diagram.

| Flag                  | Meaning                                                                                          |
| --------------------- | ------------------------------------------------------------------------------------------------ |
| `--config`, `-c`      | Config file path. Required.                                                                      |
| `--name`, `-n`        | Diagram name. Overrides `diagram.name` in the config.                                            |
| `--cloudcraft`        | Upload a Cloudcraft blueprint. Default when no output flag is given.                             |
| `--draw.io`           | Write a draw.io file. Requires `--export-path`.                                                  |
| `--export-path`, `-p` | Output directory, or a file path: `exports/prod.drawio` names the diagram `prod`, unless `--name` is set, and writes `exports/prod.drawio`. With `--cloudcraft`, also writes the blueprint JSON there. |
| `--overwrite`         | Replace an existing blueprint or file of the same name. Without it, a name clash is an error.    |

With `source: "steampipe"` in the config, the diagram shows the Steampipe database's last refresh, not the live account.
The Steampipe source serves instances, VPCs, subnets, and security groups. Other configured types are
logged and skipped, and `filters` are rejected because they are EC2 API filter names; use
`ids`. `sso_profile_name` is ignored. With
`resources.include_all`, every other AWS table that has a VPC column is read too, and its
resources are drawn as plain boxes in draw.io.

Output file names:

- draw.io: `<name>.drawio`
- Cloudcraft JSON: `<name>.json`

Cloudcraft blueprints are matched by name. With `--overwrite`, the tool updates the existing
blueprint in place, so its URL stays the same.

### `export`

Downloads blueprints that already exist in the Cloudcraft account.

| Flag                  | Meaning                                                                    |
| --------------------- | -------------------------------------------------------------------------- |
| `--export-path`, `-p` | Output directory. Required.                                                |
| `--format`            | `svg`, `json`, `png`, `pdf`. Comma-separated or repeated. Required.        |
| `--name`, `-n`        | Export only the blueprint with this exact name. Default is every blueprint. |
| `--overwrite`         | Replace existing files.                                                    |

```sh
# Every blueprint in the account, as SVG and PNG
go run . export -p exports/ --format svg,png

# One blueprint, as JSON
go run . export -p exports/ --format json --name "AWS - ACS"
```

Files are named `<blueprint name>.<format>`. Requests are paced to stay under Cloudcraft's
rate limit, so a full-account export takes a while. One failed file does not stop the rest.
The command reports the failure count at the end.

## Configuration file

The full schema is [schema/config.schema.json](schema/config.schema.json). A config names
one account under `cloud`, then the diagram settings:

```json
{
  "cloud": {
    "aws_gov": {
      "sso_profile_name": "Example-GovCloud.ReadOnly",
      "regions": ["us-gov-east-1"],
      "resources": {
        "ec2": {
          "instances": { "ids": ["i-0123456789abcdef0", "i-0fedcba9876543210"] },
          "nat_gateways": {},
          "vpn_connections": { "show_unconnected_resources": false }
        }
      }
    }
  },
  "diagram": { "name": "Example-GovCloud-Diagram", "projection": "2d" },
  "options": { "show_unconnected_resources": false }
}
```

To read Steampipe instead of calling AWS, set `source` and `account_id` on the account:

```json
{
  "cloud": {
    "aws": { "source": "steampipe", "account_id": "111111111111", "regions": ["us-east-1"] }
  },
  "diagram": { "name": "Example-Steampipe-Diagram" }
}
```

| Key                                    | Meaning                                                                                     |
| -------------------------------------- | ------------------------------------------------------------------------------------------- |
| `cloud.aws`, `cloud.aws_gov`           | The account to draw, commercial or GovCloud. Set exactly one. Azure keys are not supported yet. |
| `<account>.source`                     | `sdk` (live AWS calls, the default) or `steampipe` (a Steampipe database).                  |
| `<account>.account_id`                 | 12-digit account ID. Required for `steampipe`.                                              |
| `<account>.sso_profile_name`           | AWS profile for `sdk`. Omit to use the SDK's default credential chain.                      |
| `<account>.regions`                    | Regions to collect from. Required. Each region is drawn side by side.                       |
| `<account>.resources.<service>.<type>` | A query that selects resources of one type. See below.                                      |
| `<account>.resources.include_all`      | `true` reads every resource type the database holds. `steampipe` only.                          |
| `diagram.name`                         | Diagram name. Required here or with `--name`.                                               |
| `diagram.projection`                   | `2d` or `isometric`.                                                                        |
| `diagram.theme`                        | `light` or `dark`.                                                                          |
| `diagram.showGrid`                     | `false` hides the grid.                                                                     |
| `options.show_unconnected_resources`   | `false` drops resources with no connection to anything else drawn. Default `false`.         |
| `options.colors.<tier>`                | Colors for `public`, `private`, or `isolated` subnets, as `#RRGGBB`. A list rotates across the tier's subnets. Defaults are one hue per tier, chosen to stay apart for readers with color vision deficiencies. |
| `options.colors.resources`             | Colors for single resources by ID: a subnet, VPC, or security group. Wins over the tier.   |

### App-level options

An optional app-level file sets `options` for every diagram:
`$XDG_CONFIG_HOME/diagramming/config.json` when `XDG_CONFIG_HOME` is set to an absolute
path, otherwise `diagramming/config.json` in the OS config directory (`~/.config` on
Linux, `~/Library/Application Support` on macOS). It holds only `options`:

```json
{
  "options": {
    "show_unconnected_resources": false,
    "colors": { "private": ["#00A4A6", "#147EBA"] }
  }
}
```

A diagram's own `options` override it: a set value wins, a tier's color list replaces the
app's, and `colors.resources` entries merge by ID. Its schema is
[schema/app-config.schema.json](schema/app-config.schema.json).

In draw.io output, a subnet is drawn in its color (a tint as fill, and its label in a
darker shade), and neighboring subnets alternate a light and a stronger tint. Each security
group rule connector takes the color of the subnet, or colored resource, it starts from, so
a connector shows where its traffic comes from. A color too light to read is drawn in a
darker shade of itself: lines reach 3:1 contrast and text 4.5:1. Labels use Roboto, and
addresses, CIDRs, and ports use Roboto Mono, loaded from Google Fonts when draw.io is
online. A subnet's
tier comes from its `map_public_ip_on_launch` flag until route tables are collected, so
no subnet is `isolated` yet.

Unknown keys are rejected, so a typo fails at load instead of producing an empty diagram.
This also means a `$schema` key in the config file is an error. To get editor validation,
map the schema in your editor settings instead.

### Resource queries

Each resource type takes a query object:

- `ids` - collect these resources by ID. Listed IDs are always drawn.
- `filters` - AWS API filters, as `{"name": "...", "values": ["..."]}`. For example
  `{"name": "tag:Environment", "values": ["prod"]}`.
- `show_unconnected_resources` - overrides the global option for this type. Cannot be
  combined with `ids`.
- `hide` - leaves this type out of the diagram. A hidden container's contents are drawn in
  its own container instead: `"subnets": {"hide": true}` puts every resource, and its
  security group boxes, directly in the VPC.
- `hide_children` - leaves out everything inside this type: a VPC's or subnet's contents,
  or a security group's members. With `hide`, the container goes too.

Hiding happens before `show_unconnected_resources` pruning, so hiding instances can leave
their subnets empty, and pruning then drops them.

AWS ANDs `ids` and `filters`. An empty query (`{}`) selects every resource of the type.

Resource types come in two groups:

- **Primary** types are collected only when the config names them. These are the
  resources the diagram is about.
- **Container** types are always collected, because primary resources sit inside them. A
  query on a container type only narrows the results.

| Service          | Primary                        | Container                                                                                                                                                |
| ---------------- | ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ec2`            | `instances`, `vpn_connections` | `vpcs`, `subnets`, `security_groups`, `internet_gateways`, `nat_gateways`, `vpn_gateways`, `transit_gateways`, `customer_gateways`, `prefix_lists`        |
| `kms`            | `keys` (ids only, no filters)  |                                                                                                                                                          |
| `secretsmanager` | `secrets`                      |                                                                                                                                                          |
| `ssm`            | `parameters`                   |                                                                                                                                                          |

KMS keys, secrets, and SSM parameters are collected by name and ARN only. The tool never
reads secret or parameter values. draw.io draws them as plain boxes; Cloudcraft does not
draw them.

## Other Cloudcraft settings

The Cloudcraft SDK reads these optional environment variables in addition to
`CLOUDCRAFT_API_KEY`: `CLOUDCRAFT_PROTOCOL`, `CLOUDCRAFT_HOST`, `CLOUDCRAFT_PORT`,
`CLOUDCRAFT_PATH`, `CLOUDCRAFT_MAX_RETRIES`, `CLOUDCRAFT_TIMEOUT`. The defaults point at the
public Cloudcraft API.

## Development

Run these from a devbox shell, or prefix each with `devbox run`:

| Command                | Does                                                                  |
| ---------------------- | --------------------------------------------------------------------- |
| `devbox run build`     | `go build ./...`                                                      |
| `devbox run test`      | `go test ./...`                                                       |
| `devbox run lint`      | golangci-lint with [.golangci.yml](.golangci.yml)                     |
| `devbox run coverage`  | Tests with coverage, checked against [.testcoverage.yml](.testcoverage.yml) (80% per file, 70% per package) |
| `devbox run deadcode`  | Reports unreachable functions                                         |

### Layout

A run flows `config` -> source -> platform -> `model` -> `render`:

| Package                                                                          | Role                                                                                                   |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| [internal/config](internal/config)                                               | Reads and validates the config file.                                                                   |
| [internal/inventory](internal/inventory)                                         | `Record` and `Region`: resources in a form that depends on neither source nor platform.               |
| [internal/platform/aws](internal/platform/aws)                                   | What the tool knows about AWS: resource types, record builders from SDK types, containment, and selection (`Finish`). |
| [internal/platform/aws/aws_sdk](internal/platform/aws/aws_sdk)                   | Calls the AWS APIs, all collectors in all regions concurrently (capped at 8 in flight).                |
| [internal/platform/aws/aws_steampipe](internal/platform/aws/aws_steampipe)       | Reads Steampipe's AWS tables, decoding each row into the SDK type the builders take.          |
| [internal/steampipe](internal/steampipe)                                         | Reads Steampipe tables, for any platform: the Postgres connection, queries, decoding, and discovery.   |
| [internal/model](internal/model)                                                 | Platform-neutral diagram: hides, prunes, and builds nodes, containment, and edges from records.        |
| [internal/layout](internal/layout)                                               | Positions nested boxes and routes links with ELK, through D2's Go library.                             |
| [internal/render](internal/render)                                               | The `Output` interface every format implements, and shared file helpers.                              |
| [internal/render/cloudcraft](internal/render/cloudcraft)                         | Builds blueprint JSON, uploads it, and implements `export`.                                            |
| [internal/render/drawio](internal/render/drawio)                                 | Builds draw.io XML from the ELK layout. Resource attributes appear under Edit Data.                    |

The package doc comments give step-by-step instructions for adding a resource type:
[collect.go](internal/platform/aws/aws_sdk/collect.go) for collection, including the type,
builder, and selection rules in [internal/platform/aws](internal/platform/aws), and
[cloudcraft.go](internal/render/cloudcraft/cloudcraft.go) for Cloudcraft drawing. draw.io
shapes for each type are in [render/drawio/ec2.go](internal/render/drawio/ec2.go). A new
service also needs an entry in [schema/config.schema.json](schema/config.schema.json).

`exports/` is a git-ignored local working directory. The build does not use it.
