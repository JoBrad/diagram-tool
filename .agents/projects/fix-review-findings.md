---
type: project
status: refining
created: 2026-10-02
tags: [bug, review]
---

# Fix the correctness bugs found in the 2026-10-02 review

A code review on 2026-10-02 found bugs in output naming, label escaping, config validation,
and duplicate blueprint names. None loses AWS data, but several produce wrong or broken
output from ordinary input: a `/` in a diagram name, a `<` in a Name tag, two blueprints
with the same name. Each fix ships with a test that fails before it. Done means every task
below is closed and the repo's checks in `AGENTS.md` pass.

## Decisions

- [ ] `export` with two blueprints of the same name: both map to `<name>.<format>`. Without
      `--overwrite` the second fails as "file already exists"; with it, the second silently
      replaces the first. Pick a disambiguation: suffix the blueprint ID on every file,
      suffix it only on collisions, or fail the colliding names up front.
- [ ] `create --cloudcraft` when the account already has several blueprints with the target
      name: `Output.find` takes the first match, so `--overwrite` updates an arbitrary one.
      Pick: fail and list the matching IDs, or keep first-match and log a warning.

## Tasks

- [ ] HTML-escape draw.io labels. Leaf and group cells use `html=1`, and `htmlLines` only
      turns newlines into `<br>`. A Name tag containing `<` or `&` is read as markup, so the
      label renders wrong. Escape with `html.EscapeString` before `htmlLines`, for leaf,
      VPC, subnet, region, and prefix-list labels (`internal/render/drawio/ec2.go`).
- [ ] Sanitize output file names. `cloudcraft.FileName` and `drawio.FileName` only trim
      whitespace, so `--name team/app` fails with "no such file" and `--name ../x` writes
      outside `--export-path`. `exportFileName` already replaces separators; move that into
      one helper in `internal/render` and use it for all three.
- [ ] `nameTag` returns `""` for a Name tag with an empty value, which draws a blank label
      and `" (sg-...)"` group names. Fall back to the ID when the value is empty
      (`internal/model/ec2.go:290`).
- [ ] `config.Read` accepts duplicate and empty entries in `regions`. A duplicate region is
      collected twice and produces duplicate component IDs in Cloudcraft and a merged
      container in draw.io. Reject both, and add the rule to the schema (`uniqueItems`,
      `minLength`).
- [ ] `schema/config.schema.json`: `resources` lacks `additionalProperties: false`, unlike
      every other object. `config.Read` rejects unknown keys there, so the schema should
      too.
- [ ] draw.io files carry `host="cloudcraft"` in their `mxfile` element, a leftover from
      the tool's earlier name (`internal/render/drawio/drawio.go`). Set it to the tool's
      name (added 2026-10-14)
- [ ] Resolve the `export` duplicate-name decision above and implement it.
- [ ] Resolve the duplicate-name upload decision above and implement it.

## Notes

- Findings verified by reading the code on 2026-10-02. The label-escaping finding relies on
  draw.io treating an `html=1` label value as HTML; no file with a `<` in a name has been
  rendered yet. Confirm with a PNG export from the desktop app (command in `AGENTS.md`).
