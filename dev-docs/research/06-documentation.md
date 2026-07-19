# Documenting the Provider: Best Practices (researched July 2026)

Registry-facing docs are **generated** (tfplugindocs) from three inputs: live schema descriptions, `examples/`, and optional `templates/`. Everything else (guides, README, design docs) is hand-written. This doc sets the conventions.

## 1. tfplugindocs mechanics (v0.25)

- **`generate`**: builds the provider, pulls schema via `terraform providers schema -json`, merges with `templates/` + `examples/`, renders `docs/`. Use `--providers-schema <json>` in CI to skip the build/pin behavior. Requires Terraform ≥1.11 on PATH for write-only-attribute rendering.
- **`validate`**: registry-rule checks — directory layout, frontmatter, **500KB/file limit**, and `FileMismatchCheck` (schema entities ↔ doc files parity; catches undocumented resources).
- **Default templates cover ~95% of pages.** Datadog has only ~4 custom resource templates out of hundreds. Write custom `.md.tmpl` only for: the provider index, resources needing multi-scenario examples or warning callouts, and guides. Template functions: `{{tffile "path.tf"}}`, `{{codefile "shell" "path"}}`; variables: `.SchemaMarkdown`, `.Name`, `.HasImport`, `.ImportFile`, `.ProviderShortName`.
- **Registry layout**: `docs/index.md`, `docs/resources/<name>.md` (no provider prefix in filenames), `docs/data-sources/`, `docs/guides/`, `docs/functions/`, `docs/ephemeral-resources/`. Frontmatter: `page_title` (required for guides), `subcategory` ("Beta"/"Deprecated" auto-sort last), `description`.
- **Anything outside recognized directories is ignored by the registry** — cross-cutting pages (troubleshooting, state-portability) must be `docs/guides/*.md` with `page_title` frontmatter (jackemcpherson's registry-ignored top-level pages are the cautionary example).
- Docs publish per release tag — a docs fix requires a release. Preview before release: https://registry.terraform.io/tools/doc-preview (checks callout rendering: `->` info, `~>` warning, `!>` critical).

### examples/ conventions

```
examples/
  provider/provider.tf                                  # embedded on index page
  resources/hubspot_<name>/resource.tf                  # Example Usage
  resources/hubspot_<name>/import.sh                    # CLI import example
  resources/hubspot_<name>/import-by-string-id.tf       # import block (TF ≥1.5)
  resources/hubspot_<name>/import-by-identity.tf        # identity import (TF ≥1.12)
  data-sources/hubspot_<name>/data-source.tf
```

Glob `resource*.tf`/`provider*.tf` matches extra scenario files; other .tf files are ignored by the generator. Keep examples **applyable** — they double as manual test fixtures.

### OpenTofu registry

Scrapes the **same `docs/` tree** from GitHub per tag — one docs tree serves both registries. Its rendering pipeline is independent; avoid exotic markdown. Show both `terraform import` and `tofu import` in import examples (composite IDs need quoting: `tofu import hubspot_property.tier 'contacts/customer_tier'`).

### CDKTF

Deprecated by HashiCorp (Dec 2025); prebuilt provider packages archived. **Do not generate cdktf doc variants.**

## 2. Schema-level documentation (the source of truth)

- `MarkdownDescription` on **every** provider/resource/attribute — it surfaces in the registry, `terraform providers schema -json`, and editor hovers via terraform-ls. It's user-facing API, not comments. If content is identical, set only `MarkdownDescription` (it wins when both are set).
- Attribute-description conventions (AWS style): start with a noun/verb (never "Specifies"); booleans start "Whether to…"; state defaults ("Defaults to `false`."), valid values ("Valid values are `select`, `radio`."), and constraints explicitly.
- **tfplugindocs does NOT render plan modifiers** — write replacement semantics into the description: "Immutable property name; changes replace the definition (destroying record data)."
- Sensitive: `Sensitive: true` renders `(String, Sensitive)`; additionally say where the value lands ("Stored in state") and prefer env-var configuration in the description.
- Deprecations: `DeprecationMessage` renders a marker in docs + plan warning; group deprecated resources under the "Deprecated" subcategory.

## 3. Content conventions

### Per-resource page (default template order)
Title + one-line description → `## Example Usage` (`###` sub-headings per scenario, **simple → complex**, "Basic Usage" first; examples self-contained, no `terraform{}`/`provider{}` blocks, instance name `example`, minimal args) → Schema → `## Import` with real copy-pasteable composite IDs explained.

### Guides (`docs/guides/*.md`)
Write for: **authentication setup** (private-app creation steps, `HUBSPOT_ACCESS_TOKEN`, full resource × scopes matrix), **getting started**, **major-version upgrade guides** (one per major, every breaking change with before/after HCL + state migration commands — AWS pattern), and pattern guides (sandbox→prod promotion with workspaces/aliases, managing HubSpot defaults, destroy-semantics table). Rule: cross-resource/narrative → guide; single-resource → that resource's page.

### Provider index (`docs/index.md`, custom template)
Best-in-class model is the GitHub provider: description → Example Usage with `version = "~> 1.0"` pinning → **Authentication section with one sub-section per method, HCL + env-var alternative, documented precedence chain** → env-var table (arg ↔ env ↔ default) → `{{ .SchemaMarkdown }}` so provider args stay generated.

### HubSpot-specific content requirements
- **Per-resource required scopes**: a `->` note on each resource page (via MarkdownDescription or template) + the central scopes matrix in the auth guide. State the rule: scopes are evaluated per exact object type (`crm.schemas.contacts.write` for contact properties).
- **Tier gating**: table (Surface | Account requirement | Quota gotchas — e.g. Free = 10 custom properties) in a guide + `~>` note on affected resources; document the failure mode ("403 = missing scope OR missing product feature").
- **Destroy semantics**: explicit "Delete behavior" note per resource (archives? name reserved 90 days? restorable? preconditions?) + a summary table in a guide.
- **PII/state warnings**: `!>` callouts on `hubspot_crm_record`/`hubspot_user`; mark attributes `Sensitive`; copy the trust statement pattern: "The provider never requests CRM record scopes" (config-plane resources).

## 4. Repo-level docs

- **README**: registry badges/links, Requirements (Terraform/OpenTofu + Go versions), quick usage, "Developing the Provider" (build, `make generate`, `make testacc` with cost warning), CONTRIBUTING link.
- **CONTRIBUTING.md**: dev setup, how to add a resource, "run `make docs` and commit" rule, changelog-entry requirement, acceptance-test expectations.
- **Design records**: keep `docs/design/` (this repo's existing tree) as the AWS-style `provider-design.md` equivalent — API-mapping decisions (archive-vs-delete, ID formats, drift policy) are exactly what reviewers/users question later. Note: at scaffold time the design/research tree must move out of `docs/` (which becomes the generated registry tree) — e.g. to `dev-docs/`.
- **Changelog**: `.changelog/{PR#}.txt` typed entries assembled by hashicorp/go-changelog (AWS pattern), or keepachangelog for a small provider. Entries prefixed with resource name, **operator-impacting changes only**.

## 5. CI doc-quality gates

1. **Generate-diff gate** (most important): `tfplugindocs generate` + fail on dirty `git status --porcelain docs/`.
2. `tfplugindocs validate`.
3. `terraform fmt -check -recursive examples/` (docs embed these files verbatim).
4. `misspell -error docs/` + markdownlint + link checking.
5. **Description-coverage check**: no off-the-shelf linter exists for missing `MarkdownDescription` — a small jq script over `terraform providers schema -json` asserting every attribute has a non-empty description (recommended).
6. Mirror every CI check with a Makefile target so contributors can reproduce/fix locally.

## Minimal setup for this repo

1. `examples/` tree + `templates/index.md.tmpl` only; default templates elsewhere until a resource needs multi-scenario pages.
2. `MarkdownDescription` everywhere encoding defaults/valid values/replace semantics/scopes.
3. `make docs` = tfplugindocs generate; CI = generate-diff + validate + fmt-check + misspell + description-coverage jq.
4. Guides: `authentication.md`, `getting-started.md`, destroy-semantics table, sandbox→prod promotion; later `version-1-upgrade.md`.
5. `.changelog/` typed entries; CONTRIBUTING.md; design records stay in the design tree.

Sources: [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs) · [Registry docs spec](https://developer.hashicorp.com/terraform/registry/providers/docs) · [Doc preview](https://registry.terraform.io/tools/doc-preview) · [AWS end-user-documentation](https://github.com/hashicorp/terraform-provider-aws/blob/main/docs/end-user-documentation.md) · [AWS changelog-process](https://github.com/hashicorp/terraform-provider-aws/blob/main/docs/changelog-process.md) · [AWS provider-design](https://github.com/hashicorp/terraform-provider-aws/blob/main/docs/provider-design.md) · [GitHub provider index](https://github.com/integrations/terraform-provider-github/blob/main/docs/index.md) · [Datadog templates](https://github.com/DataDog/terraform-provider-datadog) · [OpenTofu registry](https://opentofu.org/blog/building-the-opentofu-registry/)
