<!-- Thanks for contributing! Please fill this out so reviewers have context. -->

## Description

<!-- What does this PR do and why? -->

Fixes #<!-- issue number, if any -->

## Type of change

- [ ] Bug fix
- [ ] New resource or data source
- [ ] Enhancement to an existing resource/data source
- [ ] Documentation only
- [ ] Chore / CI / refactor

## Checklist

- [ ] `make fmt`, `make lint`, and `make test` pass locally
- [ ] `make generate` was run and the resulting `docs/` changes are committed
- [ ] A `CHANGELOG.md` entry was added under `## X.Y.Z (Unreleased)` (operator-impacting changes, prefixed with the resource name)
- [ ] Tests were added/updated following the project's TDD policy — for
      resources this includes a basic lifecycle test, a `_disappears` test, and
      `RequiresReplace` plan checks for immutable fields
- [ ] Every new or changed attribute has a `MarkdownDescription` documenting
      defaults, valid values, and any replace/destroy semantics (tfplugindocs
      does not render plan modifiers, so this must live in the description)
- [ ] Changes work under both Terraform and OpenTofu

## Notes for reviewers

<!-- Anything reviewers should focus on, trade-offs, follow-ups, etc. -->
