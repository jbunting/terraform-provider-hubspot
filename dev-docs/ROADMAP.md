# Implementation Roadmap

Phases assume the conventions in `docs/research/01-terraform-provider-best-practices.md` and the resource semantics in `docs/design/resource-model.md`.

## Phase 0 — Foundations
- Scaffold from `hashicorp/terraform-provider-scaffolding-framework` (framework v1.19+, Go 1.25, protocol v6). Pick a license before writing code (MPL-2.0 like HashiCorp providers, or Apache-2.0).
- `internal/client/`: bearer auth, token-bucket limiter + 429/5xx retry with policyName awareness, typed errors (`ErrNotFound`), cursor pagination helper, structured HubSpot error-body decoding, configurable base URL + version path segments, `tflog` wire logging.
- Provider `Configure` with token validation (`/account-info/v3/details`), portal_id caching.
- Test harness per `docs/research/05-provider-testing.md`: unit layer (schema ValidateImplementation, flatten/expand tables, validator/plan-modifier tests), hermetic `resource.UnitTest` lifecycle against an httptest fake HubSpot (PR-runnable, no creds; fake scripts 429/eventual-consistency/name-purgatory), `TF_ACC`-gated real-portal acceptance tests (basic/update/`_disappears`/import/empty-replan) with `tf-acc-test` prefix, portal-ID guard, and sweepers. CI: PR = lint + unit + hermetic across TF {n-1, n} × OpenTofu (`TF_ACC_TERRAFORM_PATH` → tofu, `TF_ACC_PROVIDER_HOST=registry.opentofu.org`); nightly = real API + sweep, secrets never in fork-PR runs; tfplugindocs generate-diff gate.

## Phase 1 — v0.1 "properties-as-code" (MVP)
`hubspot_property_group`, `hubspot_property` (all types, enum option ordering, calculation_formula, validation rules block), data sources `hubspot_property`/`hubspot_properties`, `hubspot_owner`, `hubspot_portal`. Import from day one. This alone is adoptable — property drift is the #1 admin pain.

## Phase 2 — v0.3 schema plane complete
`hubspot_object_schema`, `hubspot_pipeline` (stage-diff engine keyed on stage_id + validateReferences), `hubspot_association_label`; data sources `hubspot_pipeline`, `hubspot_object_schema`, `hubspot_association_labels`.

## Phase 3 — v0.5 lists + webhooks
`hubspot_list` (JSON filter semantic-equality custom type — highest-risk item, timebox it), `hubspot_list_membership`, `hubspot_webhook_settings`, `hubspot_webhook_subscription` (+ `developer_api_key` config path).

## Phase 4 — v0.7 people + escape hatch
`hubspot_user` (+ `hubspot_team`/`hubspot_role` data sources), `hubspot_crm_record`, `hubspot_association`; PII/state-security documentation page.

## Phase 5 — v1.0 hardening
Import round-trip tests everywhere (`ImportStateVerify`), plan-modifier audit (every immutable field has RequiresReplace + destroy-impact warnings), rate-limit soak test, state-upgrade paths frozen, docs (per-resource scopes, archive-semantics table, "managing HubSpot defaults" guide, sandbox→prod promotion guide with workspaces/aliases), **dual registry publication** per `docs/research/07-releasing.md` — Terraform Registry (goreleaser + GPG, webhook auto-ingest) and OpenTofu Registry (issue-form submission of provider + non-expiring RSA signing key to `opentofu/registry`; it indexes the same GitHub release artifacts — no separate build needed). Rehearse the pipeline with a prerelease tag (`v0.1.0-rc1`) before the first real release; add `goreleaser release --snapshot` smoke + upgrade-state gate to PR CI from the first tagged release onward.

## Post-1.0 candidates
- `hubspot_workflow` (Automation v4 — raw `flow_json` with revisionId GET-then-PUT; API is beta, loudest admin pain; see resource-model §Tier 2).
- `hubspot_form` (Forms v3, new-editor only), `hubspot_currency`/FX rates.
- Typed filter blocks for lists; `hubspot_property_options` (manage options on HubSpot-defined enums like lifecyclestage); webhooks v4 journal subscriptions when GA; CMS-domain data sources.

## Known-infeasible (no API — document, watch changelog)
Conditional stage properties, pipeline rules/stage colors/pipeline access, field-level permissions, duplicate rules, rollup properties, lead scoring criteria, saved views, team/role/seat creation, private apps & scopes, private-app webhooks, email sending domains.

## Competitive note
`jackemcpherson/terraform-provider-hubspot` (framework-native, properties+groups, v0.1.1 released 2026-07-19) is active in the same niche with rigorous design docs. Before each phase, check whether collaboration or differentiation (schema plane completeness, workflows, multi-portal promotion story) is the better move.
