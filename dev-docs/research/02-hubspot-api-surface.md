# HubSpot API Surface for the Provider (researched July 2026)

Canonical docs: guides at `https://developers.hubspot.com/docs/guides/api/...`, reference at `https://developers.hubspot.com/docs/api-reference/...`, machine-readable index at `https://developers.hubspot.com/docs/llms.txt` (the `.md`-suffixed URLs fetch cleanly; some older guide URLs redirect through login).

HubSpot is introducing **date-based API versioning** (e.g. `/crm/associations/2026-03/...`) alongside classic `/crm/v3`//`v4`. Both work; keep the version path segment configurable per client service.

## 1. Authentication

**Private App access tokens are the right model for Terraform**: non-interactive, long-lived, single-portal. OAuth public apps (30-min tokens + refresh persistence) are the wrong shape; legacy `hapikey` is sunset except for a few developer-account endpoints (webhooks v3 for public apps).

- Header: `Authorization: Bearer pat-{region}-…` (validate `pat-` prefix, don't hard-fail on region).
- Token introspection for `Configure()` validation: `POST /oauth/v2/private-apps/get/access-token-info` with `{"tokenKey": "<token>"}` → hubId, scopes.
- Max 20 private apps per account. Rotation is UI-driven (7-day dual-validity window).

### Scopes by API area

| Area | Scopes |
|---|---|
| Records (contacts/companies/deals) | `crm.objects.{contacts,companies,deals}.read/.write`; tickets = legacy `tickets` umbrella scope |
| Properties & groups | `crm.schemas.{contacts,companies,deals,custom}.read/.write` (per exact object type) |
| Custom object schemas | `crm.schemas.custom.read/.write` (records: `crm.objects.custom.*`) |
| Pipelines | `crm.pipelines.orders.read/.write` + the object's scopes |
| Associations/labels | object read/write scopes of both sides; label definitions need schema scopes |
| Owners | `crm.objects.owners.read` (read-only) |
| Users/teams | `settings.users.read/.write`, `settings.users.teams.read/.write`; paid-seat role changes also `billing-write` |
| Lists | `crm.lists.read/.write` |
| Workflows | `automation` |
| Token introspection | `oauth` |

## 2. CRM record APIs (uniform across object types)

`{objectType}` accepts names (`contacts`, `deals`, …) or objectTypeIds (`0-1`, `0-3`, `2-XXXX` custom).

- `POST /crm/v3/objects/{objectType}` — create; **not idempotent** (contacts return 409 with existing ID on duplicate email — parse for adoption).
- `GET .../{id}` — params `properties`, `associations`, `archived`, `idProperty`. Strongly consistent — always read back by ID, never via search.
- `PATCH .../{id}` — partial update; clear a property with `""`.
- `DELETE .../{id}` — **archive** (90-day recycle bin; restore is UI-only for records → treat archive as destroy).
- Alternate keys: `?idProperty={uniqueProperty}` (e.g. contacts by `email`). Companies have **no** built-in unique `domain` key.
- Batch: `batch/create|read|update|archive|upsert` (upsert takes `idProperty` — the idempotent path); chunk at 100 inputs; one batch call = one request against rate limits.
- GDPR hard delete: `POST .../gdpr-delete`.
- Record ID = numeric string, mirrored in read-only `hs_object_id`.

## 3. Schema/config APIs (the provider's core surface)

### Custom object schemas (Enterprise only)
- `POST/GET /crm-object-schemas/v3/schemas` (alias `/crm/v3/schemas`); `GET/PATCH/DELETE .../{objectTypeId|fullyQualifiedName}`.
- Create requires `name`, `labels{singular,plural}`, `properties[]`, `primaryDisplayProperty`, `associatedObjects[]`; optional `requiredProperties`, `searchableProperties`, `secondaryDisplayProperties`, `description`.
- **`name` immutable.** Delete = soft-delete allowed only after all records/associations/properties deleted; hard delete (frees the name) via `DELETE ...?archived=true`.
- IDs: `objectTypeId` = `2-XXXXXXX` (portal-specific!), `fullyQualifiedName` = `p{hubId}_{name}`. Cross-portal configs must reference by name.

### Properties & groups
- `GET/POST /crm/v3/properties/{objectType}`; `GET/PATCH/DELETE .../{propertyName}`; groups under `.../groups[/{groupName}]`; batch `create|read|archive`.
- Create requires `name`, `label`, `type`, `fieldType`, `groupName`. **`name` immutable; `type`/`fieldType` effectively immutable via API** (treat as ForceNew). PATCHable: `label`, `description`, `groupName`, `options`, `displayOrder`, `hidden`, `formField`.
- `type` ∈ string|number|bool|enumeration|date|datetime (+ read-only internal `json`, `object_coordinates`). `fieldType` ∈ text|textarea|number|date|select|radio|checkbox|booleancheckbox|file|html|phonenumber|calculation_equation.
- Enum `options[]`: `{label, value, displayOrder, hidden, description}`; option `value` is the stable diff key. Multi-checkbox record values are semicolon-delimited.
- `hasUniqueValue: true` create-time only (max 10/object) — enables idProperty lookups.
- Calculated: `calculationFormula` + `fieldType: calculation_equation`. **API-created calc properties can only be edited via API (not UI), and vice versa** — favorable exclusive-ownership story for Terraform.
- Read-back flags to treat as computed: `calculated`, `hubspotDefined`, `modificationMetadata{readOnlyValue, readOnlyDefinition, archivable}`. `hubspotDefined: true` properties can't be deleted and are only partially editable.
- **DELETE archives; archived properties purge after ~90 days and the `name` is locked until purge** — recreate-same-name inside the window fails. Surface a clear error.

### Pipelines & stages
- `GET/POST /crm/v3/pipelines/{objectType}`; `GET/PUT/PATCH/DELETE .../{pipelineId}`; stages at `.../stages[/{stageId}]`.
- **PUT replaces the whole pipeline including the full stages array** — good fit for one resource with nested stages, but omitted stages are deleted.
- Deal stages **require** `metadata.probability` ("0.0"–"1.0", string); tickets use `metadata.ticketState` (OPEN|CLOSED).
- Always pass `validateReferencesBeforeDelete=true` on DELETE — returns which stages/records still reference the pipeline instead of orphaning records.
- Native audit endpoints: `.../{pipelineId}/audit`, `.../stages/{stageId}/audit`.
- Every portal has a `default` pipeline; last pipeline of an object can't be deleted → model defaults as importable/adoptable.
- Limits: 100 stages (deals/tickets/custom); multiple pipelines gated by Sales/Service Hub tier; supported objects: deals, tickets, appointments, courses, listings, orders, services, leads (Pro/Ent), custom objects (Ent).

### Associations v4 (labels = the Terraform-worthy part)
- Definitions: `GET/POST/PUT /crm/v4/associations/{from}/{to}/labels`, `DELETE .../labels/{associationTypeId}`. Create body `{label, name, inverseLabel?}` — `inverseLabel` ⇒ paired (two directional typeIds). Custom labels require Pro/Ent.
- `typeId`s are **directional**; `HUBSPOT_DEFINED` typeIds are global constants (contact→company primary = 1), `USER_DEFINED` typeIds are **portal-specific** — resolve by name at read time, never hard-code.
- Record-level: `PUT /crm/v4/objects/{type}/{id}/associations/[default/]{toType}/{toId}`; label PUT **replaces** unless existing labels are included; batch endpoints (`/batch/create` ≤2000, `/batch/read` ≤1000, `/batch/archive` ≤100).
- Association limits per pair: `POST /crm/v4/associations/definitions/configurations/{from}/{to}/batch/create` (`maxToObjectIds`).

### Property validation rules (little-known, dedicated API)
- `PUT/GET /crm/v3/property-validations/{objectTypeId}/{propertyName}/rule-type/{ruleType}` — ruleTypes: REGEX, MIN/MAX_LENGTH, MIN/MAX_NUMBER, DECIMAL, ALPHANUMERIC, EMAIL/DOMAIN/URL, PHONE_NUMBER_WITH_EXPLICIT_COUNTRY_CODE, date rules. Now enforced on API writes too. Docs live under a "legacy" URL but the API is not deprecated.

## 4. Other resources

| Resource | API | Verdict |
|---|---|---|
| Owners | `GET /crm/v3/owners[?email=]`, `?archived=true` for deactivated. Read-only. Owner `id` (not `userId`) goes into `hubspot_owner_id` properties | Data source |
| Users | `/settings/v3/users` full CRUD (`idProperty=EMAIL` supported); attrs: email, roleId, primaryTeamId, secondaryTeamIds, sendWelcomeEmail. No API to selectively remove a user from a team (deprovision+reprovision workaround). SCIM may own user lifecycle — tolerate externally managed users | Resource (limited) |
| Roles / permission sets | `GET /settings/v3/users/roles` — id+name only, creation UI-only | Data source |
| Teams | `GET /settings/v3/users/teams` — read-only | Data source |
| Lists v3 | `POST /crm/v3/lists` (`name`, `objectTypeId`, `processingType` MANUAL/DYNAMIC/SNAPSHOT, `filterBranch`); by-name GET `/crm/v3/lists/object-type-id/{objectTypeId}/name/{name}`; `PUT .../update-list-name`, `PUT .../update-list-filters` (replaces the filter tree); `DELETE` restorable via `PUT .../restore` ≤90 days; memberships `add-and-remove` (MANUAL/SNAPSHOT only). **v1 lists sunset 2026-04-30** | Resource |
| Workflows (Automation v4) | `POST/GET /automation/v4/flows`, `GET/PUT/DELETE .../{flowId}`, `POST /flows/batch/read`. **Beta.** PUT = full replace requiring current `revisionId` (optimistic lock → GET-then-PUT). Flow types CONTACT_FLOW / PLATFORM_FLOW. Scope `automation` | Raw-JSON resource, later phase |
| Sequences | read + enroll only, no definition CRUD | Data source at most |
| Forms v3 | `GET/POST /marketing/v3/forms`, `GET/PATCH/PUT/DELETE .../{formId}` — full CRUD but only `formType: hubspot` (new editor) writable; lifecycleStages quirk (set both `0-1` and `0-2`) | Resource candidate |
| Currencies/FX | `GET /settings/v3/currencies/codes`, exchange-rate batch create/patch, `PUT .../company-currency` — full API, zero competition | Resource candidate |
| Webhooks (public apps, v3) | `GET/PUT /webhooks/v3/{appId}/settings`, `POST/GET .../subscriptions`, `PATCH/DELETE .../{id}` — **requires developer API key, not portal PAT**. Private-app webhooks have no REST management API (UI / projects `webhooks.json` only). New pull-based Webhooks Journal v4 exists | Second auth domain; defensible but not v1 |
| CMS domains | `GET /cms/v3/domains` — read-only | Data source |
| Lifecycle stages | options readable via the `lifecyclestage` property; not writable | Data source |
| CRM cards (legacy) | **endpoints removed 2026-10-31** | Do not build |

## 5. Rate limits

Private apps (burst per app / daily per account, shared across apps, resets midnight portal time):

| Tier | Per 10s | Per day |
|---|---|---|
| Free/Starter | 100 | 250,000 |
| Professional | 190 | 625,000 |
| Enterprise | 190 | 1,000,000 |
| + API add-on (max 2) | 250 | +1,000,000 each |

- OAuth apps: 110/10s per installed account, no daily cap.
- **Search API is a separate pool: 5 req/s per token, 200 results/page, hard 10,000-result cap per query** (paging past it → 400).
- 429 body `policyName` distinguishes `TEN_SECONDLY_ROLLING` vs `DAILY` (abort on DAILY). Headers: `X-HubSpot-RateLimit-Max/-Remaining/-Interval-Milliseconds/-Daily/-Daily-Remaining` (absent on search endpoints). `Retry-After` is **not reliably present** — token-bucket throttle under the burst limit (~150/10s headroom) + exponential backoff with jitter. Prefer batch endpoints (100 items = 1 request).

## 6. Quirks the provider must handle

1. **Soft delete everywhere**: records/properties/schemas/lists archive on DELETE; `?archived=true` lists archived. Read maps 404 → remove from state.
2. **Name purgatory**: property and schema `name`s locked ~90 days after archive; schema names freed by hard delete `?archived=true`.
3. **`name` vs `label`**: internal `name`/typeId is identity (immutable, use in Terraform IDs); `label` is display-only and mutable.
4. **Server normalization**: HubSpot rewrites `displayOrder: -1`, injects defaults into filter JSON, normalizes dates — semantic diffing, never raw equality.
5. **Eventual consistency**: search index lags writes (seconds to ~1 min) — read-after-write only via direct GET by ID; dynamic list membership evaluates async.
6. **Pagination**: uniform `paging.next.after` cursor; `limit` max 100 (objects), 200 (search); no total counts outside search.
7. **No idempotency keys**: only upsert-by-idProperty gives create-or-update.
8. **PUT-replace semantics**: pipelines (stages array), workflows v4 (whole flow + `revisionId`), list filters — always read-modify-write.
9. **Multi-checkbox encoding** (`;a;b`, leading `;` appends) and date (`YYYY-MM-DD`/epoch-millis) normalization in Read.
10. **Tier gating**: custom objects = Enterprise; association labels, multiple pipelines = Pro/Ent. Translate 403 `MISSING_SCOPES` (body lists required scopes) and product-tier errors into actionable messages.

## 7. Go client

**No official HubSpot Go SDK** (official libs: Node/PHP/Ruby/Python only). Community options (scopiousdigital/hubspot-go, clarkmcc/go-hubspot generated, belong-inc/go-hubspot) are young/partial/stale. **Hand-roll a thin client** in `internal/client/`: the Terraform-relevant surface is small and uniform (JSON + bearer + cursor paging), and we need provider-specific middleware anyway (token bucket, 429 policyName handling, rate-limit header parsing, configurable version path segments). HubSpot's per-API OpenAPI specs can seed hand-written types.

## Key links

- Private apps: https://developers.hubspot.com/docs/guides/apps/private-apps/overview
- Rate limits: https://developers.hubspot.com/docs/developer-tooling/platform/usage-guidelines
- Properties: https://developers.hubspot.com/docs/guides/api/crm/properties
- Schemas: https://developers.hubspot.com/docs/api-reference/legacy/crm/objects/schemas/guide
- Pipelines: https://developers.hubspot.com/docs/guides/api/crm/pipelines
- Associations v4: https://developers.hubspot.com/docs/guides/api/crm/associations/associations-v4
- Lists v3: https://developers.hubspot.com/docs/guides/api/crm/lists/overview
- Users: https://developers.hubspot.com/docs/guides/api/settings/users/user-provisioning
- Workflows v4 (beta): https://developers.hubspot.com/docs/guides/api/automation/create-manage-workflows
- Property validations: https://developers.hubspot.com/docs/api-reference/legacy/crm/property-validations/guide
