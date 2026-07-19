# Prior Art: Existing HubSpot Providers & Analogous SaaS-Config Providers (researched July 2026)

## 1. Existing HubSpot providers

| Provider | Scope | SDK | Auth | Status |
|---|---|---|---|---|
| **CleverTap/hubspot** ([GitHub](https://github.com/CleverTap/terraform-provider-hubspot), [Registry](https://registry.terraform.io/providers/CleverTap/hubspot/latest)) | `hubspot_user` resource + data source only | SDKv2 v2.6.1, Go 1.16 | OAuth refresh-token flow (pre-dates private apps) | Abandoned since 2021-06-22; ~15.6k downloads; **no license file** |
| **saurabhsaini-dev/hubspot** | Same (near-identical twin, same author lineage) | SDKv2 | Same | Abandoned 2021 |
| **jackemcpherson/hubspot** ([GitHub](https://github.com/jackemcpherson/terraform-provider-hubspot)) | `hubspot_property`, `hubspot_property_group` resources; `hubspot_property_definition[s]` data sources; pipelines & custom schemas in-repo but unreleased. Explicitly never touches CRM records | **Plugin Framework v1.19, Go 1.25** | Private-app token, `HUBSPOT_ACCESS_TOKEN`; per-alias rate controller | **Active — v0.1.1 released 2026-07-19**; rigorous design docs |
| **dylanottinger/hubspot** ([GitHub](https://github.com/dylanottinger/terraform-provider-hubspot)) | `hubspot_pipeline`, `hubspot_pipeline_stage`, `hubspot_property`, `hubspot_property_group`, all importable; roadmap: workflows, owner/pipeline data sources | Plugin Framework v1.6, Go 1.21 | Private-app token | GitHub only, no releases; last push 2026-05 |

**No official or partner-tier provider exists.** The 2021 generation modeled only users (IAM-shaped); the 2026 generation independently converged on portal schema config (properties, groups, pipelines) and explicitly excludes CRM records. The field is effectively open — but check jackemcpherson's repo before duplicating effort; it is releasing actively.

### CleverTap deep-dive (code-level)

~600 LOC non-test Go. Everything except the endpoint knowledge is anti-pattern:

- **Auth**: mints an access token once at provider configure from OAuth refresh token, all errors ignored (`req, _ :=`, `res, _ :=`) — empty token on failure, nil-pointer risk; tokens expire in 30 min so long applies would break. No private-app support.
- **Client**: hand-rolled `net/http`, no timeout, bodies never closed, HubSpot's structured error body (`category`, `correlationId`, `message`) discarded in favor of a static status-code→string map.
- **CRUD**: SDKv2 `resource.Retry` 2-min loops retrying only on string-matched "429"; `time.Sleep(2s)` cargo cult after failure; immutable `email` rejected at **apply** time in Update instead of `ForceNew` (one-line fix they missed); Terraform ID = email instead of HubSpot's stable numeric user id.
- **Data source bug**: not-found check matches "not found" but the client returns "User Does Not Exist", then dereferences a nil struct → panic.
- **Read** does fully refresh both attributes and clears ID on 404 — the one correct behavior.
- **Tests**: acceptance tests hit the real API with the author's hardcoded personal emails and portal-specific role IDs; no CheckDestroy, no import step, no CI test run.
- **Useful domain notes from its README**: `/settings/v3/users` with `idProperty=EMAIL`; Super Admins can't be deleted/reassigned via API; a user's role can't be reset to "no role".

**Verdict: nothing worth porting** (missing license also legally complicates reuse). Value = proof the users endpoint is Terraformable + quirk list.

### Design decisions worth stealing (mostly from jackemcpherson's docs)

- **Archive-not-delete**: destroy archives a property and removes state **after a confirming read**; no restore operation; document quota impact of archived definitions (Free tier: 10 custom properties).
- **Options as an owned set**: the property resource owns the complete option map; option **value** is the identity key; removing/renaming a key warns that records may hold orphaned raw values; when `external_options = true`, `options` must be unset.
- **Replace vs update**: unique-value, sensitivity, object type, internal name ⇒ replace; type/fieldType changes warn that record-value interpretation may change.
- **Import = explicit adoption** via composite natural keys (`object_type/property_name`); never auto-adopt on a 409 create conflict.
- **Least privilege**: scopes are per-object-type (`crm.schemas.contacts.write` etc.); never request CRM record scopes for a config-only provider — an advertisable security posture.
- **Tier awareness**: 403 disambiguation (missing scope vs missing product feature vs quota); document Free-tier limits.
- **Rate limiting**: jackemcpherson uses proactive `golang.org/x/time/rate`; dylanottinger uses reactive `go-retryablehttp` 429 backoff. **Do both.**

## 2. Analogous SaaS-config providers

- **hashicorp/terraform-provider-salesforce** (archived 2023): Users/Profiles/Roles only — even HashiCorp never modeled Salesforce objects/fields. Canonical **can't-delete pattern**: destroy sets `IsActive=false`, removes state, and emits `AddWarning("Users cannot be deleted from salesforce", "Destroy has deactivated the user … but the record continues to exist")`. Also a cautionary anti-pattern: `reset_password` as an action-ish toggle — model actions outside Terraform.
- **nukosuke/terraform-provider-zendesk**: ticket fields/forms/triggers/automations/brands/SLA policies — config only, never tickets. One-person maintenance decay; plan for bus factor.
- **DataDog/terraform-provider-datadog** (partner tier): gold standard — monitors/dashboards/SLOs (definitions), never events/metrics. Currently paying the SDKv2→framework **mux migration** cost; greenfield providers should start framework-native. Uses a generated API client only because Datadog owns its OpenAPI spec.
- **PagerDuty/terraform-provider-pagerduty**: years of rate-limit scar tissue ([issue #546](https://github.com/PagerDuty/terraform-provider-pagerduty/issues/546)) — users running ~80 parallel resources expected the provider to absorb 429s; official guidance now "use ≥ v3.2.2 for retry logic". Also added initial delays for **eventual consistency** (create returns before GET sees the object). Lesson: 429 handling + read-after-write retries belong in the HTTP transport from day one.

### The config-vs-records dividing line (unanimous across all prior art)

Terraform manages **durable, low-cardinality, admin-owned configuration** (schemas, fields, pipelines, forms, triggers, users/roles, policies). It never manages **high-cardinality transactional records** created by end users or integrations (tickets, contacts, deals): they drift constantly, have unbounded cardinality, are owned by other systems, and destroy semantics are dangerous. Both 2026 HubSpot providers state this exclusion explicitly.

## 3. Naming conventions

- `hubspot_<singular_noun>` snake_case: `hubspot_property`, `hubspot_property_group`, `hubspot_pipeline`, `hubspot_user`.
- Child objects: parent-prefixed compound names (`hubspot_pipeline_stage`) — though HubSpot's whole-pipeline PUT argues for stages as nested attributes inside `hubspot_pipeline` instead (owned-set pattern).
- Generic over per-object types: one `hubspot_pipeline` keyed by `object_type` attribute, not `hubspot_deal_pipeline`.
- Data sources may deliberately differ from resource names (`hubspot_property_definition`) to signal read-only inspection of any (incl. HubSpot-defined) object.

## 4. Takeaways

1. Field is open; watch jackemcpherson/hubspot (active, well-engineered, same niche).
2. Plugin Framework from day one; private-app token auth; provider aliases with per-alias rate limiters (multi-portal is the headline use case).
3. Scope = portal configuration; never CRM record scopes.
4. Destroy = archive with confirming read + Salesforce-style warning when the remote object persists.
5. Transport resilience (proactive limiter + reactive backoff + consistency retries) from day one.
6. Import via natural composite keys; no auto-adopt.
7. Document tier/quota/scope failure modes (Free 10-property cap, per-object-type scopes, ambiguous 403s).
