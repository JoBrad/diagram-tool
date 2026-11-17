---
type: project
status: active
created: 2026-10-12
tags: [feature, multi-cloud, azure, cloudflare, schema]
branch: feature/multi-cloud-support
---

# Collect and draw AWS, AWS GovCloud, Azure, Azure Government, and Cloudflare resources from one config

The tool collects AWS commercial resources only, from one set of regions at the top of the
config. Teams also run workloads in AWS GovCloud and Azure, including Azure Government,
and use Cloudflare. The
config moves to a `cloud` object keyed by cloud, each entry with its own credentials,
regions, and resources. Each platform gets one package that turns its resources into
platform-neutral records, with a subpackage per source (SDK, Steampipe, Resource Graph). The
model draws from those records alone, with no platform logic. A separate generator keeps the JSON
schema in step with the Go config structs. Done means:

- A config naming any mix of the five platforms loads and validates in the editor.
- An Azure subscription draws in draw.io.
- A Cloudflare account draws in draw.io.
- Existing AWS configs, once migrated to the new shape, produce the same diagrams as before.
- The schema drift test passes, along with the repo's checks in `AGENTS.md`.
- New and changed code meets the `.testcoverage.yml` thresholds (80% per file, 70% per
  package).

## Decisions

- [x] 2026-10-12 - Inventory source: two backends. The SDK collectors stay; they cover
      commercial and GovCloud for the current types and run live. A Steampipe backend
      (a Postgres database, refreshed daily) is added; it covers far more types,
      commercial only until GovCloud is added. Nothing that works today is removed.
      The SDK backend can retire once Steampipe covers its types and partitions.
- [x] 2026-10-12 - `inventory` hands `model` neutral records (ID, kind, name, refs,
      attrs, rules) from the Steampipe backend. The SDK collectors still fill typed fields for
      now; converting them to records is a later task.
- [x] 2026-10-12 - GovCloud gets added to the Steampipe database, not left to the SDK
      collectors alone.
- [x] 2026-10-12 - Steampipe database connection details come from the standard `PG*` environment
      variables and `~/.pgpass`, never the config file.
- [x] 2026-10-12 - `include_all` sits under each account's resources
      (`cloud.aws.resources.include_all: true`), next to the per-type queries that narrow
      or override it. The user wants the Steampipe path to scale without enumerating resource
      types.
- [x] 2026-10-12 - Pruning: `show_unconnected_resources: false` removes every
      container (VPCs, subnets, security groups, gateways, prefix lists) that holds
      nothing kept. Instances and generic resources always stay. Unresolved security
      group rule references stay as warnings for now.
- [x] 2026-10-12 - A resource listed in several subnets (Lambda, RDS, load balancers,
      network ACLs) is drawn once, in its first subnet by availability zone name
      (`us-east-1a`), then subnet ID. Secondary subnets are not shown yet; shape data or a
      label for them is a later change. Resources with no VPC (most Lambda functions) are
      drawn at region level, outside any VPC.
- [x] 2026-10-13 - `include_all` reads every drained table, not only network-placed
      ones; resources with no VPC are drawn at region level. Exclusions name the API
      resource type (`AWS::EC2::NetworkAcl`), never a Steampipe table name. With
      `source: sdk`, `include_all` means every type the collectors support.
- [x] 2026-10-13 - Each cloud key holds an array of account objects (`"aws": [ {...} ]`),
      so one diagram can map several accounts or subscriptions. An object keyed by account
      would enforce uniqueness, but its keys carry no type. JSON Schema cannot require a
      unique `account_id` across array items, so that check belongs in `config.Read`. Two
      accounts can both hold `us-east-1`, so the model needs the account level
      (`Diagram -> Account -> Region`) to keep them apart.
- [x] 2026-10-13 - `cloud` keys are snake_case, like every other config key: `aws`,
      `aws_gov`, `azure`, `azure_gov`.
- [x] 2026-10-13 - No generated collectors. Steampipe is the main input, so the
      generator covers the JSON schema only. Earlier intent, kept for reference (2026-10-12): from a list of services,
      discover each SDK's major resource types, their list/describe calls, and their filter
      inputs; generate the inventory config types and collectors; generate the JSON schema
      from those. Open: the source (SDK method-name matching or Smithy models), how "major"
      types are chosen, how quirks and secret fields are handled, whether Azure needs it
      given Resource Graph, and whether the type count justifies it. Option raised
      2026-10-12: a hand-written manifest maps service -> resource type -> list/describe
      call, ID filter, filter names, and secret fields. The generator emits collectors,
      config types, and schema from it. If adopted, the manifest is JSON (decided,
      2026-10-12), and references between types are declared as output paths
      to a target type.
- [x] 2026-10-13 - Region validation is the schema enum only. An unknown region makes the
      tool fail at run time, which is acceptable.
- [x] 2026-10-13 - The JSON schema will be generated from the Go config types at some
      point. Losing hand control of its composition is accepted; hand rules (`ids` vs
      `show_unconnected_resources`, KMS no-filters) move to `JSONSchemaExtend` hooks.
- [x] 2026-10-13 - Azure goes through Steampipe first, which supersedes the 2026-10-12
      Resource Graph decision. Azure credentials in the config are not a concern for now.
- [x] 2026-10-13 - draw.io draws Azure for now. Cloudcraft can come later by adding the
      metadata its Azure components need.
- [x] 2026-10-12 - Rollout in two stages: AWS and AWS GovCloud first, then Azure and Azure
      Government. The Azure tasks wait for stage one to finish.
- [x] 2026-10-12 - Config shape: a top-level `cloud` object keyed by cloud, with regions and
      resources inside each entry. Supports several clouds in one config, and each key gets
      its own region enum without `if`/`then`.
- [x] 2026-10-12 - Partitions: GovCloud and Azure Government are separate `cloud` keys, not
      a `partition` field. One profile reaches one partition, so mixing regions across
      partitions in one entry can never work.
- [x] 2026-10-12 - Region lists are hand-maintained enums for the MVP. Generating them
      from the CLIs needs a login per cloud and partition. Newly launched regions are rarely
      needed.
- [x] 2026-10-12 - Commercial and gov variants share one base definition (`awsAccount`)
      and differ only in `regions`, closed with `unevaluatedProperties: false`.
- [x] 2026-10-12 - Inventory: one package per cloud SDK (`internal/inventory/aws`,
      `internal/inventory/azure`), not per partition. GovCloud uses the same SDK and calls.
      Superseded 2026-10-13 by the `internal/platform/<platform>` layout; the
      not-per-partition part stands.
- [x] 2026-10-12 - Model: a cloud-neutral core plus per-cloud kinds and attrs. Pure
      generic kinds lose the detail renderers need for shapes. A model per cloud would
      break the rule that renderers draw whatever the model holds. Superseded 2026-10-13:
      per-cloud kinds and rules move out of `model` into the platform packages.
- [x] 2026-10-12 - Azure inventory uses Azure Resource Graph. It needs no enablement, only
      read access, and returns no secret values. Rows decode into the Azure SDK's generated
      types. Superseded 2026-10-13: Azure goes through Steampipe first.
- [x] 2026-10-12 - AWS keeps its SDK collectors. Cloud Control API would change the meaning
      of config `filters`, return CloudFormation property names, and may return secret
      fields per type. Revisit per new resource type, not as a rewrite.
- [x] 2026-10-12 - No client code generated from Smithy or OpenAPI specs. The SDKs are
      already generated from them and are the typed layer for both clouds.
- [x] 2026-10-12 - Generation is a separate tool run through `go generate`, not a CLI
      subcommand. It keeps a dev task out of the CLI. Generated files are committed so a
      normal build never depends on running the generator.
- [x] 2026-10-13 - Created `internal/inventory/steampipe`. The package reads Steampipe tables and holds data for several platforms.
- [x] 2026-10-13 - `model` and `layout` hold no platform-specific logic. This is the
      measure of the restructure. `model` hides, prunes, and builds nodes, containment,
      and edges from records alone. A record carries platform-neutral meaning, so reading
      it needs no platform knowledge. Today `model` still switches on AWS types in
      `addRecords`, `recordAttrs`, and `ec2Rules`.
- [x] 2026-10-13 - Platform knowledge lives in one package per platform,
      `internal/platform/<platform>` (AWS, Azure, Cloudflare, and later ones): type
      constants, record builders, state filters, relations, containment, and the selection
      rules now in `internal/model/ec2.go`. Sources are subpackages of their platform (for
      example `platform/aws/sdk` and `platform/aws/steampipe`). They share the platform
      code and output records. `ec2.go` gets rewritten, not moved: its rules run on
      `model.Region`, which the platform package will not have.
- [x] 2026-10-13 - Records carry a `Prunable` flag whose zero value means kept. It
      replaces today's "a type with no selection rule is always kept" (instances, generic
      resources). A platform that forgets the flag draws too much rather than silently
      dropping resources.
- [x] 2026-10-14 - `KeepsParent` is inverted to `Attached`, so every selection flag's
      zero value keeps resources drawn. Only attachments (internet and VPN gateways) set
      it; a kept record that is not attached keeps its parent. `HideChildren` stays on
      the container: it tells the reader to stop at that record, which is the record's
      own setting, while `KeepsParent` pointed up at another record.
- [x] 2026-10-14 - Resource types follow one convention for every source: the API's
      service and type name, such as `EC2::NatGateway`, with no `AWS::` prefix. The SDK
      and Steampipe name and case the same resource differently, and one discoverable
      name is needed. A type only appears inside one cloud's records and config, so a
      cloud prefix adds nothing.
- [x] 2026-10-14 - The roadmap lives in `docs/ROADMAP.md`. This project carries stage 1,
      single-platform diagrams for AWS, Azure, and Cloudflare.
- [ ] Record-field additions made during the 2026-10-14 build, for review:
      `Hidden` and `HideChildren` (the platform folds the config's hide settings in, as
      it does `Prunable`, so `model` reads no config); `Zone` and `Tier` (the neutral
      layout fields the renderer decision named); and `Refs` stays, for memberships, plus
      `RelContainedIn` candidates that `aws.Finish` turns into `Parent`. Added in the
      Azure build: `Rule.Deny`, zero value allow, since Azure NSG rules can deny;
      draw.io never draws a deny rule as a connector.
- [x] 2026-10-14 - Azure resource types drop the `Microsoft.` namespace and keep the
      documented casing: `Network/virtualNetworks`, `Compute/virtualMachines`.
      `azure.ResourceType` maps any casing to that form; other publishers keep their
      namespace.
- [x] 2026-10-14 - A VM or scale set is a member of every NSG that filters it: its
      network interface's and its subnet's. A subnet NSG draws as a box around the
      subnet's VMs.
- [x] 2026-10-14 - Key Vault keys and secrets are out of the schema and config. Only
      vaults are read, by ID, name, and URI; `include_all` never reads the key, secret,
      or certificate tables.
- [x] 2026-10-14 - `include_all` works for Azure now: every regional `azure_` table
      with `id`, `name`, `resource_group`, `region`, `subscription_id`, and `_deleted_at`,
      read as ID, name, type, and AKS node pool subnets only.
- [ ] Built 2026-10-14 for review: Azure record builders take the Azure SDK's
      track-2 types (`armnetwork/v9`, `armcompute/v8`), as the AWS builders take SDK
      types; Steampipe's jsonb columns are ARM JSON, which those types decode. Key
      vaults take plain strings, as KMS keys do. Network interfaces and public IPs are
      records only until `azure.Finish` copies them onto VMs and firewalls, then are
      dropped. Azure IDs are lowercased everywhere, since Azure returns one ID in several
      casings.
- [x] 2026-10-13 - Record fields: `Parent` (one ID); `From`, `To`, and `Relation` when the
      record is an edge; `KeepIf []ID` (kept when any listed ID is kept: a NAT gateway
      lists its VPC's instances, a VPN gateway its VPN connections); `KeepsParent` (false
      for attachments such as internet gateways); `Prunable`; `Platform`, `Type`, and a
      string map `Attrs` for renderers; `Rules` in a neutral form Azure NSG rules can
      share. No `Role`: a record is a container when another names it as `Parent`, and an
      edge when it has `From`/`To`. Pruning is one generic loop that repeats until nothing
      changes. Details may move during the build; changes to this list go back to the user.
- [x] 2026-10-13 - The platform resolves `show_unconnected_resources` and folds it into
      `Prunable`: set only when the kind can be pruned and the resolved option for its type
      is false. Config paths are per platform, so `model` cannot resolve them. `model`
      reads no config for selection, and selection still runs once in `model` before any
      renderer.
- [x] 2026-10-13 - Shared platform code sits directly in the platform package
      (`platform/aws`). Choosing a source per account happens outside it (`main.go` or a
      small package above the platforms), since a parent cannot import a child that
      imports it.
- [x] 2026-10-13 - Record builders take SDK types. The Steampipe adapter decodes each row
      into the SDK struct: scalar columns are renamed fields, and jsonb columns are already
      SDK JSON. `include_all` tables with no hand-written mapping use a generic builder.
- [x] 2026-10-13 - One generic Steampipe reader in its own top-level package,
      `internal/steampipe`, beside the platforms rather than under them, since it serves
      several. A copy per platform would mean fixing the connection, decoding, SQL, and
      discovery code once per platform. Its AWS parts move to the platform: the `aws_` table
      prefix, the table-to-type map, `skipTables`, the column and key lists, and
      `firstSubnets`. The scoping column (`account_id`, `subscription_id`) and the
      `_deleted_at` filter become parameters.
- [x] 2026-10-13 - Renderers stay platform-aware for drawing. `Platform` and `Type` pick
      the shape library and shape. A string map `Attrs` carries labels and shape hints,
      read by each renderer's per-platform mapping. Values layout depends on (zone, tier)
      and security group rules get neutral fields.
- [x] 2026-10-13 - The AWS SDK collectors are tested up to the SDK boundary through a
      fake per client. `clients` in `internal/inventory/aws/collect.go` is the
      seam, as `blueprintAPI` is for Cloudcraft; `fake_test.go` serves one example of each
      supported type. Tests check the inputs sent, paging, ID matching, secret scrubbing,
      and that each collector writes only its own field. The SDKs themselves stay untested.
- [x] 2026-10-13 - For now, thin SDK-calling code with no fake is excluded in
      `.testcoverage.yml`: any future `internal/platform/*/{sdk,resourcegraph}` package.
      The AWS SDK collectors count toward coverage, and keep counting after they move to
      `platform/aws/aws_sdk`. Superseded 2026-10-14: the user removed the exclusion, so
      `.testcoverage.yml` excludes nothing.
- [x] 2026-10-13 - The platform restructure goes ahead now, since collectors will not be
      generated.
- [x] 2026-10-13 - The config `source` value `steampipe` refers to a db source created by Steampipe. `main.go` picks
      the source per account from it.

## Tasks

Stage one, AWS and AWS GovCloud:

- [ ] 🔥 Active: Steampipe spike - reads instances, VPCs,
      subnets, and security groups from the Steampipe database into inventory records, which the model
      turns into nodes (`internal/inventory/steampipe`, `internal/model/records.go`). Code and
      tests done 2026-10-12. Left: confirm column types against the live schema, run
      against a commercial account, compare with an SDK run (added 2026-10-12)
- [ ] Steampipe database: add the missing diagram tables (NAT, internet, VPN, customer, and
      transit gateways, VPN connections, prefix lists, KMS, Secrets Manager, SSM), with
      column allowlists on any table that can carry secrets (added 2026-10-12)
- [ ] 🧩 Refining: Add the GovCloud accounts to the Steampipe database (added 2026-10-12)
- [ ] 🔥 Active: `include_all` prototype - `steampipe` only. Discovers AWS tables with
      `arn`, `title`, and `vpc_id` from `information_schema`, reads only ID, name,
      placement, and security group columns, and draws unknown kinds as plain boxes in
      draw.io (Cloudcraft skips them). Built and run against account B 2026-10-12: adds load
      balancers, Lambda, RDS, and network ACLs. Left: the scope, exclusion, and pruning
      decisions (added 2026-10-12)
- [x] draw.io: palette icons for Steampipe kinds (load balancer `elastic_load_balancing`,
      RDS `rds`, Lambda `lambda` resource icons; network ACL
      `network_access_control_list`), with the palette's fill, white stroke, and fixed
      aspect. Containers lay their children out in a near-square D2 grid instead of
      ELK's single row. Edges start from ELK's interior bends (route ends left out)
      and use `edgeStyle=orthogonalEdgeStyle;libavoidRouting=1;`, so draw.io treats the
      bends as router-owned and routes around shapes once a connected shape moves. Routing at generation time through
      the desktop CLI (`--layout orthogonalEdge`) runs after rendering when the app is
      found (`DRAWIO_BIN`, `PATH`, or the macOS location); it is skipped with a warning
      otherwise. ELK bends turned out empty: D2 routes every link that touches a grid as
      two points, so no waypoints come from the layout (2026-10-13).
      Later icon changes: RDS instance `rds_instance`, RDS cluster `rds` resource icon,
      network ACLs not collected (added 2026-10-12, done 2026-10-12)
- [ ] A read-only Steampipe database login for this tool (added 2026-10-12)
- [x] 🪦 Dropped: Add an inventory backend interface with SDK and Steampipe implementations,
      and a per-entry config choice of backend - the config choice landed with the `cloud`
      shape (`source`), and the platform layout replaces the interface (added 2026-10-12,
      done 2026-10-13)
- [x] Add the platform-neutral record fields and rewrite `model` to hide, prune, and
      build nodes, containment, and edges from them alone (added 2026-10-13, done
      2026-10-14)
- [x] Create `internal/platform/aws` from `internal/inventory/aws`, the AWS parts of
      `internal/inventory/steampipe`, `inventory.PermissionRules`, and the rules in
      `internal/model/ec2.go`, with `aws_sdk` and `aws_steampipe` source subpackages
      (added 2026-10-13, done 2026-10-14)
- [x] Set config `source` value `steampipe`: `config.SourceSteampipe`, the
      schema enum and default, the config tests, the README, and the
      account configs. The user did the code and configs; the README followed 2026-10-14
      (added 2026-10-13, done 2026-10-14)
- [x] Move `internal/inventory/steampipe` to `internal/steampipe` as a generic table
      reader. AWS parts go to `internal/platform/aws`; the scoping column and
      `_deleted_at` filter become parameters (added 2026-10-13, done 2026-10-14)

- [ ] 🧩 Refining: Finish the schema for the `cloud` shape - define or stub the four
      missing Azure `$defs`, move `minItems`/`uniqueItems` from `items` to the region
      arrays, add `minProperties: 1` to `cloud`, fix the `cloud` description typo, apply
      the account-array and key-name decisions (added 2026-10-12)
- [x] Change the config structs and `config.Read` to the `cloud` shape (one account
      per cloud key, `source` and `account_id` per account), drop the `--source` and
      `--account` flags, update the README config section (added 2026-10-12, done
      2026-10-12)
- [x] 🪦 Dropped: Move `internal/inventory` to `internal/inventory/aws` with no behavior
      change - the SDK collectors already sit there, and the target is now
      `internal/platform/aws` (added 2026-10-12, done 2026-10-13)
- [x] Replace `Node.VPC` and `Node.Subnet` with a `Parent` ID, and update both renderers
      to walk the parent chain (added 2026-10-12, done 2026-10-14)
- [ ] Add an account level to the model (`Diagram -> Account -> Region`), and draw
      account boxes in both renderers. Config takes an array of accounts per cloud key,
      with `config.Read` rejecting a repeated `account_id` (added 2026-10-12)
- [x] 🪦 Dropped: Move EC2 builders and selection rules into a per-cloud model package that
      registers kinds and rules with the core - replaced by the platform packages, which
      put the rules into record fields so nothing registers with `model` (added
      2026-10-12, done 2026-10-13)
- [ ] 🧩 Refining: Define the generation hierarchy - what a future `go generate` run
      produces and in what order (for example manifest or table mapping, then config
      types, then collectors, then JSON schema), what each step reads, and which outputs
      are committed. Depends on the generator decision (added 2026-10-12)
- [ ] 🧩 Refining: Build the generator under `internal/tools/`, wire `//go:generate` and a
      `devbox run generate` script, and add a test that fails when the committed schema
      differs from the generated one (added 2026-10-12)
- [ ] Run a GovCloud config end to end against `us-gov-west-1` and fix what breaks
      (added 2026-10-12)
- [ ] Update `AGENTS.md` and the README: platform package layout, platform-neutral records
      and model, the
      Resource Graph and AWS SDK decisions, the generator, and the schema no longer being
      hand-maintained (added 2026-10-12)
- [ ] Drop the `AWS::` prefix from resource types, so `AWS::EC2::NatGateway` becomes
      `EC2::NatGateway`: the API's service and type name. A type only appears within one
      cloud's records and config, so the prefix repeats `Platform`. Touches the type
      constants in `internal/platform/aws/aws.go`, the Steampipe `resourceTypes` map in
      `aws_steampipe/generic.go`, the draw.io icon map in `render/drawio/ec2.go`, doc
      comments in `inventory/record.go` and `model/model.go`, tests, and the
      DECISIONS.md entry "Resource types use CloudFormation names" (added 2026-10-14)
- [x] Azure resource types: use the ARM type in its documented casing, compared
      case-insensitively; the `Microsoft.` namespace is dropped (added 2026-10-14, done
      2026-10-14)

No PR until the feature is complete: AWS, Azure, and Cloudflare all draw (decided,
2026-10-14).

Stage two, Azure and Azure Government:

- [x] Azure inventory through Steampipe, in `internal/platform/azure/azure_steampipe`:
      virtual networks and their subnets, NSGs, NICs, public IPs, VMs, scale sets, disks,
      firewalls, private endpoints, key vaults, and `include_all`. Queries take `ids`,
      `resource_groups`, and `tags` (added 2026-10-12, done 2026-10-14)
- [x] Azure record builders and selection rules in `internal/platform/azure`
      (`Finish`), with tests (added 2026-10-12, done 2026-10-14)
- [x] Draw Azure kinds in draw.io with `img/lib/azure2/...svg` image paths; Cloudcraft
      output is refused for an Azure config (added 2026-10-12, done 2026-10-14)
- [x] Change the config structs and `config.Read` to accept `cloud.azure` and
      `cloud.azure_gov`, one account or subscription per config; `schema` gains Azure
      `include_all` and loses Key Vault keys and secrets (added 2026-10-14, done
      2026-10-14)
- [ ] Run an Azure config against the live Steampipe database and compare with the portal. The jsonb
      shapes come from the Steampipe plugin source (v1.16.0), not the live schema:
      `azure_firewall.ip_configurations` is flattened, the rest are ARM JSON (added
      2026-10-14)
- [ ] Run an Azure Government config end to end; the Steampipe database lists commercial
      tenants only (added 2026-10-14)
- [ ] Draw a private endpoint's connection to the resource it links to, such as a key
      vault (principle 5). Today the target is only in the endpoint's shape data
      (added 2026-10-14)
- [ ] Azure subnets have no zone, so the subnet grid is one column per VNet. Decide on
      a near-square grid for Azure, and whether a subnet gets a tier (Azure has no
      public-subnet flag; all draw in the private colors today) (added 2026-10-14)
- [ ] Steampipe database: add `azure_nat_gateway` and `azure_app_configuration`, which the
      config accepts but the database lacks; both are logged and skipped (added 2026-10-14)

Stage three, Cloudflare:

- [ ] 🌱 Backlog: Cloudflare inventory through Steampipe, in
      `internal/platform/cloudflare`. The Steampipe database holds only `cloudflare_zone`
      and `cloudflare_dns_record` today (added
      2026-10-14)
- [ ] 🌱 Backlog: Decide what a Cloudflare diagram shows: zones as containers holding DNS
      records, and whether records link to the AWS or Azure resources they point at
      (added 2026-10-14)
- [ ] 🌱 Backlog: Add the `cloud.cloudflare` config key and schema entry (added 2026-10-14)
- [ ] 🌱 Backlog: Draw Cloudflare kinds in draw.io (added 2026-10-14)

## Resume here

Updated 2026-10-14, after the Azure build. Azure draws in draw.io from Steampipe:
`internal/platform/azure` and `azure_steampipe`, config and schema for `cloud.azure` and
`cloud.azure_gov`, and Azure shapes in `render/drawio`. The user's open items: the Azure
build notes and `Rule.Deny` (decisions above), and a README update, since the
README still says Azure is not supported. The ELK layout now keys D2 items by position,
because Azure IDs hold dots; an AWS fixture rendered byte-identical before and after.
Sample output: `exports/claude/azure-platform/azure-sample.png`. Next after review:
the live Steampipe run, then Cloudflare.

Earlier, 2026-10-14, end of session. The platform restructure is built, tested, and pushed
on `feature/multi-cloud-support` (through `349411f`). No PR until Azure and Cloudflare
are done too. Since then: `KeepsParent`
became `Attached`, the user removed the coverage exclusion, and the user removed the `drawio/`
entry from `.gitignore`. Next:

1. Review the remaining record-field additions (`Hidden`, `HideChildren`,
   `Zone`, `Tier`, `Refs`; open decision above).
2. Approve the `AGENTS.md` and `docs/DESIGN.md` updates (the docs task above).
   Both still name `internal/inventory/aws`, `internal/model/ec2.go`, and
   `model_test.go`. `AGENTS.md` also still calls `drawio/` a git-ignored local clone,
   which no longer matches `.gitignore`.
3. `devbox run coverage` still fails on files the restructure did not touch:
   `render/render.go`, `render/cloudcraft/output.go`, `steampipe/postgres.go`, and
   `main.go`. The user planned a separate session to add tests for these.
4. `include_all` over every drained table, with exclusions by API resource type.
5. `origin/main` (`3db2d32`) does not build: it predates tracking
   `internal/render/drawio`. Merging this branch fixes it.

Parity check, 2026-10-14: no `PG*` variables were set, so the baseline came from a
hand-built SDK fixture (every EC2 type, a Lambda function, an S3 bucket) run through six
configs: default, global prune, hidden subnets and VPN connections, security group
hide_children with VPC pruning, hidden instances with pruning, and VPC hide_children.
draw.io and Cloudcraft output were byte-identical before and after. Outputs are in
`exports/claude/platform-restructure/`. The harness was a throwaway test and is not
committed. The user then ran the account A config against the Steampipe database from their environment
(2026-10-14): the draw.io output had no differences from the previous drawing.

## Notes

- The schema work started 2026-10-12, before this project. `cloud`, the per-cloud region
  enums, and the `awsAccount` base are in `schema/config.schema.json`. The Go config does
  not match yet, so `config.Read` rejects any config using `cloud`.
- Regions came from `aws ec2 describe-regions --all-regions` (per partition, with that
  partition's credentials) and
  `az account list-locations --query "[?metadata.regionType=='Physical'].name"`. The Azure
  commercial list includes canary and staging regions (`*euap`, `*stg`) that normal
  subscriptions cannot use. The Azure Government list was written from memory and has not
  been checked against a live login.
- Cloud Control IAM action names (`cloudformation:ListResources`,
  `cloudformation:GetResource`) are unverified. Check them before any Cloud Control work.
- Overlaps: `draw-collected-resources` (KMS, secrets, and parameters not drawn) touches
  `model.Build` and the renderers. The coverage project was dropped 2026-10-13.

- Convention review, held until the design settles (2026-10-13): source subpackages
  `aws_sdk` and `aws_steampipe` -> `sdk` and `steampipe`, with an import alias where
  `platform/aws/steampipe` imports the generic reader. Go package names avoid
  underscores.

- 2026-10-14 restructure, behavior changes outside the parity fixture: a record's
  `Parent` is its subnet or VPC only when that container was collected, so Cloudcraft no
  longer places a resource in a VPC the config filtered out. A kept record whose parent
  is pruned (an internet gateway shown by override in a pruned VPC) moves up to the next
  kept container. A Steampipe subnet with a NULL `map_public_ip_on_launch` now reads as
  `"false"` rather than `""`. `ec2Filters` and `scrubVPNSecrets` stayed in `aws_sdk`:
  both belong to SDK calls, and the SDK test seam covers them.

- 2026-10-13: AWS SDK collectors now return records like the Steampipe reader. Their SDK
  data lives in an unexported `collected` struct in `internal/inventory/aws`, converted
  to records after collection; `inventory.Region` is `Name` and `Records`; `model`
  reads only records and imports no cloud SDK. Prefix list entries are no longer
  fetched. KMS keys, secrets, and SSM parameters, when named, now become records and
  draw as plain boxes.
