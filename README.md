# Terraform Provider for HubSpot

Manage your HubSpot portal configuration as code. This provider targets the
HubSpot **configuration plane** — the structural setup of your portal
(properties, groups, pipelines, custom object schemas) — deliberately **not**
CRM records (contacts, companies, deals) themselves. It works identically
with Terraform and OpenTofu.

**Why?** HubSpot admins have no good answer for promotion, drift, and audit:
sandbox→production deploys can't promote *edits* to existing assets, the
audit log is Enterprise-only with a 30-day window, and replicating
configuration across portals is brittle because HubSpot IDs are
portal-specific. Terraform solves all three: the same module applied to
several portals, `plan` as drift detection, and git history as the audit
trail.

## Supported resources

| Resource | Manages | Destroy behavior | Import ID |
|---|---|---|---|
| [`hubspot_property_group`](./docs/resources/property_group.md) | CRM property groups — the named sections that organize properties in the HubSpot UI | Deletes the group | `{object_type}/{name}` |
| [`hubspot_property`](./docs/resources/property.md) | Custom CRM property definitions on any object type, including enumeration options (list order = display order) and all field types | **Archives** the property — HubSpot reserves the name for ~90 days ("name purgatory"); the provider reports an actionable error if you recreate the name too soon | `{object_type}/{name}` |

Both resources support the full lifecycle: create, in-place update, replace
on immutable-field changes (planned at plan time via `RequiresReplace`, with
data-loss warnings in the docs), drift detection (out-of-band deletions are
re-created, out-of-band edits are corrected), and `terraform import`.

### Example

```terraform
resource "hubspot_property_group" "machine_info" {
  object_type = "contacts"
  name        = "machine_info"
  label       = "Machine information"
}

resource "hubspot_property" "warranty_status" {
  object_type = "contacts"
  name        = "warranty_status"
  label       = "Warranty status"
  type        = "enumeration"
  field_type  = "select"
  group_name  = hubspot_property_group.machine_info.name

  # List position is the display order.
  options = [
    { label = "Active", value = "active" },
    { label = "Expired", value = "expired" },
  ]
}
```

More runnable examples live in [`examples/`](./examples/); full argument
reference in [`docs/`](./docs/) (rendered on the registries once published).

### Roadmap

The provider follows a phased plan (see [`dev-docs/ROADMAP.md`](./dev-docs/ROADMAP.md)).
Beta HubSpot APIs are in scope: a public-beta API is enough to build on —
the resource is then documented as beta-backed and uses a raw-JSON schema
where the API is still moving.

- **Next:** data sources (`hubspot_property`, `hubspot_owner`, `hubspot_portal`)
- **Phase 2 — schema plane complete:** `hubspot_object_schema` (custom objects), `hubspot_pipeline` (deal/ticket/custom pipelines with inline stages), `hubspot_association_label`
- **Phase 3:** `hubspot_list` (dynamic/static list definitions), public-app webhook settings & subscriptions
- **Phase 4:** `hubspot_workflow` — workflows as code via the Automation v4 beta API (raw JSON graph, optimistic locking); the single loudest admin pain (backup, rollback, sandbox→prod promotion)
- **Phase 5:** `hubspot_user` (+ team/role data sources), a generic `hubspot_crm_record` escape hatch for seed/fixture records
- **Post-1.0:** forms, currencies/FX rates, typed workflow action blocks

### Out of scope — HubSpot has no API (yet)

Some things admins configure in the HubSpot UI **cannot** be managed by any
provider today, because HubSpot exposes no public API for them — not even a
beta. Each becomes a roadmap candidate the moment an API ships:

| Feature | Status |
|---|---|
| Conditional stage properties (required properties per pipeline stage) | No API — HubSpot's [most-requested API gap](https://community.hubspot.com/t5/HubSpot-Ideas/Expose-conditional-stage-properties-via-API/idi-p/1011599) |
| Pipeline rules, stage colors, pipeline team access | UI-only |
| Field-level property permissions | UI-only (and not enforced on API writes) |
| Duplicate/dedupe rules | UI-only |
| Rollup property creation | UI-only (read-only via API) |
| Lead scoring criteria | No API |
| Saved views / index-page filters | No API — use `hubspot_list` (Phase 3) instead |
| Team, role (permission set), and seat creation | Read-only APIs — exposed as data sources instead |
| Private apps & their scopes | UI-only — this is the provider's own out-of-band credential |
| Private-app webhook subscriptions | UI-only (public-app webhooks are covered in Phase 3) |
| Email sending domains (DKIM/SPF/DMARC) | No API |

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0 or [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6
- [Go](https://golang.org/doc/install) 1.25+ (only to build the provider from source)

## Using the provider

```terraform
terraform {
  required_providers {
    hubspot = {
      source = "revosai/hubspot"
    }
  }
}

provider "hubspot" {
  # Authentication uses a HubSpot private app access token,
  # read from the HUBSPOT_ACCESS_TOKEN environment variable.
}
```

### Authentication & scopes

Create a [private app](https://developers.hubspot.com/docs/guides/apps/private-apps/overview)
in your HubSpot portal and export its token:

```shell
export HUBSPOT_ACCESS_TOKEN="pat-..."
```

Grant only the configuration scopes you need — HubSpot evaluates them **per
object type**:

| Managing | Required scopes |
|---|---|
| Contact properties/groups | `crm.schemas.contacts.read`, `crm.schemas.contacts.write` |
| Company properties/groups | `crm.schemas.companies.read`, `crm.schemas.companies.write` |
| Deal properties/groups | `crm.schemas.deals.read`, `crm.schemas.deals.write` |
| Custom-object properties/groups | `crm.schemas.custom.read`, `crm.schemas.custom.write` |

The provider never requests or uses CRM **record** scopes
(`crm.objects.*`) — it cannot read or modify your contacts, companies, or
deals. A `403` from HubSpot usually means the token is missing a scope *or*
the portal's product tier lacks the feature; the provider's error messages
say which scope to check.

### Multi-portal / sandbox promotion

Because resources are addressed by name (not portal-specific IDs), the same
configuration applies cleanly to multiple portals — use one workspace or
provider alias per portal and promote changes with `plan`/`apply`:

```terraform
provider "hubspot" {
  alias        = "sandbox"
  access_token = var.sandbox_token
}

provider "hubspot" {
  alias        = "production"
  access_token = var.production_token
}
```

## Developing the provider

Build, lint, and test with the included `GNUmakefile`:

```shell
make build    # go build ./...
make test     # unit + acceptance tests
make lint     # golangci-lint
make testacc  # acceptance tests (TF_ACC=1)
```

Acceptance tests are **hermetic by default** — they run the full Terraform
lifecycle against an in-process fake HubSpot API that emulates real API
behavior (rate limits, archive semantics, name purgatory), so no credentials
or real portal are required and the suite finishes in seconds. CI runs the
same suite against both Terraform (1.13, 1.14) and OpenTofu.

Development is strictly **test-driven** — the desired HCL and a failing
lifecycle test come before any implementation. See
[`CONTRIBUTING.md`](./CONTRIBUTING.md) for the workflow and
[`dev-docs/`](./dev-docs/) for the design decisions and research behind the
resource model (archive-vs-delete semantics, import ID formats, rate-limit
strategy, and more).

To regenerate documentation after schema changes, run `make generate`
(uses [tfplugindocs](https://github.com/hashicorp/terraform-plugin-docs)).

## FAQ

**Why doesn't the provider manage contacts, companies, or deals?**
Records are the wrong shape for Terraform: they're edited continuously by
sales reps, workflows, and integrations (so `plan` would show perpetual
drift), they put PII into Terraform state (a GDPR liability — state backups
aren't erasable), and refreshing thousands of records doesn't scale. Every
mature SaaS provider (Datadog, Zendesk, Salesforce) draws the same line:
configuration yes, transactional records no. A narrow `hubspot_crm_record`
escape hatch for seed/fixture records is planned (Phase 5).

**What does `terraform destroy` actually do?**
Whatever the HubSpot API does — which is usually *archive*, not delete.
Properties are archived and their names stay reserved for ~90 days; the
provider tells you explicitly when a name is in that "purgatory" window.
Each resource's destroy behavior is documented in its registry page and in
the table above. The provider never fakes a successful delete of something
HubSpot won't remove.

**Can I manage HubSpot's built-in default properties or pipelines?**
Not by declaring them — the provider never silently adopts objects it didn't
create. Explicitly `terraform import` a HubSpot-defined object if you want
to manage it; where HubSpot forbids deletion, destroy returns a clear error
suggesting `terraform state rm`.

**Does it work with OAuth apps instead of private app tokens?**
No. OAuth access tokens expire every 30 minutes and need an interactive
install flow — the wrong shape for non-interactive Terraform runs. Private
app tokens are long-lived, portal-scoped, and rotatable. (OAuth support may
be reconsidered if there's demand.)

**Is my access token stored in Terraform state?**
The provider config's `access_token` is marked sensitive and, when supplied
via the `HUBSPOT_ACCESS_TOKEN` environment variable, never appears in your
configuration or state at all — that's the recommended setup.

**How does the provider handle HubSpot rate limits?**
A client-side token bucket stays under the burst limit proactively, and 429
responses are retried automatically with backoff (honoring `Retry-After`).
If your *daily* API quota is exhausted, the provider fails fast with a clear
message instead of retrying pointlessly.

**Is it safe to build on beta HubSpot APIs (e.g. workflows)?**
Resources backed by beta APIs are explicitly marked as beta-backed in their
documentation and use raw-JSON schemas where the API surface is still
moving, so upstream changes don't break your state. Expect these resources
to evolve faster than the rest of the provider.

**Can I migrate from another HubSpot provider (CleverTap, jackemcpherson, …)?**
There is no automatic state migration from third-party HubSpot providers —
their resource schemas and IDs are incompatible with this provider's. The
supported path is adoption via import: remove the resource from the old
provider's management (`terraform state rm`), then `terraform import` it
here using this provider's ID format (e.g. `contacts/customer_tier`). The
HubSpot objects themselves are untouched by the switch.

**Why is feature X missing?**
Check the [out-of-scope table](#out-of-scope--hubspot-has-no-api-yet) first —
most gaps exist because HubSpot has no public API for that feature. If an
API exists and the resource just isn't built yet, it's on the
[roadmap](#roadmap) or worth an issue.

**Terraform or OpenTofu?**
Both, as equals. The same binary serves both tools, CI runs the acceptance
suite against both, and the provider will be published to both registries.

## License

This project is licensed under the [Mozilla Public License 2.0](./LICENSE).
