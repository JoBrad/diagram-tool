---
type: project
status: refining
created: 2026-11-17
tags: [feature, aws, resource-types, drawio, schema]
branch: feature/compute-data-storage
---

# Collect and draw AWS compute, data, and storage resources from both sources

Version 0.1.1 draws EC2 networking, instances, and the secret stores. Most of what runs in
an account (functions, containers, databases, caches, file systems, buckets) is missing
unless the Steampipe source's `include_all` picks it up as a plain box. This project adds
those types to both the SDK and Steampipe sources, each with a hand-written builder that
declares its references, a type registry that drives the config keys and the generated
schema, and references that resolve whether the API gave an ID, an ARN, or a name. Done
means every type listed under Tasks collects from both sources where the source holds it,
draws in draw.io, has tests, the schema is generated from the registry, and the repo's
checks in `AGENTS.md` pass. Ships as v0.2.0.

## Decisions

- [x] 2026-11-17 - How connections are defined: in Go code, not in JSON or other config
      specs. A config spec moves the verbosity into files people edit by hand and will get
      wrong, and buys one generic parser at the cost of a second language.
- [x] 2026-11-17 - Each builder declares its references as a `[]RefSpec[T]` (`Relation`,
      `Target`, `Field`, `Note`, and a typed `Values func(T) []string`) and fills `Refs`
      with `buildRefs`. The list reads on its own, and one generic test can walk every
      type's declarations. Review checks are in `AGENTS.md` under "Declared references".
- [x] 2026-11-17 - Generated docs from the declarations: deferred. Revisit when the types
      land, if the README needs a supported-resources table.
- [x] 2026-11-17 - Config keys: a registry-backed map. `config.Resources` holds
      `service -> type -> query`, a registry in `internal/platform/aws` lists the supported
      types, and a platform validation step rejects unknown keys before any AWS call.
      Config files keep their shape. Adding a type means one registry entry, not a struct.
- [x] 2026-11-17 - The JSON schema is generated from the registry. Hand-editing it per type
      does not scale to this many types.
- [x] 2026-11-17 - References resolve through a per-type alias index in `Finish`. Each
      record has a canonical ID (the short ID for EC2 types, else the ARN) and lists its
      other forms (ARN, ID, name) as aliases. Refs carry whatever form the API gave and are
      rewritten to the canonical ID, so an ARN ref and an ID ref land on the same node. An
      unresolved ref is logged and dropped. Replaces the idea of a `Key` field on
      `inventory.Ref`.
- [x] 2026-11-17 - Resources with no VPC sit in the container they belong to (VPC,
      account, or region), grouped by service, and then follow the canvas-order rules in
      `beautify-diagrams.md`.
- [x] 2026-11-17 - New types draw in draw.io only. Cloudcraft support will be dropped
      (see "Extending the Cloudcraft output" in `docs/DECISIONS.md`).
- [x] 2026-11-17 - Clusters are drawn without their members: an ECS cluster without its
      services and tasks, an EKS cluster without its node groups, an RDS cluster without its
      instances. Load balancers do not draw target groups.
- [x] 2026-11-17 - A new `connects_to` relation, drawn as an arrow, runs from a load
      balancer to what its target groups reach: an EC2 instance, an Auto Scaling group, or
      an ECS cluster. Target groups are collected only to resolve the arrow.
- [x] 2026-11-17 - DocumentDB and Neptune clusters are drawn as RDS clusters, with the
      engine in the label.
- [x] 2026-11-17 - A type the configured source does not hold is not drawn. When a
      Steampipe database has no table for a type (possibly DSQL or Storage Gateway), the
      run logs it and continues, per "Omissions are logged, not silent".
- [x] 2026-11-17 - Timestream: dropped from the list.
- [x] 2026-11-17 - Reachability (evaluating routes, NACLs, and security groups) is out of
      scope. The tool stays a diagrammer.
- [x] 2026-11-17 - Connections that exist only in IAM policies, environment variables, or
      code are not drawn. The collectors cannot see them reliably.
- [x] 2026-11-17 - Every new type is primary: collected only when the config names its
      key, like instances. No new container types. Clusters (RDS, ECS, EKS) are primary
      leaf nodes, and their members (ECS services and tasks, EKS node groups) are not
      collected. RDS instances outside a cluster are their own primary type. As containers,
      clusters would be collected for every config and pruned when empty, which every
      cluster would be. Members inside clusters can come later, with a registry rule that
      collects a container type only when its service is named.
- [x] 2026-11-17 - When a resource belongs to a collected group or cluster, the group is
      drawn instead of its members: an Auto Scaling group instead of its instances, an RDS
      cluster instead of its DB instances, an ElastiCache replication group instead of its
      nodes, and so on. Load balancer arrows land on the group. Membership is a `part_of`
      relation, separate from `member_of` (security groups), declared in each builder's
      `RefSpec` list. `Finish` hides any record with a `part_of` ref to a collected record.
      An EC2 instance is part of a group when it carries the `aws:autoscaling:groupName`
      tag or appears in the group's `Instances`.
- [x] 2026-11-17 - Nested groups: the outermost collected group is drawn. A Beanstalk
      environment owns an Auto Scaling group, which owns instances, and an EKS managed node
      group runs on an Auto Scaling group. With both levels collected, only the outer one
      shows. `Finish` follows `part_of` refs up to the outermost collected record.
- [x] 2026-11-17 - `include_members` on a group type's query (`ResourceQuery`, beside
      `hide_children`) draws each group as a box with its members inside, instead of one
      node. Load balancer arrows still land on the group. One flag covers one level: a
      Beanstalk environment with the flag holds its Auto Scaling groups, which hold no
      instances unless their own type has the flag too. The flag collects the member type
      for those groups, so it works without naming the member type. The registry marks
      which types are groups, and validation rejects the flag on any other type. Named
      `include_members`, not `expand_members`, because the box opens folded (below), and
      "expand" would suggest it opens unfolded.
- [x] 2026-11-17 - In draw.io, a group with the flag on is a collapsible container
      (`collapsible=1`) that opens folded (`collapsed="1"`). The layout is computed for the
      folded box, and the members' layout is stored as the alternate bounds. A reader
      clicks + to show the members, and the box grows over its neighbors. For the MVP that
      overlap is accepted. Groups without the flag stay plain nodes.
- [x] 2026-11-17 - Linked detail pages (a page per group, reached by clicking the group)
      are not part of this project. When and where to use them has not been worked out.
- [x] 2026-11-17 - A group with `include_members` whose members span subnets is one folded
      box, placed by "A resource in several subnets sits in the first by zone". Load
      balancer arrows land on that box. A member that runs in a different subnet from the
      box gets its subnet's name (falling back to its ID) as a second label line, in
      parentheses:

      ```
      web-01
      (private-b)
      ```

      One box per subnet was rejected because the group would draw as several boxes. An
      overlay rectangle across the subnets, as in AWS reference diagrams, was rejected
      because an overlay is not a container, so it cannot fold. draw.io gives every cell
      exactly one parent, so a member cannot sit in its subnet and its group at once.
- [x] 2026-11-17 - Primary types are always kept, not only instances. The rule exists so
      the compute the config asks for is always drawn; pruning removes context, never the
      subject. The registry's primary flag sets it. Edits the "Listed IDs are always
      drawn" entry in `docs/DECISIONS.md` and the selection rules in `AGENTS.md`.
- [x] 2026-11-17 - Members are hidden only when their group is collected. A config that
      names `instances` but not Auto Scaling groups draws every instance, so no compute
      goes missing.
- [x] 2026-11-17 - A member listed by ID is drawn even when its group is collected. Listed
      IDs are never filtered.
- [x] 2026-11-17 - New types are checked against Moto, run in a container that
      `testcontainers-go` starts from an integration test suite behind the `integration`
      build tag. Unit tests keep the interface fakes, and `devbox run test` does not need
      Docker. Moto covers types the owner's accounts do not have.

## Tasks

Shared work, before the types:

- [ ] Type registry in `internal/platform/aws`, the registry-backed `config.Resources` map,
      and the platform validation step. Move the existing EC2, KMS, Secrets Manager, and
      SSM types onto it (added 2026-11-17)
- [ ] Generate `schema/config.schema.json` from the registry, and update the "Config and
      schema" section of `AGENTS.md` and the "Unknown config keys fail the read" entry in
      `docs/DECISIONS.md` (added 2026-11-17)
- [ ] `RefSpec[T]`, `buildRefs`, its helpers, and the generic test that walks every
      declaration. Move the existing builders onto it (added 2026-11-17)
- [ ] `Record.Aliases` and the alias index in `Finish`, with the unresolved-ref warning
      (added 2026-11-17)
- [ ] `connects_to` relation, drawn as an arrow in draw.io (added 2026-11-17)
- [ ] Placement for resources with no VPC: grouped by service in their account or region
      (added 2026-11-17)
- [ ] Load balancers and target groups, for the `connects_to` arrows (added 2026-11-17)
- [ ] Integration test suite: `testcontainers-go` starts Moto, collectors run against it,
      gated by `//go:build integration`. Add a devbox script for it, and check which types
      Moto lacks (possibly DSQL, Lightsail, Storage Gateway, FSx) (added 2026-11-17)

Compute:

- [ ] Lambda functions (added 2026-11-17)
- [ ] ECS clusters (added 2026-11-17)
- [ ] EKS clusters (added 2026-11-17)
- [ ] Batch compute environments (added 2026-11-17)
- [ ] Auto Scaling groups (added 2026-11-17)
- [ ] Elastic Beanstalk environments (added 2026-11-17)
- [ ] Lightsail instances (added 2026-11-17)

Data:

- [ ] RDS instances and clusters, including DocumentDB and Neptune (added 2026-11-17)
- [ ] Aurora DSQL clusters (added 2026-11-17)
- [ ] DynamoDB tables (added 2026-11-17)
- [ ] ElastiCache clusters and replication groups (added 2026-11-17)
- [ ] Redshift clusters (added 2026-11-17)
- [ ] OpenSearch domains (added 2026-11-17)
- [ ] Glue jobs and connections (added 2026-11-17)

Storage:

- [ ] S3 buckets (added 2026-11-17)
- [ ] EBS volumes (added 2026-11-17)
- [ ] EFS file systems (added 2026-11-17)
- [ ] FSx file systems (added 2026-11-17)
- [ ] Storage Gateway gateways (added 2026-11-17)
- [ ] AWS Backup plans and vaults (added 2026-11-17)

Release:

- [ ] Move the design decisions above into `docs/DECISIONS.md` as the code lands (added
      2026-11-17)
- [ ] Update `README.md` and `CHANGELOG.md`, then tag `v0.2.0` (added 2026-11-17)

## Notes

- Prior art reviewed 2026-11-17. Cartography (CNCF, Apache-2.0) declares relationships as
  Python dataclasses read by a generic loader, the model for `RefSpec`. Its AWS placement
  edges are missing for Lambda, EKS, and ElastiCache. Powerpipe's
  `steampipe-mod-aws-insights` (Apache-2.0) stores each edge as a SQL query over Steampipe
  tables, which makes it a good source for column names and JSON paths. CloudFormation
  resource schemas use `relationshipRef` in only 27 of 1,894 types, so they are not a
  source of references.
- Tests stop at the SDK boundary, as in `AGENTS.md`: fakes for the SDK source and
  hand-built rows for Steampipe. The SDK's Go types and Steampipe's table docs define the
  data shapes. Live checks catch fields that are documented but empty in practice.
- DescribeDBInstances returns the subnet group with its subnets, so RDS needs no lookup.
  ElastiCache gives only `CacheSubnetGroupName`, and an Aurora instance gives
  `DBClusterIdentifier`. Those resolve through the alias index.
