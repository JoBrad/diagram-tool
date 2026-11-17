---
type: project
status: backlog
created: 2026-10-02
tags: [feature, drawio, cloudcraft]
---

# Draw the resources and rules that are collected but never shown

KMS keys, Secrets Manager secrets, and SSM parameters are collected when a config names
them, but `model.Build` only calls `addEC2`, so they never reach either renderer. The
README says they are "not drawn yet". Separately, draw.io shows only inbound rules that
target a security group or prefix list; rules with CIDR targets appear nowhere in the
draw.io file, while Cloudcraft carries them on its `sg` groups. Done means each collected
type is drawn in both outputs, or deliberately left out with the reason in the README.

## Decisions

- [ ] How KMS keys, secrets, and parameters appear: region-level nodes, or attached to the
      resources that use them (which needs IAM or resource-policy data not collected
      today). Check Cloudcraft component types and the `aws4` shape names before choosing.
- [ ] Until they are drawn, should the three collectors still run? Today a config that
      names them spends API calls for no output.
- [ ] How CIDR-target rules appear in draw.io: cell data on member instances, a rules table
      per security group, or edges from a shared "internet" or CIDR node.

## Tasks

- [ ] 🧩 Refining: Add KMS, Secrets Manager, and SSM kinds to `model`, with selection
      rules and `model_test.go` cases.
- [ ] 🧩 Refining: Draw them in `render/cloudcraft`.
- [ ] 🧩 Refining: Draw them in `render/drawio`.
- [ ] 🧩 Refining: Show CIDR-target security group rules in draw.io.
