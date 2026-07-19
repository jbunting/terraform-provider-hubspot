# Contributing to terraform-provider-hubspot

## Development environment

- Go 1.25+ (see `go.mod`)
- Terraform >= 1.0 or OpenTofu >= 1.6 on PATH (acceptance tests auto-download one unless `TF_ACC_TERRAFORM_PATH` is set)
- No HubSpot account needed for development: the acceptance suite is hermetic (runs against the in-memory fake in `internal/provider/fake_hubspot_test.go`)

## Workflow: TDD is mandatory

This repo follows the policy in `dev-docs/research/08-tdd.md`:

1. **New resource**: write the desired HCL in `examples/resources/hubspot_<name>/resource.tf` first, then the red lifecycle acceptance test against the fake, then implement until green. The failing test is the design-review artifact.
2. **Bug fix**: two commits — the failing regression test first, then the fix.
3. Every resource needs: basic (create/re-plan-empty/update/import) test, a `_disappears` test, and RequiresReplace plan checks for immutable attributes.
4. Pure conversion logic (flatten/expand, normalizers) gets table/property tests before implementation.

## Commands

```sh
make build       # go build ./...
make test        # unit + hermetic acceptance tests, no credentials
make lint        # golangci-lint
make fmt         # gofmt
make generate    # regenerate registry docs (tfplugindocs) — commit the diff
make testacc     # full acceptance run (TF_ACC=1)

# one resource's tests, fast loop:
TF_ACC_TERRAFORM_PATH=$(which terraform) go test ./internal/provider/ -run 'TestAccProperty_' -count=1 -v
```

## Pull requests

- Run `make fmt`, `make lint`, `make test`, and `make generate` (commit any docs diff) before opening a PR.
- Add a CHANGELOG entry under `## X.Y.Z (Unreleased)` for operator-impacting changes only, prefixed with the resource name.
- Every schema attribute needs a `MarkdownDescription` stating defaults, valid values, and replace semantics (plan modifiers are not rendered in docs — write them out).
- One resource per PR where possible.

## Design context

Read `CLAUDE.md` and `dev-docs/` before proposing structural changes — resource semantics (archive-vs-delete, import ID formats, immutability) are researched decisions, documented in `dev-docs/design/resource-model.md`.
