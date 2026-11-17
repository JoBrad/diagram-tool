---
type: project
status: refining
created: 2026-10-13
tags: [drawio, layout, design]
branch: feature/beautify-diagrams
---

# Make draw.io diagrams read clearly at a glance

Generated draw.io diagrams now hold every resource, but they read as a dump rather than
an architecture. Security groups have no visible shape, resources outside VPCs scatter
across the region, and nothing shows how traffic enters the account. This project
styles security groups and other items with a consistent color scheme, lays out each
container level deliberately (region first, then VPC, then subnet), and orders resources
top to bottom by exposure, from internet-facing to internal. Done means a reader can
trace the public entry points down to the data tier without reading labels, and the
repo's checks in `AGENTS.md` pass.

## Decisions

- [x] 2026-10-13 - Security groups are drawn as containers, one box per group per
      subnet (or VPC, for resources in no subnet), nested by member set: B inside A when
      B's members are a subset of A's; groups with identical members in a container share
      one box; on partial overlap, larger groups (then more rules) are placed first and
      the later group keeps only the members that sit together. Every resource keeps all
      its groups in its cell data. Style: the AWS palette security group plus
      `container=1;pointerEvents=0;collapsible=0;recursiveResize=0;`. Fleet data
      (2026-10-13, 128 accounts): of 188 overlapping group pairs, 135 nest and 53
      partially overlap; test accounts C, A, and D.
- [x] 2026-10-13 - Removed the "security groups are not containers" invariant from
      `AGENTS.md`. How shapes draw is a product decision, not a steering rule; it belongs
      in a product spec, if anywhere.
- [x] 2026-10-13 - Color scheme: colors are per subnet, configured per tier (public,
      private, isolated, each one or more colors) with per-resource overrides by ID.
      Connectors take the color of where they originate. Defaults come from the AWS
      palette; users can supply their own.
- [x] 2026-10-13 - Canvas order: left to right, the most contained first (subnets, then
      VPC-level, then region-level, then global); top to bottom, public then private.
      Inside a VPC, subnets form a grid with Availability Zones as columns and exposure
      as rows. Region-level resources go in an invisible layout block to the right of
      the VPCs.
- [x] 2026-10-13 - Subnet tiers, first match wins (replaces the two-tier exposure
      decision): (0) routes not collected: launch flag means public, else private
      (temporary); (1) `0/0` directly to an internet gateway: public; (2) any of its
      resources has a public IP: public; (3) `0/0` or `::/0` to anything other than an
      internet gateway (NAT gateway or instance, transit gateway, VPN, firewall or GWLB
      endpoint, egress-only internet gateway): private; (4) otherwise isolated. A subnet
      whose only outside routes are specific prefixes (another VPC, on-premises) is
      isolated. Azure maps the same way, with public set per resource.
- [x] 2026-10-13 - Exposure comes from the subnet tier rules only. No per-service
      flags such as RDS `publicly_accessible`; a resource's own exposure can be shown
      through its security groups.
- [x] 2026-10-13 - Grid stretching stays: every cell in a zone column takes the widest
      subnet's width, so small subnets draw wide. Content-fitted boxes were considered
      and not taken.
- [x] 2026-10-13 - Terms: a resource (an EC2 instance) has a resource type
      (`AWS::EC2::Instance`, `Microsoft.Compute/virtualMachines`), provided by a service
      (Amazon EC2; in Azure, a resource provider such as `Microsoft.Compute`). Our
      reader-facing grouping is a category: edge (networking that manages ingress or
      egress: load balancers, CloudFront, API Gateway, NAT gateways, firewalls), compute,
      data, other. "Kind" is not used.
- [x] 2026-10-13 - Categorization, first match wins: (1) `resource_category_overrides`
      by resource ID; (2) `resource_categories` by resource type, from an optional file;
      (3) built-in resource types; (4) built-in services, read from the resource type
      identifier (`AWS::<Service>::...`, `Microsoft.<Provider>/...`) or the Steampipe
      table prefix, seeded from AWS's service categories with Networking split into edge
      and other; (5) security groups allowing inbound from `0.0.0.0/0` or `::/0` mean
      edge; (6) otherwise other, logged. Resource types use the AWS and Azure CLI
      identifiers. File shape: `{"resource_categories": {"edge": [...], "compute":
      [...], "data": [...]}, "resource_category_overrides": {"edge": ["i-..."]}}`.
      They live in `options` (app-level file, overridable per diagram).
- [x] 2026-10-13 - Global items (Route 53, CloudFront; IAM will not be modeled) go far
      right, past the regions. Connectors from them to S3 buckets, Lambda functions, and
      other targets are wanted. Nothing global is collected yet.
- [x] 2026-10-12 - Unconnected items: `show_unconnected_resources: false` prunes every
      container that holds nothing kept; instances and generic resources stay.
- [x] 2026-10-12 - Containers lay their children out in a near-square D2 grid instead of
      ELK's single row.
- [x] 2026-10-13 - Connectors: orthogonal and flagged `libavoidRouting=1`, routed around
      shapes at generation time by the draw.io desktop app when it is installed, and
      rerouted live by draw.io when a shape moves.
- [x] 2026-10-16 - Default colors replace the AWS palette: one hue per tier (green,
      blue, rose) from the iso-diagram skill's Okabe-Ito palette, light blue for rules
      instead of red, and neighboring subnets alternate tints in a checkerboard. Colors
      under 3:1 (lines) or 4.5:1 (text) are darkened. Labels use Roboto, literal values
      Roboto Mono, from Google Fonts. Follows the principles rewritten the same day in
      `docs/DECISIONS.md`. Branch: `initial-push`.

## Tasks

- [x] Suppress unconnected items with `show_unconnected_resources` (added 2026-10-13,
      done 2026-10-12)
- [x] Lay out items mostly well: grids in containers, verified palette icons for Steampipe
      kinds (added 2026-10-13, done 2026-10-12)
- [x] Route connectors: no stuck waypoints, libavoid routing through the desktop app
      (added 2026-10-13, done 2026-10-13)
- [x] Draw security groups as nested boxes per subnet (`render/drawio/sg.go`); account A
      draws 96 boxes, 20 nested, 147 of 193 resources inside a group (added 2026-10-13,
      done 2026-10-13)
- [x] Security group boxes are dashed, and rule connectors join boxes instead of
      first members: every box of the receiving group gets an edge, from the source
      group's box in the same subnet, else its first box; groups with no box fall back
      to their first member (`render/drawio/rules.go`). account A: 94 box-to-box edges,
      account B: 68 (added 2026-10-13, done 2026-10-13)
- [x] `hide` and `hide_children` on each resource query (`model/hide.go`), applied
      before selection: hidden containers pass their contents up a level. Hiding subnets
      draws security groups per VPC; account A then has 89 group boxes, 61 directly in a VPC
      and 28 nested (added 2026-10-13, done 2026-10-13)
- [x] Color schemes: `diagram.colors` lists one or more `#RRGGBB` colors per tier
      (`public`, `private`, `isolated`), rotated across that tier's subnets, plus
      `resources` (ID to color) for a subnet, VPC, or security group. Subnets take their
      color as outline, a darker label, and a 90% tint as fill. Rule connectors take the
      color of the subnet or colored resource they start from. Defaults from the AWS
      palette (`render/drawio/colors.go`). Account A: 26 subnets in 5 colors, 94 connectors
      colored by origin (added 2026-10-13, done 2026-10-13)
- [ ] Collect route tables (`aws_vpc_route_table` in Steampipe, `DescribeRouteTables`
      for the SDK source) and classify subnets by the tier rules, so `isolated` and rule
      (1) can apply (added 2026-10-13)
- [x] Container layout, stage 1 - region level: each region is one row, its VPCs then
      a layout-only block for what sits in the region itself (Lambda functions outside
      VPCs, prefix lists, customer gateways), drawn directly in the region
      (`render/drawio/graph.go`) (added 2026-10-13, done 2026-10-13)
- [x] Container layout, stage 2 - VPC level: each VPC is one row, a layout-only subnet
      grid (Availability Zones as columns, public rows above private, spacer cells where
      a zone has fewer subnets in a band) then a layout-only block for VPC-level
      resources. Resources with a public IP, and their security group boxes, sort first.
      Grids now fill row by row: D2 had been filling every grid column by column
      (added 2026-10-13, done 2026-10-13)
- [x] App-level options: optional `$XDG_CONFIG_HOME/diagramming/config.json` (else
      `os.UserConfigDir()/diagramming`) holds `options` for every diagram; a diagram's
      `options` override them. `colors` moved from `diagram` to `options`;
      `show_unconnected_resources` defaults to false. `internal/config` split into
      `constants.go`, `config.go` (options, colors), and `diagram.go`; app schema in
      `schema/app-config.schema.json` (added 2026-10-13, done 2026-10-13)
- [x] Rename the code's resource-type concept to match the terms: `ResourceType` with
      CloudFormation identifiers (`AWS::EC2::Instance`), `Relation` for edges,
      `TargetType` for rule targets, cell data `type`; Steampipe tables map to resource types
      (`aws_lambda_function` to `AWS::Lambda::Function`). SDK collectors moved to
      `internal/inventory/aws`; shared types stay in `internal/inventory`
      (added 2026-10-13, done 2026-10-13)
- [ ] Categorize resources and lay each container out in category bands, stacked top
      to bottom: edge, compute, data, other. Within a band, order by connection, not
      exposure: each resource sits near what it is connected to (compute by the position
      of the edge resource that reaches it, data by the compute that reaches it).
      Security group boxes take their highest-category member's band, and boxes and loose
      resources sort together. This replaces the public-first sort inside containers
      (added 2026-10-13)
- [ ] 🌱 Backlog: Container layout, stage 3 - subnet level, keeping related resources
      (compute and its database) close so the connector between them stays short and
      direct (added 2026-10-13)
- [ ] 🧩 Refining: Place resources by exposure, top to bottom from public-facing to
      internal. Depends on the exposure tier decision (added 2026-10-13)
- [ ] Keep tier colors distinct: subnets rotate through their tier's color list, and
      nothing stops a color from appearing in two tiers' lists, so a public and a private
      subnet can share a color. Enforce distinct tier lists (added 2026-10-14)

## Notes

- The completed items were built under `multi-cloud-support` on 2026-10-12 and
  2026-10-13; they are listed here as this project's starting point.
- Layout limits found so far: D2 routes every link that touches a grid as a straight
  two-point line, so the layout supplies no bends; the desktop app's libavoid pass does
  the routing. A grid widens each cell to its column, so leaves can come back larger
  than requested.
- Cloudcraft keeps its own fixed grid, which `AGENTS.md` says not to tune. This project
  covers draw.io output only.
