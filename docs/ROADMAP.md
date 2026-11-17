# Roadmap

Where the tool is going, in stages. Each stage links to the project files that carry it
out, which hold the tasks, decisions, and open questions. [DESIGN.md](DESIGN.md)
describes what is built today, and [CHANGELOG.md](../CHANGELOG.md) lists what changed.

This roadmap holds until the MVP ships, then gets revisited.

## 1. Single-platform diagrams - in progress

Consistent, useful diagrams of one platform at a time: AWS (commercial and GovCloud),
Azure (commercial and Government), and Cloudflare. AWS draws today. Azure and Cloudflare
are next.

- [Multi-cloud support](../.agents/projects/multi-cloud-support.md): config, platform
  packages, and sources for each platform.
- [Beautify diagrams](../.agents/projects/beautify-diagrams.md): layout, color, and
  readability of draw.io output.
- [Draw collected resources](../.agents/projects/draw-collected-resources.md): resources
  and rules that are collected but not yet drawn.

## 2. Multi-platform diagrams - next

One diagram holding resources from one or more platforms and accounts. This needs an
account level in the model, which is a task in
[multi-cloud support](../.agents/projects/multi-cloud-support.md). No project of its own
yet.

## 3. Additional views - later

Diagram types that show different views of the same resources. No project yet.

- **Network view:** close to today's diagram, without security groups.
- **Security view:** only regions, VPCs or VNets, security groups, routes, and compute,
  data, and edge resources, focused on allowed ingress and egress. Drawing routes here
  would revise the "Drawing routes as connectors" entry in
  [DECISIONS.md](DECISIONS.md#drawing-routes-as-connectors).
