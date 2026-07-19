# HubSpot CRM Admin Use-Cases for Config-as-Code (researched July 2026)

What HubSpot admins / RevOps teams actually configure, how often, and whether an API exists — this determines what the provider can and should model. Sources: knowledge.hubspot.com, developers.hubspot.com, community.hubspot.com ideas/threads, RevOps vendor blogs, admin job descriptions.

## 1. API feasibility matrix (the key output)

### Fully API-manageable → viable Terraform resources today

| Use-case | Change frequency | API |
|---|---|---|
| Custom properties + enum options + calculated formulas | **High — most-touched surface**; governance/data-dictionary review is core admin duty | Properties v3, full CRUD |
| Property groups | with properties | Properties v3 groups, full CRUD |
| Property validation rules (regex, min/max, email/domain rules) | medium | Dedicated Property Validations API (`/crm/v3/property-validations/...`) — little-known; now enforced on API writes too |
| Deal/ticket/custom-object pipelines + stages + probabilities | low-medium, high blast radius, "should be code-reviewed" | Pipelines v3, full CRUD + native audit endpoints |
| Custom object schemas | low creation rate, high stakes | Schemas v3, full CRUD (API is ahead of the UI) |
| Association labels + per-pair limits | medium | Associations v4, full CRUD |
| Active/static lists (filter definitions) | high overall; Terraform-worthy subset = stable operational segments (lifecycle, MQL, territory, suppression) | Lists v3, full CRUD; v1 sunset 2026-04-30 |
| Workflows | **highest frequency of any asset** — weekly/daily | Automation v4, full CRUD — **beta**; PUT full-replace with `revisionId` optimistic lock (actually matches Terraform's declarative model) |
| Users + role/team assignment | medium-high (joiners/leavers/reorgs) | User Provisioning v3, full CRUD; no selective team-removal (replace semantics); SCIM coexistence needed |
| Forms (new editor only) | high (weekly) | Forms v3 full CRUD; only `formType: hubspot` writable; legacy-editor forms not editable |
| Currencies & FX rates | low/periodic | Full settings API — sleeper hit, zero competition |
| Public-app webhook subscriptions | low | Webhooks v3 full CRUD — but developer-account API key auth |

### Read-only → data sources

Roles/permission sets (id+name only, no permissions payload — no drift detection on grants) · teams · owners · lifecycle stage options · CMS domains · sequences.

### UI-only, no API → out of scope (document as known gaps, watch for APIs)

Conditional stage properties / required-per-stage (the **most-requested API gap**: [idea thread](https://community.hubspot.com/t5/HubSpot-Ideas/Expose-conditional-stage-properties-via-API/idi-p/1011599)) · pipeline rules (stage-skip bans, approvals) & stage colors & pipeline team access · field-level property permissions (and note: they don't apply to API writes at all) · duplicate/dedupe rules · rollup property creation · lead scoring criteria (new engine GA Aug 2025, no API) · saved views · seat assignment (direct) · team/role creation · private apps & their scopes (the out-of-band prerequisite credential) · private-app webhooks (UI/projects only) · email sending domains (DKIM/SPF/DMARC) · business units · tracking/consent settings.

## 2. Pain points a Terraform provider directly solves

- **P1 — Sandbox → production promotion is one-way and partial.** HubSpot's deploy feature supports only *new* assets, max 300 changes, and explicitly cannot deploy *edits* to assets copied from production ([deploy docs](https://knowledge.hubspot.com/account-management/deploy-sandbox-changes-to-production)); deployed workflows arrive switched off. Multiple long-running community ideas beg for a real sync. *Terraform: same module, two workspaces — plan/apply promotes edits, the exact case HubSpot refuses.*
- **P2 — No change tracking/audit below Enterprise, no rollback.** Audit log for workflows/properties/pipelines is Enterprise-only with a 30-day window; workflow revert is blocked for workflows containing webhook/custom-code actions; a paid product (WorkflowGuard) exists solely for workflow backup/rollback. *Terraform: git history + state = diff, audit, rollback on any tier.*
- **P3 — Multi-portal replication (agencies, holdcos).** HubSpot's native answer requires Marketing Enterprise + same Multi-Account-Management org; portal-specific IDs (`objectTypeId`, `associationTypeId`) make hand-rolled replication brittle. A paid-tool market (Datawarehouse.io Portal Migration Suite, Supered Package Builder, Portal Replicator, hapily) proves demand. *Terraform: resource references resolve portal-specific IDs automatically.*
- **P4 — Explicit config-as-code demand.** Community thread literally titled ["Config as code"](https://community.hubspot.com/t5/APIs-Integrations/Config-as-code/m-p/1176948); practitioners already script the APIs with reviewed deploy scripts.
- **P5 — Admin duties are declarative-resource-shaped.** Job postings: "establish and enforce naming conventions, property governance, data standards… design and manage multiple deal pipelines, including stage definitions, probabilities, required fields."
- **P6 — Competitive gap.** Registry coverage today is users-only (2021, abandoned). HubSpot's own CLI/projects covers developer app artifacts, not CRM config.

## 3. Top 15 use-cases ranked (frequency × pain × API feasibility)

| # | Use-case | Terraform surface |
|---|---|---|
| 1 | Custom properties + enum options | `hubspot_property` |
| 2 | Property groups | `hubspot_property_group` |
| 3 | Pipelines + stages + probabilities | `hubspot_pipeline` |
| 4 | Workflows (backup/rollback/promotion — loudest pain; beta API) | `hubspot_workflow` (JSON graph) |
| 5 | Custom object schemas | `hubspot_object_schema` |
| 6 | Active/static lists | `hubspot_list` |
| 7 | Association labels + limits | `hubspot_association_label` |
| 8 | Property validation rules (differentiator — API almost nobody knows) | nested in `hubspot_property` or `hubspot_property_validation` |
| 9 | Users + role/team assignment | `hubspot_user` |
| 10 | Calculated properties (API-created = API-managed exclusively — perfect ownership story) | within `hubspot_property` |
| 11 | Forms (new editor) | `hubspot_form` |
| 12 | Currencies & FX rates | `hubspot_currency` |
| 13 | Public-app webhook subscriptions | `hubspot_webhook_subscription` |
| 14 | Sandbox/multi-portal promotion pattern | workspaces + modules (docs pattern, not a resource) |
| 15 | Read-only glue: roles, teams, owners, lifecycle stages, domains | `data.hubspot_*` |

## 4. Strategic notes

1. The strongest wedge is **schema config** (properties, groups, validations, pipelines, objects, associations): stable APIs, chronic promotion pain, zero Terraform competition.
2. **Workflows are highest-pain/highest-risk**: v4 beta with full-replace PUT + `revisionId` fits Terraform's model, but expect API churn — ship as raw-JSON resource, typed blocks later.
3. Do not build on legacy CRM cards (removed 2026-10-31) or Lists v1 (sunset 2026-04-30).
4. The provider needs two auth domains: private-app portal token (most resources) and developer-account key (public-app webhooks).
5. UI-only enforcement caveat: HubSpot's required properties per stage and field-level permissions are not enforced on API writes — don't promise governance the platform doesn't back.
