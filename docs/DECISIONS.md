# Design decisions

Each entry records a design decision the code reflects and the reason for it.
[DESIGN.md](DESIGN.md) describes the system those decisions produce. This file holds
current decisions only. When the code changes, its entry is edited or removed, and git
keeps the history.

## Principles

These principles guide how the tool draws a diagram. Entries below give a principle as
their reason where one applies.

1. **The diagram tells a story.** Each diagram type has a story that fits in one
   sentence. The default diagram's story is what exists in the account: which resources
   sit where and how they connect. The story decides what is shown and emphasized, never
   where a kind of resource sits. A shape earns its place by serving the story, and a
   shape that does not is pruned. What remains after hiding and pruning stays faithful to
   what exists.
2. **The diagram is for people.** The output is a diagram a person finds useful and
   pleasing to read. When a rule makes a diagram harder to read, legibility wins, and the
   decision that bends the rule names it.
3. **Resources sit in consistent places.** The same kind of resource lands in the same
   kind of place in every diagram, so a reader learns the layout once.
4. **Containment is visible.** A child sits fully inside its parent, inset from the
   parent's edges. It never touches or crosses the parent's border; the parent grows
   instead.
5. **Connections are drawn, with direction.** Where the data shows how resources connect
   within the network, the diagram draws that connection. Arrowheads show its direction,
   never color alone. Where the renderer controls routing, connectors are orthogonal,
   bend as little as possible, and touch shapes only at their outlines.
6. **The diagram reads at a glance, and shape data holds the details.** Each shape's data
   records the resource's metadata. The facts a reader needs to grasp the system's
   organization also appear as labels, colors, and shapes. A label sits on or beside the
   thing it names. For example, a connector for allowed traffic takes the color of the
   group where the traffic starts, and its ports label it near that start. Each instance
   family gets its own shape where one exists.
7. **Color carries meaning.** The same color means the same thing everywhere: public,
   private, and isolated subnets each have a consistent color. Color usually, though not
   always, marks resources that are alike. Color is never the only cue: anything color
   tells the reader is also told by a label, a line style, or a shape. Emphasis comes
   from muting the rest, not from recoloring what matters. A hue that reads as danger is
   kept for risk and is never decorative, including in a tier's color rotation.
8. **Color stays readable.** Colors are chosen to avoid clashes and to stay
   distinguishable for readers with color vision deficiencies. Lines reach at least 3:1
   contrast against the canvas, and text at least 4.5:1.

## Run stages and contracts

### Records are neutral to platform and source

- **Decision:** every source returns `inventory.Region` values holding `inventory.Record`
  values. The model reads records only and imports no platform, source, or renderer
  package.
- **Why:** the model can draw any platform's records without knowing the platform, so a
  new source or platform needs no change to the model.
- **Alternatives:** the model reads SDK types directly; a model per cloud, which would
  break the rule that renderers draw whatever the model holds.
- **Shows in:** `internal/inventory`, `internal/platform/aws`, `internal/model`

### The platform finishes records before the model runs

- **Decision:** after a source returns, the platform's `Finish` resolves containment into
  `Parent`, sets connection fields (`KeepIf`, `Attached`), and folds the config's `hide`,
  `hide_children`, and `show_unconnected_resources` settings into record fields. The model
  reads no config.
- **Why:** config paths and selection rules are per platform, so the model cannot
  resolve them. The model applies the record fields the same way for every platform.
- **Shows in:** `internal/platform/aws`, `internal/inventory`, `internal/model`, `main.go`

### Every output implements one interface

- **Decision:** draw.io and Cloudcraft each implement `render.Output`, whose `Save`
  returns where the result went. `main.go` loops over outputs without knowing their
  types.
- **Why:** the command treats every format alike, so `main.go` stays a thin layer of flag
  parsing, output building, and logging.
- **Shows in:** `internal/render`, `internal/render/cloudcraft`, `internal/render/drawio`,
  `main.go`

### Outputs are built before collection

- **Decision:** `create` builds its outputs, and each checks its own requirements, before
  reading the config or collecting resources.
- **Why:** collection is slow, so a missing requirement such as the Cloudcraft API key
  fails before any cloud call.
- **Shows in:** `main.go`, `internal/render/cloudcraft`

### Resource IDs are the cloud IDs

- **Decision:** nodes, edges, and Cloudcraft components use the cloud's resource IDs.
  Every reference is a plain ID.
- **Why:** rebuilds reproduce the same IDs, so diagrams can be compared over time. Group
  members, rule targets, edge ends, and label anchors name a resource directly, as in
  Cloudcraft's own scans.
- **Shows in:** `internal/model`, `internal/render/cloudcraft`, `internal/render/drawio`

### Renderers stay platform-aware

- **Decision:** the renderers import the platform package for resource types, attribute
  keys, and rule target types, and pick shapes by `Platform` and `Type`.
- **Why:** an informative, readable diagram needs some platform-specific knowledge, such
  as which shape draws each resource type.
- **Shows in:** `internal/render/cloudcraft`, `internal/render/drawio`,
  `internal/platform/aws`

## Sources

### Two AWS sources share one set of record builders

- **Decision:** the live SDK source and the Steampipe source both stay. The Steampipe
  source decodes each row into the AWS SDK type that the SDK source reads, and both hand
  those types to the same builders in `internal/platform/aws`.
- **Why:** the SDK source reads live state and reaches GovCloud. The Steampipe source
  covers far more resource types without a collector per type and needs no cloud
  credentials at run time. Steampipe's scalar columns are renamed SDK fields and its
  jsonb columns already hold SDK JSON, so both sources produce the same records.
- **Alternatives:** the Cloud Control API, which would change what `filters` mean, return
  CloudFormation property names, and may return secret fields.
- **Shows in:** `internal/platform/aws/aws_sdk`, `internal/platform/aws/aws_steampipe`,
  `internal/platform/aws`

### One Steampipe reader serves every platform

- **Decision:** `internal/steampipe` knows the shape of Steampipe tables but not what any
  table means. The account, region, and soft-delete columns are parameters, so no
  platform's column names are built in.
- **Why:** a copy per platform would duplicate every fix to connection, decoding, SQL,
  and discovery.
- **Shows in:** `internal/steampipe`, `internal/platform/aws/aws_steampipe`

### Steampipe columns are read as text

- **Decision:** every selected column is cast to text and returned as a `Row` of string
  pointers, with nil for SQL NULL. Only `postgres.go` imports the database driver.
- **Why:** small type differences in the drained schema do not break decoding, and the
  driver stays behind one thin file.
- **Shows in:** `internal/steampipe`

### The Steampipe source reads tables one after another

- **Decision:** the Steampipe source reads one table at a time on the caller's goroutine,
  while the SDK source runs collectors concurrently.
- **Why:** one Steampipe query already covers every configured region, and a single
  `pgx.Conn` is not safe for concurrent use. SDK clients are regional, so the SDK source
  needs a call per region and type.
- **Shows in:** `internal/platform/aws/aws_steampipe`, `internal/steampipe`

### SDK collectors run concurrently with a cap

- **Decision:** the SDK source runs every collector in every region through one
  `errgroup`, with at most 8 calls in flight. Each collector writes only its own field of
  its region's results, and records are built after all collectors finish.
- **Why:** EC2 throttles describe calls per account, and the cap keeps throttled retries
  rare. One writer per field keeps collection free of locks.
- **Shows in:** `internal/platform/aws/aws_sdk`

### EC2 query IDs go in as a filter

- **Decision:** IDs from the config are sent as a per-type ID filter, never as the
  describe call's IDs parameter.
- **Why:** the IDs parameter fails the whole call when one ID is not in the region.
- **Shows in:** `internal/platform/aws/aws_sdk`

### Container types are always collected

- **Decision:** VPCs, subnets, security groups, gateways, and prefix lists are collected
  even when the config does not name them, and a query on them only narrows the result.
  Instances, VPN connections, KMS keys, secrets, and SSM parameters are collected only
  when named.
- **Why:** primary resources sit inside containers, so the diagram shows every container
  they need.
- **Shows in:** `internal/config`, `internal/platform/aws/aws_sdk`,
  `internal/platform/aws/aws_steampipe`

### include_all reads every placed table through one generic path

- **Decision:** with `include_all`, the Steampipe source discovers every AWS table that
  has an ARN, a title, and a VPC column. Tables with no hand-written mapping go through
  one generic reader and `aws.Generic`, which keep only ID, name, placement, and security
  groups.
- **Why:** the Steampipe source scales without enumerating resource types, and new tables
  appear without code changes. A table with no VPC column has nowhere to be drawn.
- **Shows in:** `internal/platform/aws/aws_steampipe`, `internal/platform/aws`

### The Steampipe source rejects filters

- **Decision:** a config query with `filters` fails when the account's source is
  `steampipe`. IDs still apply.
- **Why:** `filters` are EC2 API filter names, which mean nothing to a Steampipe table.
- **Shows in:** `internal/platform/aws/aws_steampipe`

## Platforms

### Platform knowledge lives in one package per platform

- **Decision:** `internal/platform/aws` holds AWS resource types, record builders, rule
  flattening, and `Finish`. Each source is a subpackage, and `main.go` picks the source
  per account.
- **Why:** both sources share the platform code and return the same records. A parent
  package cannot import a child that imports it, so the choice of source sits outside the
  platform package.
- **Shows in:** `internal/platform/aws`, `main.go`

### Resource types use CloudFormation names

- **Decision:** resource types are the names the AWS CLI and CloudFormation use, such as
  `AWS::EC2::Instance`, and discovered Steampipe tables map to those names.
- **Why:** the SDK and Steampipe name and case the same resource differently. One naming
  convention, which users can look up, identifies a resource type whatever the source.
- **Shows in:** `internal/platform/aws`, `internal/platform/aws/aws_steampipe`

### A resource in several subnets sits in the first by zone

- **Decision:** a resource listed in several subnets gets as its parent the first subnet
  sorted by availability zone and then by ID.
- **Why:** sorting by zone puts a resource that spans zones in the same zone as its
  peers, whatever order the source listed its subnets in.
- **Shows in:** `internal/platform/aws`

### A parent must be a collected container

- **Decision:** a record's parent is its subnet or VPC only when that container is in
  the region's records. Otherwise it sits at region level.
- **Why:** a resource is never placed in a container the config filtered out.
- **Shows in:** `internal/platform/aws`

### Attached gateways do not keep their VPC

- **Decision:** internet and VPN gateways are marked `Attached`. They are kept while
  their VPC is kept, but they do not keep it. A VPN gateway keeps its VPC only through a
  kept VPN connection.
- **Why:** every default VPC has an internet gateway, and the pair would otherwise keep
  each other.
- **Shows in:** `internal/platform/aws`, `internal/model`

### Exposure comes from launch flags and public IPs

- **Decision:** a subnet's tier is public when it maps public IPs on launch and private
  otherwise. An instance's tier is public when it has a public IP and unknown otherwise.
  A NAT gateway is kept while its VPC holds a kept instance.
- **Why:** route tables are not collected, so the launch flag and public IPs are the
  signals available. Names are the least reliable signal and are not used.
- **Shows in:** `internal/platform/aws`

### Listed IDs are always drawn

- **Decision:** a resource type whose query lists IDs is never pruned. Otherwise the
  type's `show_unconnected_resources`, then the global option, decides. Instances are
  never pruned, and types outside the selection rules, such as `include_all` types, are
  always kept.
- **Why:** listed IDs and instances are what the config asks to draw.
- **Shows in:** `internal/platform/aws`, `internal/config`

## Model

### Selection flags keep resources by default

- **Decision:** the zero value of every selection field on a record keeps the resource
  drawn: `Prunable` false, `Attached` false, `Hidden` false, and `KeepIf` empty.
- **Why:** a platform that forgets a flag draws too much rather than silently dropping
  resources.
- **Shows in:** `internal/inventory`, `internal/model`

### Hiding runs before pruning

- **Decision:** `model.Build` applies `hide` and `hide_children`, then prunes.
- **Why:** hiding can leave containers empty, and pruning sees the result.
- **Shows in:** `internal/model`

### Unconnected containers are pruned on request

- **Decision:** when `show_unconnected_resources` is false for a type, its containers are
  kept only by what they hold or by a record in their `KeepIf`. A container does not keep
  its children. When the setting is true, nothing of that type is pruned.
- **Why:** containers with nothing drawn in them clutter the diagram.
- **Shows in:** `internal/platform/aws`, `internal/model`

### Pruning repeats until nothing changes

- **Decision:** pruning passes over the records until a pass drops nothing.
- **Why:** dropping one record can leave another unconnected, such as a customer gateway
  whose only VPN connection was dropped.
- **Shows in:** `internal/model`

### Omissions are logged, not silent

- **Decision:** a reference the model does not draw, or a resource or rule a renderer
  cannot draw, is logged as a warning, and the run continues.
- **Why:** an omission surfaces for triage instead of disappearing.
- **Shows in:** `internal/model`, `internal/render/cloudcraft`, `internal/render/drawio`

## Layout

### draw.io uses an engine layout; Cloudcraft keeps a fixed grid

- **Decision:** `internal/layout` lays out draw.io diagrams only. Cloudcraft output keeps
  its own fixed grid.
- **Why:** Cloudcraft recomputes group boxes and draws edges as straight lines, so an
  engine layout does not carry over.
- **Shows in:** `internal/layout`, `internal/render/cloudcraft`, `internal/render/drawio`

### Layout is behind an engine-neutral interface

- **Decision:** `internal/layout` defines neutral graph and result types and a `Layouter`
  interface. It imports no other package of this module.
- **Why:** a layout engine can be swapped without changing the renderer.
- **Shows in:** `internal/layout`, `internal/render/drawio`

### ELK lays out the graph through D2

- **Decision:** the layout engine is ELK, run through D2's Go library.
- **Why:** in a trial on real diagrams, ELK nested containers cleanly and routed links
  without crossings.
- **Alternatives:** Graphviz and dagre.
- **Shows in:** `internal/layout`

### Siblings sit in a near-square grid

- **Decision:** a container with more than one child places its children in a D2 grid,
  near-square unless the caller sets the column count.
- **Why:** ELK's layered algorithm puts siblings with no links between them in one row.
- **Shows in:** `internal/layout`

## Rendering

### Cloudcraft components are untyped maps

- **Decision:** Cloudcraft nodes, groups, and edges are built as untyped maps shaped like
  exported diagrams.
- **Why:** Cloudcraft publishes no schema for these components.
- **Shows in:** `internal/render/cloudcraft`

### Empty Cloudcraft groups get a placeholder node

- **Decision:** every subnet, security group, and VPC with no other node gets a
  placeholder node.
- **Why:** Cloudcraft sizes a group around its members and does not render an empty
  group. Cloudcraft's own diagrams use placeholders the same way.
- **Shows in:** `internal/render/cloudcraft`

### Prefix lists are drawn as stand-in security groups in Cloudcraft

- **Decision:** each prefix list is an unlabeled placeholder inside a stand-in security
  group, and rules to a prefix list target that group.
- **Why:** a Cloudcraft rule can target a group but not a prefix list. A text label
  anchored to a placeholder made Cloudcraft crash when it was clicked.
- **Shows in:** `internal/render/cloudcraft`

### Cloudcraft labels are anchored to their node

- **Decision:** each label is a separate text item anchored to its node through
  `mapPos.relTo`.
- **Why:** the label moves with its node.
- **Shows in:** `internal/render/cloudcraft`

### Overwriting a blueprint replaces it in place

- **Decision:** upload matches blueprints by name. With `--overwrite`, it updates the
  existing blueprint without a version check.
- **Why:** the blueprint keeps its URL. `--overwrite` asks for the last write to win, so
  a version check would only get in the way.
- **Shows in:** `internal/render/cloudcraft`

### Export is paced; create is not

- **Decision:** `export` spaces its Cloudcraft API calls at least 500ms apart. A create's
  upload is not paced.
- **Why:** Cloudcraft allows 20 requests per 10 seconds. The SDK retries a rate-limited
  call, but pacing avoids spending retries on a long export. A create makes two or three
  calls, far below the limit.
- **Shows in:** `internal/render/cloudcraft`

### One failed export does not stop the rest

- **Decision:** `export` reports each file's result and keeps going after a failure, then
  returns the number that failed.
- **Why:** one bad blueprint does not block an account-wide export.
- **Shows in:** `internal/render/cloudcraft`, `main.go`

### JSON export keeps the raw blueprint

- **Decision:** the JSON export saves the API's response body, re-indented, rather than a
  decoded SDK struct.
- **Why:** decoding would drop fields the SDK does not model.
- **Shows in:** `internal/render/cloudcraft`

### Image export leaves size to the API

- **Decision:** image exports pass empty export parameters, not nil.
- **Why:** nil makes the SDK force 1920x1080, while empty parameters leave size and
  styling to the API's defaults.
- **Shows in:** `internal/render/cloudcraft`

### draw.io shape names are never guessed

- **Decision:** draw.io shapes come from the `aws4` library. Instance icons come from a
  map keyed by EC2 family, with a shared icon for family groups and a generic instance
  icon as the fallback. A resource type with no verified shape is drawn as a plain
  labeled box.
- **Why:** draw.io draws an unknown shape name as an empty box, and it names its instance
  shapes inconsistently.
- **Shows in:** `internal/render/drawio`

### Security groups are dashed boxes in draw.io

- **Decision:** each security group is a dashed, unfilled box in each subnet that holds
  its members. Groups with the same members share a box, and smaller groups nest inside
  larger ones.
- **Why:** members move with the box, and a dashed line reads as a grouping rather than a
  network boundary. Most overlapping groups nest.
- **Shows in:** `internal/render/drawio`

### A connector takes the color of where it starts

- **Decision:** a rule edge takes the color of the resource, security group box, subnet,
  or VPC it starts from. A rule from something with no color takes the palette's flow
  color, a light blue.
- **Why:** a reader sees where traffic comes from by color (principle 7). The fallback
  was red, which reads as risk.
- **Shows in:** `internal/render/drawio`, `internal/config`

### Default colors are one hue per tier

- **Decision:** public, private, and isolated subnets default to green, blue, and rose
  from a palette tuned from Okabe-Ito. Neighboring subnets alternate a light and a
  stronger tint in a checkerboard across zones and rows. A configured list of colors
  still rotates across a tier's subnets. Tests check that the defaults stay at least 15
  apart in CIELAB under simulated color vision deficiencies.
- **Why:** the earlier defaults, from draw.io's AWS palette, used several close hues per
  tier, and some collapsed for readers with color vision deficiencies: private and
  isolated looked alike with deuteranopia (principle 8). Tint tells neighbors apart, so
  hue can carry the tier alone (principle 7).
- **Alternatives:** dashed outlines for every other subnet, ruled out because dashed
  lines already mean security groups and rules.
- **Shows in:** `internal/render/drawio`

### Colors too light to read are darkened

- **Decision:** a line whose color is under 3:1 against the canvas is drawn in the
  color's text shade. Text in a color uses its text shade: the palette's own for palette
  colors, else the color darkened until it reaches 4.5:1 on what it sits on. This applies
  to configured colors too.
- **Why:** principle 8 holds whatever colors a config sets, and port labels in light
  colors were unreadable at 11px.
- **Shows in:** `internal/render/drawio`

### Uncolored text is dark charcoal

- **Decision:** a label with no color of its own uses dark charcoal, `#374151`. This
  covers resource labels, prefix lists, the default VPC label, and the Azure region
  label. Colored labels, such as subnets and security groups, keep their color.
- **Why:** one neutral text color keeps uncolored text consistent (principle 7). The
  default VPC label, light gray `#AAB7B8`, was about 2:1 on white (principle 8).
- **Shows in:** `internal/render/drawio`

### Literal values use a mono font

- **Decision:** draw.io labels use Roboto. Literal values use Roboto Mono: addresses,
  CIDRs, ports, region names, and the lines under a resource's name. Every cell names a
  Google Fonts stylesheet as its `fontSource`.
- **Why:** a mono font sets identifiers apart from names, so a reader finds them at a
  glance (principle 6). A draw.io file cannot embed a font, so draw.io loads it when
  online and falls back to its default font offline.
- **Alternatives:** relying on installed fonts, which shows Helvetica to most readers.
- **Shows in:** `internal/render/drawio`

### draw.io edges are routed by the desktop app

- **Decision:** edges use draw.io's orthogonal style with libavoid routing. When the
  draw.io desktop app is installed, `Save` runs it to route the edges. Without it, the
  file is saved unrouted.
- **Why:** D2 routes every link that touches a grid as a straight line. draw.io reroutes
  edges around shapes once a connected shape moves, so an unrouted file still works.
- **Shows in:** `internal/render/drawio`

### draw.io files are uncompressed

- **Decision:** draw.io XML is written uncompressed, and resource attributes are stored
  as cell data.
- **Why:** the file stays readable and diffable, and draw.io shows the attributes under
  Edit Data.
- **Shows in:** `internal/render/drawio`

### Rule port labels sit near the source

- **Decision:** a rule edge's port label sits near the edge's start.
- **Why:** a reader looks for the ports on the side whose traffic the rule allows.
- **Shows in:** `internal/render/drawio`

## Security

### Secret values are never collected

- **Decision:** each VPN connection's customer gateway configuration and tunnel
  pre-shared keys are cleared at collection time.
- **Why:** both carry the pre-shared keys, which then never reach logs, exports, or
  anything built from the inventory.
- **Shows in:** `internal/platform/aws/aws_sdk`

### Secret stores are read by name only

- **Decision:** KMS, Secrets Manager, and SSM are read only with list and describe calls.
  Their record builders take plain strings, keeping only ID or ARN and name.
- **Why:** no secret value or other field of a secret store can reach a record.
- **Shows in:** `internal/platform/aws/aws_sdk`, `internal/platform/aws`

### Steampipe queries name their columns

- **Decision:** every Steampipe query lists the columns it reads. No query selects every
  column.
- **Why:** Steampipe tables carry sensitive columns, such as an instance's user data.
- **Shows in:** `internal/steampipe`, `internal/platform/aws/aws_steampipe`

### Only checked names are written into SQL

- **Decision:** table and column names are checked against a strict pattern and are the
  only text written into SQL. Every value is a query argument.
- **Why:** names found in the database or the config never reach SQL unchecked.
- **Shows in:** `internal/steampipe`

### Credentials never sit in the config

- **Decision:** the Steampipe database connection comes from the `PG*` environment variables, the
  Cloudcraft key from `CLOUDCRAFT_API_KEY`, and AWS credentials from a named SSO profile
  or the SDK's default chain.
- **Why:** diagram configs are committed, and credentials never sit in them.
- **Shows in:** `internal/steampipe`, `internal/render/cloudcraft`, `internal/config`,
  `internal/platform/aws/aws_sdk`

## Configuration

### Unknown config keys fail the read

- **Decision:** config files are decoded with unknown fields disallowed, including a
  `$schema` key.
- **Why:** a typo fails at load instead of producing an empty diagram.
- **Shows in:** `internal/config`

### Config depends on no cloud SDK

- **Decision:** `internal/config` imports only the standard library. Its `Filter` type
  copies the AWS filter shape.
- **Why:** the config serves every platform and source.
- **Shows in:** `internal/config`

### A diagram covers one account

- **Decision:** a config names exactly one account, under `cloud.aws` or `cloud.aws_gov`.
- **Why:** the model has no account level.
- **Shows in:** `internal/config`

### Partitions are separate cloud keys

- **Decision:** GovCloud and commercial AWS are separate keys under `cloud`, each with its
  own region list.
- **Why:** one profile reaches one partition, so mixing partitions in one entry cannot
  work. Each key gets its own region enum in the schema.
- **Shows in:** `internal/config`, `schema/config.schema.json`

### Regions are checked by the schema only

- **Decision:** region names are a hand-maintained enum in the schema. `config.Read`
  does not check them.
- **Why:** generating the lists needs a login per cloud and partition, newly launched
  regions are rarely needed, and an unknown region failing at run time is acceptable.
- **Shows in:** `schema/config.schema.json`, `internal/config`

### Colors are hex strings

- **Decision:** every configured color must be `#RRGGBB`.
- **Why:** it is the form draw.io styles take.
- **Shows in:** `internal/config`

## Testing

### Tests stop at the SDK boundary

- **Decision:** each external dependency sits behind a small interface with a test fake:
  the AWS clients, Cloudcraft's blueprint service, the Steampipe source and connection,
  the draw.io desktop app, and the Steampipe and export constructors in `main.go`.
- **Why:** tests exercise this project's code without the SDKs, the network, a database,
  or the desktop app.
- **Shows in:** `internal/platform/aws/aws_sdk`, `internal/render/cloudcraft`,
  `internal/steampipe`, `internal/render/drawio`, `main.go`

### main is a thin wrapper around run

- **Decision:** `main` only calls `os.Exit` on the result of `run`, which takes the
  arguments and output writers and returns the exit code.
- **Why:** tests run every command without exiting, and the logic worth testing lives in
  packages.
- **Shows in:** `main.go`

## Ruled out

### Extending the Cloudcraft output

- **Decision:** the Cloudcraft renderer draws the EC2 and VPC types it started with. New
  resource types, including generic ones, are drawn in draw.io only.
- **Why:** Cloudcraft support is not expected to last, so new work goes to draw.io.
- **Shows in:** `internal/render/cloudcraft`

### Drawing routes as connectors

- **Decision:** routes between subnets, gateways, and other networks are not drawn as
  connectors. Connectors show allowed traffic between security groups and VPN
  connections.
- **Why:** a connector per route would make the diagram too cluttered to read.
- **Shows in:** `internal/render/drawio`

### Tuning Cloudcraft's layout

- **Decision:** Cloudcraft's fixed grid is not tuned.
- **Why:** Cloudcraft recomputes its group boxes and draws straight edges, so tuning does
  not carry over.
- **Shows in:** `internal/render/cloudcraft`

### 3D shapes in draw.io

- **Decision:** draw.io output does not use the `aws3d` shapes.
- **Why:** too few of the drawn resource types exist there.
- **Shows in:** `internal/render/drawio`

### Generated API clients and collectors

- **Decision:** no client code is generated from Smithy or OpenAPI specs, and collectors
  are hand-written.
- **Why:** the SDKs are already generated from those specs, and Steampipe is the main
  input for broad coverage.
- **Shows in:** `internal/platform/aws/aws_sdk`

### Per-service exposure flags

- **Decision:** flags such as RDS `publicly_accessible` do not set a resource's exposure.
- **Why:** exposure comes from subnet tiers, and a resource's own exposure shows through
  its security groups.
- **Shows in:** `internal/platform/aws`

### Content-fitted subnet boxes

- **Decision:** subnet boxes stretch to their grid cell rather than fitting their
  contents.
- **Why:** the zone columns stay aligned.
- **Shows in:** `internal/render/drawio`

### Modeling IAM

- **Decision:** users, roles, and policies are not collected or drawn.
- **Why:** the diagrams show network placement and traffic paths, and IAM is neither.
- **Shows in:** `internal/platform/aws`

### Connection specs in config files

- **Decision:** how resources connect is defined in Go code, not in JSON or other spec
  files read by a generic loader.
- **Why:** a spec moves the verbosity into files people edit by hand, where mistakes go
  uncaught until run time. Hard cases (lookups, templates, conditions) would still need
  code, so a reader would have to follow both.
- **Shows in:** `internal/platform/aws`

### Computing reachability

- **Decision:** the tool does not evaluate whether one resource can reach another through
  routes, network ACLs, and security groups.
- **Why:** the tool is a diagrammer. Reachability is analysis, and a different product.
- **Shows in:** `internal/platform/aws`

### Connections from policies, settings, or code

- **Decision:** connections that exist only in IAM policies, environment variables, or
  application code are not drawn.
- **Why:** collectors cannot see them reliably, and a guessed connector would mislead.
- **Shows in:** `internal/platform/aws`

### Network ACLs in include_all

- **Decision:** `include_all` skips the network ACL table.
- **Why:** a network ACL applies to subnets rather than living in one, and drawing it
  would keep otherwise empty subnets.
- **Shows in:** `internal/platform/aws/aws_steampipe`
