# Changelog

Major changes to this project. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). `Unreleased` lists changes on
the working branch that are not yet merged to `main`.

## [Unreleased]

## [0.1.1] - 2026-11-17

### Added

- Design principles for story, containment, connector direction, and readable color,
  with contrast thresholds of 3:1 for lines and 4.5:1 for text.
- draw.io labels use Roboto, with literal values such as addresses, CIDRs, and ports in
  Roboto Mono.

- `docs/DESIGN.md`, generated from the code, and `docs/DECISIONS.md`, which records the
  reason behind each design decision. The design doc was `docs/design.md`.
- `docs/ROADMAP.md`, the stages the tool is heading through.
- Design principles at the top of `docs/DECISIONS.md`, with a Goals section in the
  README that points to them.
- Test seams under the Steampipe connection, the `export` command's Cloudcraft client,
  and `create`'s Steampipe source. `main.go`, `internal/steampipe`, and Cloudcraft output
  setup now have tests.

### Changed

- draw.io default colors are one hue per tier, chosen to stay apart for color vision
  deficiencies, and neighboring subnets alternate tints. The default rule color is light
  blue, not red. Colors too light to read are drawn in a darker shade.
- draw.io labels with no color of their own use a dark charcoal, `#374151`, in place of
  AWS's `#232F3E` and the light gray VPC label.

- AWS knowledge moved out of `internal/model` into `internal/platform/aws`, with the live
  SDK and Steampipe sources as its subpackages `aws_sdk` and `aws_steampipe`. Both sources
  feed the same record builders.
- Records carry platform-neutral placement and selection fields (`Parent`, `KeepIf`,
  `Attached`, `Prunable`, `Hidden`, `HideChildren`, `Zone`, `Tier`). The model hides and
  prunes from those fields alone and reads no config.
- `internal/steampipe` is a generic Steampipe table reader for any platform, with the
  account column and soft-delete filter as parameters.
- `main` only calls `os.Exit` on the result of `run`, which holds the command dispatch.
- Agent instructions moved from `CLAUDE.md` to `AGENTS.md`. `CLAUDE.md` imports it.
- `README.md` and `AGENTS.md` describe the platform package layout. The README names the
  tool `diagram`, documents `include_all`, and drops the nonexistent `--drawio` alias.

### Removed

- Coverage exclusions in `.testcoverage.yml`.
- The `drawio/` ignore rule. Ignore rules for `exports/` are anchored to the repo root.

### Fixed

- `internal/render/drawio` is tracked in git. `main` does not build without it.

[Unreleased]: https://github.com/JoBrad/diagram-tool/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/JoBrad/diagram-tool/releases/tag/v0.1.1
