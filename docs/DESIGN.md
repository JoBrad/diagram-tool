# Diagramming tool: design

This document describes the system as built: what it does, how it is structured, and the
design choices behind it. [DECISIONS.md](DECISIONS.md) records the reason for each
choice. The [README](../README.md) covers usage and config keys, and
[AGENTS.md](../AGENTS.md) covers working rules for changing the code.

## Purpose and scope

The `diagram` CLI draws architecture diagrams of an AWS account from what is deployed. A
reader sees which resources sit where, how they are exposed, and which security group
rules let them talk to each other. The diagram is for people: resources sit in
consistent places, connections are drawn, and labels, colors, and shapes carry meaning.
[DECISIONS.md](DECISIONS.md#principles) lists the principles in full.

It has two commands:

- **`create`** collects one account's resources and draws them as a draw.io file, a
  Cloudcraft blueprint, or both.
- **`export`** saves existing Cloudcraft blueprints as svg, png, pdf, or json files.

It does not:

- Draw more than one account, or Azure, in one diagram.
- Collect route tables, so subnet exposure comes from launch flags.
- Collect or draw IAM.
- Read secret values from any source.
- Tune Cloudcraft's layout.

## Terms

| Term          | Meaning                                                                          | Example                               |
|---------------|----------------------------------------------------------------------------------|---------------------------------------|
| Resource      | One deployed thing                                                               | the EC2 instance `i-0abc...`          |
| Resource type | What kind of thing a resource is, as the AWS CLI and CloudFormation name it      | `AWS::EC2::Instance`                  |
| Platform      | The cloud a resource belongs to                                                  | `aws`                                 |
| Source        | Where a platform's resources are read from                                       | `sdk` (live AWS APIs), `steampipe`    |
| Record        | One resource as a source hands it on: neutral to platform and source             | an `inventory.Record`                 |
| Container     | A resource drawn as a box around others                                         | VPC, subnet, security group           |
| Tier          | How exposed a container or resource is                                           | `public`, `private`, `isolated`       |
| Node, edge    | What the model holds: a drawn resource, or a connection between two              | an instance; a VPN connection         |
| Output        | One rendered format                                                              | draw.io file, Cloudcraft blueprint    |

## How a run works

A `create` run has five stages:

1. **Config.** Read the diagram config, lay its options over the app-level options, and
   validate both.
2. **Source.** Read the account's resources from the live AWS APIs or a Steampipe
   database. Either way, each resource becomes a record built from an AWS SDK type.
3. **Platform.** `aws.Finish` resolves each record's parent, sets what keeps it, and
   folds the config's selection settings into record fields.
4. **Model.** `model.Build` hides, prunes, and turns records into nodes and edges, per
   region.
5. **Render.** Each output draws the model and saves it.

```
  App-level options                  Diagram config
  <user config dir>/diagramming/     config/<name>.json
  config.json
          |                                |
          +---------------+----------------+
                          |
                     [1] config
                 (diagram options win)
                          |
          +---------------+-----------------+
          |                                 |
  [2] platform/aws/aws_sdk          [2] platform/aws/aws_steampipe
  live AWS APIs, concurrent         Steampipe (Postgres), through
                                    internal/steampipe
          |                                 |
          +------- SDK types -> builders ---+
                          |
               [3] platform/aws: Finish
          parents, KeepIf, Attached, Prunable, Hidden
                          |
              []*inventory.Region of records
                          |
                    [4] model.Build
              hide, prune, nodes and edges
                          |
          +---------------+-----------------+
          |                                 |
  [5] render/drawio                 [5] render/cloudcraft
  layout: ELK through D2            fixed grid
  routing: draw.io desktop app      upload: Cloudcraft API
  (optional)                        local JSON copy (optional)
          |                                 |
     <name>.drawio                  blueprint URL, <name>.json
```

The contract between stages:

- **Source to platform:** AWS SDK types, passed to the record builders in
  `internal/platform/aws`.
- **Platform to model:** `[]*inventory.Region`, each holding finished records. **The
  platform finishes records before the model runs,** so the model reads only record
  fields. [why](DECISIONS.md#the-platform-finishes-records-before-the-model-runs)
  **Records are neutral to platform and source:** a new source or platform needs no
  change to the model. [why](DECISIONS.md#records-are-neutral-to-platform-and-source)
- **Model to render:** `*model.Diagram`, with regions of ordered nodes and edges. Each
  output implements `render.Output`.

## Components

### cli (`main.go`)

- **Responsibility:** parse the command line, build outputs, run the stages, and log
  where results went.
- **Commands:** `create` (`c`) and `export` (`e`). `run` returns exit code 2 for a missing
  or unknown command, 1 for a failed command, and 0 for success or help.
- **Depends on:** every internal package except `layout`.
- **Rules:**
  - Outputs come from `--draw.io` and `--cloudcraft`. With neither, Cloudcraft is used.
    draw.io needs `--export-path`.
  - The diagram name comes from `--name`, then the stem of a file `--export-path`, then
    `diagram.name` in the config.
  - The account's `source` picks the reader: `steampipe` opens the Steampipe database, anything else
    calls the live SDK source.
  - `create` stops at the first output whose `Save` fails, and logs what it saved.
- **Decisions:**
  - **Outputs are built before collection.** A missing requirement fails before any
    cloud call. [why](DECISIONS.md#outputs-are-built-before-collection)
  - **main is a thin wrapper around run.** Tests run every command without exiting.
    [why](DECISIONS.md#main-is-a-thin-wrapper-around-run)

### config (`internal/config`)

- **Responsibility:** read and validate the app-level and diagram config files, and
  merge their options.
- **Inputs and outputs:** two JSON file paths and `XDG_CONFIG_HOME` in; `*config.Config`
  out.
- **Depends on:** the standard library only.
- **Rules:**
  - The app-level file is optional and holds only `options`. A diagram's option replaces
    the app-level value when set. A tier's color list replaces the app-level list.
    Per-resource colors merge by ID, with the diagram winning.
  - `show_unconnected_resources` defaults to false after the merge.
  - A config names exactly one account, under `cloud.aws` or `cloud.aws_gov`, with
    non-empty `regions`. `source` is `sdk` (the default) or `steampipe`. `account_id` is
    required for `steampipe`.
  - `resources` holds `include_all` and a map of service to type to query. Config does not
    know which services and types exist; it decodes each query strictly, so a misspelled
    query field fails the read. The platform's registry checks the keys.
  - `include_all` needs the `steampipe` source. A query cannot set both `ids` and
    `show_unconnected_resources`.
  - `azure` and `azure_gov` are held as raw JSON and rejected with an error that names the
    cloud.
  - Validation errors name the dotted path of the failing key, prefixed with the file.
- **Decisions:**
  - **Unknown config keys fail the read.** A typo fails at load.
    [why](DECISIONS.md#unknown-config-keys-fail-the-read)
  - **Config depends on no cloud SDK.** It serves every platform and source.
    [why](DECISIONS.md#config-depends-on-no-cloud-sdk)
  - **A diagram covers one account.** The model has no account level.
    [why](DECISIONS.md#a-diagram-covers-one-account)
  - **Partitions are separate cloud keys.** One profile reaches one partition.
    [why](DECISIONS.md#partitions-are-separate-cloud-keys)
  - **Colors are hex strings.** It is the form draw.io styles take.
    [why](DECISIONS.md#colors-are-hex-strings)

### steampipe (`internal/steampipe`)

- **Responsibility:** read Steampipe tables for any platform: connect, build queries,
  return rows, and discover tables.
- **Inputs and outputs:** a `Table` (name, ID column, columns), a `Scope` (account,
  regions, and the column names for each), and optional IDs in; `[]Row` ordered by ID
  out. `Discover` returns tables with a given prefix that have every required column.
- **Depends on:** the standard library and the `pgx` driver. No package of this module.
- **Rules:**
  - Every read filters on the account and the regions, adds an ID filter when IDs are
    given, and leaves out soft-deleted rows when the scope names a deleted column.
  - `Text` returns "" for a NULL or unselected column. `JSONIDs` reads a jsonb list of
    plain IDs or objects keyed by one of the given names.
- **Decisions:**
  - **One Steampipe reader serves every platform.** Fixes are made once.
    [why](DECISIONS.md#one-steampipe-reader-serves-every-platform)
  - **Steampipe columns are read as text.** Type differences in the drained schema do
    not break decoding. [why](DECISIONS.md#steampipe-columns-are-read-as-text)
  - **Only checked names are written into SQL.**
    [why](DECISIONS.md#only-checked-names-are-written-into-sql)
- **Invariants:** `DB` wraps one connection, opened with `Connect` and closed by the
  caller. Connection settings come only from the `PG*` environment and `~/.pgpass`.

### platform (`internal/platform/aws` and its sources)

`internal/platform/aws` holds what the tool knows about AWS: resource types, the type
registry, attribute keys, record builders, rule flattening, and `Finish`. Its two sources
are subpackages.

- **Inputs and outputs:** builders take AWS SDK types and return `inventory.Record`
  values. Each source returns `[]*inventory.Region`, one per configured region, in config
  order. `Finish` takes the config and the regions and finishes the records in place.
- **Depends on:** `config` and `inventory`. `aws_steampipe` also depends on `steampipe`.
  The two sources never import each other.
- **Registry:** `aws.Registry` lists every type the config can name, with its config path
  (`resources.<service>.<key>`), its record type, and its flags: `Primary` (collected only
  when named; other types are context and always collected), `Prunable`, and `NoFilters`
  (KMS keys, whose list calls take no filters).
  - `aws.Validate` checks the config's resource keys against the registry and each query
    against its type's flags. `main.go` runs it before any source reads, so a typo fails
    before any AWS or database call.
  - Both sources key their readers by record type and take the config path and the
    primary flag from the registry. A test in each source checks every reader's type is
    registered.
- **Builders:**
  - A builder for a resource with a state returns false for a resource that is gone,
    such as a terminated instance or a deleted gateway, and the caller skips it.
  - A name is the `Name` tag, else the ID. Security groups and prefix lists use their own
    names.
  - Placement is expressed as `contained_in` references (subnet, then VPC), and group
    membership as `member_of` references.
  - A VPN connection becomes a connection record from its VPN gateway (or transit
    gateway) to its customer gateway.
  - Security group rules flatten into one rule per target, in AWS's order: IPv4 ranges,
    IPv6 ranges, groups, then prefix lists. Ingress comes before egress.
  - `aws.Generic` builds a record for any type with no builder of its own.
- **Finish:** for each region, in turn:
  1. Resolve `contained_in` references into `Parent`, then drop them.
  2. Set `KeepIf` and `Attached` from AWS's connection rules.
  3. Copy each registered type's `hide` and `hide_children` onto its records, and set
     `Prunable` from the registry's flag and the type's `show_unconnected_resources`.
     Types outside the registry, such as those read through `include_all`, are left as
     built.
- **Connection rules:** a VPC is kept by the VPN connections of its VPN gateways. An
  internet gateway is kept by its VPC. A VPN gateway is kept by its VPC and its VPN
  connections. A security group is kept by its members. A prefix list is kept by the
  groups whose rules name it. Transit and customer gateways are kept by their VPN
  connections.
- **Decisions:**
  - **Platform knowledge lives in one package per platform.** Sources share it and return
    the same records.
    [why](DECISIONS.md#platform-knowledge-lives-in-one-package-per-platform)
  - **Resource types use CloudFormation names.** One convention names a type whatever
    the source.
    [why](DECISIONS.md#resource-types-use-cloudformation-names)
  - **A resource in several subnets sits in the first by zone.** It lands in the same zone
    as its peers.
    [why](DECISIONS.md#a-resource-in-several-subnets-sits-in-the-first-by-zone)
  - **A parent must be a collected container.**
    [why](DECISIONS.md#a-parent-must-be-a-collected-container)
  - **Attached gateways do not keep their VPC.** A default VPC and its internet gateway
    would otherwise keep each other.
    [why](DECISIONS.md#attached-gateways-do-not-keep-their-vpc)
  - **Exposure comes from launch flags and public IPs.** Route tables are not collected.
    [why](DECISIONS.md#exposure-comes-from-launch-flags-and-public-ips)
  - **Listed IDs are always drawn.** Listed IDs and instances are what the config asks
    for. [why](DECISIONS.md#listed-ids-are-always-drawn)

#### aws_sdk (`internal/platform/aws/aws_sdk`)

- **Responsibility:** read the configured resources live through the AWS SDK: EC2, KMS,
  Secrets Manager, and SSM.
- **Rules:**
  - Credentials come from the account's `sso_profile_name`, or the SDK's default chain
    when it is empty.
  - A collector runs only when the config names its type, except types the registry
    does not mark primary, which always run. Every registered type has a collector.
  - KMS IDs match a key's ID, ARN, or alias, and a key's name is its first alias.
    Secrets Manager IDs match a name or ARN exactly. SSM IDs become an exact name filter.
  - Prefix list entries are not fetched.
  - Records are built in a fixed type order once every collector finishes.
- **Decisions:**
  - **SDK collectors run concurrently with a cap.**
    [why](DECISIONS.md#sdk-collectors-run-concurrently-with-a-cap)
  - **EC2 query IDs go in as a filter.**
    [why](DECISIONS.md#ec2-query-ids-go-in-as-a-filter)
  - **Container types are always collected.**
    [why](DECISIONS.md#container-types-are-always-collected)

#### aws_steampipe (`internal/platform/aws/aws_steampipe`)

- **Responsibility:** read AWS resources from Steampipe's AWS tables in a Postgres
  database, and decode each row into the SDK type the
  builders take.
- **Rules:**
  - The account's `account_id` is required. Reads are scoped to it and to the configured
    regions.
  - Four tables are mapped: instances, VPCs, subnets, and security groups. Other named
    types are logged as not in the database and skipped.
  - Tables of types the registry does not mark primary (VPCs, subnets, and security
    groups) are always read. Instances are read when named or with `include_all`.
  - `title` becomes a `Name` tag, jsonb columns decode as the SDK's JSON shapes, and a
    security group with no name takes its ID.
- **Decisions:**
  - **Two AWS sources share one set of record builders.**
    [why](DECISIONS.md#two-aws-sources-share-one-set-of-record-builders)
  - **The Steampipe source reads tables one after another.**
    [why](DECISIONS.md#the-steampipe-source-reads-tables-one-after-another)
  - **include_all reads every placed table through one generic path.**
    [why](DECISIONS.md#include_all-reads-every-placed-table-through-one-generic-path)
  - **The Steampipe source rejects filters.**
    [why](DECISIONS.md#the-steampipe-source-rejects-filters)

### inventory (`internal/inventory`)

- **Responsibility:** define `Region` and `Record`, the form every source and platform
  hands to the model.
- **Depends on:** nothing. It holds types and constants only.
- **Record fields:**
  - Identity: `ID` (the cloud's ID), `Type`, `Platform`, `Name`.
  - Placement: `Parent`, `Zone`, `Tier`.
  - Connections: `From`, `To`, and `Relation` for a record that is itself a connection.
    `Refs` for typed references; after `Finish`, only `member_of` remains.
  - Selection: `KeepIf`, `Attached`, `Prunable`, `Hidden`, `HideChildren`.
  - Content: `Attrs` (scalar attributes for labels and shapes) and `Rules` (security
    group rules, with an open `TargetType`).
- **Decisions:**
  - **Selection flags keep resources by default.** A platform that forgets a flag draws
    too much rather than too little.
    [why](DECISIONS.md#selection-flags-keep-resources-by-default)

### model (`internal/model`)

- **Responsibility:** the tool-neutral diagram: which resources to draw, where each one
  sits, and how they connect.
- **Inputs and outputs:** `[]*inventory.Region` in; `*Diagram` of regions, each with
  ordered nodes and edges, out. `Build` does not change its input.
- **Depends on:** `inventory` only. It reads no config and knows no platform.
- **Build:** for each region, hide, then prune, then add:
  - A record with `From` or `To` becomes an edge carrying its own ID and relation.
  - Every other record becomes a node, plus one membership edge per `member_of`
    reference.
- **Hide:** a `Hidden` record is removed, and its contents move up to the nearest drawn
  ancestor. `HideChildren` removes everything inside a record and every member of it,
  but keeps the record. A connection to a hidden record goes with it.
- **Prune:** everything starts kept. A prunable connection is kept while both its ends
  are kept. Any other prunable record is kept while a record in its `KeepIf` is kept, or
  while it holds a kept child that is not `Attached`. Records that are not prunable always
  stay. A survivor whose parent was dropped moves up to the nearest kept ancestor.
- **Decisions:**
  - **Hiding runs before pruning.** Pruning sees containers that hiding emptied.
    [why](DECISIONS.md#hiding-runs-before-pruning)
  - **Pruning repeats until nothing changes.** Dropping one record can leave another
    unconnected. [why](DECISIONS.md#pruning-repeats-until-nothing-changes)
  - **Unconnected containers are pruned on request.** With
    `show_unconnected_resources` false, a container is kept only by what it holds, which
    reduces clutter. [why](DECISIONS.md#unconnected-containers-are-pruned-on-request)
- **Invariants:** nodes keep the order the source returned them in. After `Build`, a
  node's parent is "" or a node in the same region.

### layout (`internal/layout`)

- **Responsibility:** position a graph of nested boxes and links, and return their
  coordinates and routes.
- **Inputs and outputs:** a `Graph` of items (leaves with a size, or containers) and
  links in; a `Result` of boxes by item ID and routes by link ID out. Coordinates are
  absolute points from the top left.
- **Depends on:** the standard library and D2. No package of this module. Only
  `render/drawio` uses it.
- **Rules:**
  - Items list a parent before its children, and item IDs contain no dots.
  - Siblings fill a grid row by row, in input order. A container's column count is its
    `Columns` value, else the ceiling of the square root of its child count.
  - Spacing and padding are the engine's defaults.
- **Decisions:**
  - **draw.io uses an engine layout; Cloudcraft keeps a fixed grid.**
    [why](DECISIONS.md#drawio-uses-an-engine-layout-cloudcraft-keeps-a-fixed-grid)
  - **Layout is behind an engine-neutral interface.** An engine can be swapped without
    changing the renderer.
    [why](DECISIONS.md#layout-is-behind-an-engine-neutral-interface)
  - **ELK lays out the graph through D2.**
    [why](DECISIONS.md#elk-lays-out-the-graph-through-d2)
  - **Siblings sit in a near-square grid.**
    [why](DECISIONS.md#siblings-sit-in-a-near-square-grid)

### render (`internal/render` and its outputs)

`internal/render` defines `Output` and two file helpers: `WriteFile`, which refuses to
replace a file unless asked, and `SplitExportPath`, which splits an export path that
names a file into a directory and a diagram name. Each format is a subpackage. Neither
subpackage imports the other.

- **Decisions:**
  - **Every output implements one interface.**
    [why](DECISIONS.md#every-output-implements-one-interface)
  - **Resource IDs are the cloud IDs.** Rebuilds reproduce the same IDs.
    [why](DECISIONS.md#resource-ids-are-the-cloud-ids)
  - **Renderers stay platform-aware.** A readable diagram needs some platform-specific
    knowledge. [why](DECISIONS.md#renderers-stay-platform-aware)
  - **Omissions are logged, not silent.**
    [why](DECISIONS.md#omissions-are-logged-not-silent)

#### render/drawio

- **Responsibility:** turn the model into a laid-out draw.io file.
- **Flow:** build a layout graph and cells from the model, lay it out with ELK, write
  uncompressed XML, then optionally route edges with the draw.io desktop app.
- **Canvas:** reads left to right from most to least contained, and top to bottom from
  public to private. A region is one row of its VPCs, then its regional resources. A VPC
  is its subnet grid, then its loose resources. Subnets form a grid with availability
  zones as columns and public rows above private ones. Layout-only blocks are laid out
  but never drawn.
- **Shapes and labels:** `aws4` icons 60x60 at the top of a 160x100 slot, with the label
  below holding one fact per line. Resource attributes are stored as cell data.
- **Colors:** each tier's subnet colors rotate in drawing order, and a per-resource color
  wins. A subnet uses its color as its outline, a light tint as fill, and a darker label.
- **Security groups:** dashed boxes per subnet, or per VPC for resources in no subnet.
  Groups with the same members share a box, and subset groups nest inside larger ones.
- **Edges:** an inbound rule that names a group or prefix list becomes a dashed edge from
  the source to each box of the receiving group, labeled with its ports. VPN connections
  are solid edges.
- **Requires:** an export directory. The desktop app is found through `DRAWIO_BIN`, then
  `PATH`, then the macOS install location, and each run is limited to two minutes.
- **Decisions:**
  - **draw.io shape names are never guessed.** An unknown name draws an empty box.
    [why](DECISIONS.md#drawio-shape-names-are-never-guessed)
  - **Security groups are dashed boxes in draw.io.**
    [why](DECISIONS.md#security-groups-are-dashed-boxes-in-drawio)
  - **A connector takes the color of where it starts.**
    [why](DECISIONS.md#a-connector-takes-the-color-of-where-it-starts)
  - **draw.io edges are routed by the desktop app.**
    [why](DECISIONS.md#drawio-edges-are-routed-by-the-desktop-app)
  - **draw.io files are uncompressed.** [why](DECISIONS.md#drawio-files-are-uncompressed)
  - **Rule port labels sit near the source.**
    [why](DECISIONS.md#rule-port-labels-sit-near-the-source)

#### render/cloudcraft

- **Responsibility:** build Cloudcraft blueprint JSON, upload it, and export existing
  blueprints.
- **Build:** regions sit side by side. Each region is built in three passes: drawers add
  nodes with placements, a fixed grid positions them, and linkers add groups and edges.
  The diagram's projection, theme, and grid setting carry over.
- **Save:** write `<name>.json` when there is an export directory, then upload, and return
  the file path and the blueprint URL.
- **Export:** list blueprints (all, or one by exact name), save each requested format, and
  skip a download when the file exists and overwrite is off.
- **Requires:** `CLOUDCRAFT_API_KEY`, checked when the output is built.
- **Decisions:**
  - **Cloudcraft components are untyped maps.**
    [why](DECISIONS.md#cloudcraft-components-are-untyped-maps)
  - **Empty Cloudcraft groups get a placeholder node.**
    [why](DECISIONS.md#empty-cloudcraft-groups-get-a-placeholder-node)
  - **Prefix lists are drawn as stand-in security groups in Cloudcraft.**
    [why](DECISIONS.md#prefix-lists-are-drawn-as-stand-in-security-groups-in-cloudcraft)
  - **Cloudcraft labels are anchored to their node.**
    [why](DECISIONS.md#cloudcraft-labels-are-anchored-to-their-node)
  - **Overwriting a blueprint replaces it in place.**
    [why](DECISIONS.md#overwriting-a-blueprint-replaces-it-in-place)
  - **Export is paced; create is not.** [why](DECISIONS.md#export-is-paced-create-is-not)
  - **One failed export does not stop the rest.**
    [why](DECISIONS.md#one-failed-export-does-not-stop-the-rest)
  - **JSON export keeps the raw blueprint.**
    [why](DECISIONS.md#json-export-keeps-the-raw-blueprint)
  - **Image export leaves size to the API.**
    [why](DECISIONS.md#image-export-leaves-size-to-the-api)

## Cross-cutting concerns

### Security

- **Secret values are never collected.** VPN pre-shared keys are cleared at collection
  time. [why](DECISIONS.md#secret-values-are-never-collected)
- **Secret stores are read by name only.** KMS, Secrets Manager, and SSM use list and
  describe calls, and their records keep only ID or ARN and name.
  [why](DECISIONS.md#secret-stores-are-read-by-name-only)
- **Steampipe queries name their columns.** Sensitive columns such as user data are
  never read. [why](DECISIONS.md#steampipe-queries-name-their-columns)
- **Only checked names are written into SQL,** and every value is a query argument.
  [why](DECISIONS.md#only-checked-names-are-written-into-sql)
- **Credentials never sit in the config.** They come from `PG*` variables,
  `CLOUDCRAFT_API_KEY`, and AWS profiles.
  [why](DECISIONS.md#credentials-never-sit-in-the-config)
- **The draw.io desktop app runs without a shell,** with a fixed argument list.
- **Export file names** have path separators replaced and NUL bytes dropped, so a
  blueprint name cannot write outside the export directory.

### Concurrency

- The SDK source runs one `errgroup` across all regions and collectors, with at most 8
  calls in flight. The first error cancels the rest. Each collector writes only its own
  field, so collection needs no locks.
  [why](DECISIONS.md#sdk-collectors-run-concurrently-with-a-cap)
- Every other stage runs on one goroutine. The Steampipe source reads one table at a
  time. [why](DECISIONS.md#the-steampipe-source-reads-tables-one-after-another)
- `main.go` runs with `context.Background()` and no timeout. The draw.io desktop app is
  the only call with a deadline.

### Configuration

- Two JSON files: an optional app-level file holding `options`, and the diagram config,
  whose options win. Both have hand-maintained JSON schemas in `schema/` for editor
  validation.
- Keys are snake_case, except `diagram.showGrid`.
- Each resource type takes a query: `ids`, `filters`, `show_unconnected_resources`,
  `hide`, and `hide_children`. `ids` and `filters` are ANDed, and an empty query selects
  every resource of its type.
- **Regions are checked by the schema only.**
  [why](DECISIONS.md#regions-are-checked-by-the-schema-only)
- Environment: `PG*` for the Steampipe database, `CLOUDCRAFT_API_KEY` and other `CLOUDCRAFT_*`
  settings for Cloudcraft, `DRAWIO_BIN` for the desktop app, and `XDG_CONFIG_HOME` for
  the app-level file.

### Errors and logging

- Collection errors name the config path and region. Config errors name the dotted key
  path and the file.
- Resources, references, and rules that cannot be drawn are logged as warnings with
  their region, and the run continues.
  [why](DECISIONS.md#omissions-are-logged-not-silent)
- The draw.io routing step is best effort: a missing or failing desktop app logs a
  warning, and the unrouted file is saved.
- `create` stops at the first failed output. `export` keeps going and reports how many
  files failed.

## Extension points

- **A resource type for the SDK source:** add its type, builder, and registry entry to
  `internal/platform/aws`, with any selection rules beyond the registry's flags, then a
  field to the SDK source's results, a collector that writes only that field, its entry
  in `collectors`, its SDK method to the client interface and fakes, and a schema entry.
  The config needs no change.
- **A resource type for the Steampipe source:** add a table mapping that decodes rows
  into the SDK type the builder takes. With `include_all`, a table with a VPC column is
  read through the generic path with no code change.
- **A source:** a subpackage of the platform that returns `[]*inventory.Region` built
  with the platform's builders, chosen per account in `main.go`.
- **A platform:** a package under `internal/platform` whose records set `Platform`,
  `Parent`, `Refs`, and the selection fields. The model and layout need no change.
  Renderers need shapes for its types.
- **A Cloudcraft type:** a drawer or linker registered in the renderer's lists.
- **An output:** a type that implements `render.Output`, built in `main.go`.
- **A layout engine:** an implementation of `layout.Layouter`.

## Ruled out

- **Extending the Cloudcraft output.** [why](DECISIONS.md#extending-the-cloudcraft-output)
- **Tuning Cloudcraft's layout.** [why](DECISIONS.md#tuning-cloudcrafts-layout)
- **3D shapes in draw.io.** [why](DECISIONS.md#3d-shapes-in-drawio)
- **Generated API clients and collectors.**
  [why](DECISIONS.md#generated-api-clients-and-collectors)
- **Per-service exposure flags.** [why](DECISIONS.md#per-service-exposure-flags)
- **Content-fitted subnet boxes.** [why](DECISIONS.md#content-fitted-subnet-boxes)
- **Drawing routes as connectors.** [why](DECISIONS.md#drawing-routes-as-connectors)
- **Modeling IAM.** [why](DECISIONS.md#modeling-iam)
- **Network ACLs in include_all.** [why](DECISIONS.md#network-acls-in-include_all)

## Constraints and known gaps

- Cloudcraft draws only the EC2 and VPC types it has drawers for. Generic `include_all`
  resources and KMS, Secrets Manager, and SSM records appear in draw.io only.
  [why](DECISIONS.md#extending-the-cloudcraft-output)
- draw.io files carry `host="cloudcraft"` in their `mxfile` element, a leftover from the
  tool's earlier name.
- Route tables are not collected, so no subnet is classified as isolated.
- The Steampipe source maps four tables. Other named types are skipped.
- The JSON schemas are maintained by hand, and nothing checks them against the config
  structs. In the regions schema, `minItems` and `uniqueItems` sit under `items`, so they
  do not apply to the list.
- `config.Read` does not reject duplicate regions.
- Flag errors exit through the flag package, bypassing `run`'s exit codes.
- `layout` does not reject duplicate item IDs; a duplicate overwrites an earlier box.
- The code has no TODO, FIXME, or HACK comments.
