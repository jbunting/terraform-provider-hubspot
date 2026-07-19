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

The provider follows a phased plan (see [`dev-docs/ROADMAP.md`](./dev-docs/ROADMAP.md)):

- **Next:** data sources (`hubspot_property`, `hubspot_owner`, `hubspot_portal`)
- **Phase 2 — schema plane complete:** `hubspot_object_schema` (custom objects), `hubspot_pipeline` (deal/ticket/custom pipelines with inline stages), `hubspot_association_label`
- **Phase 3:** `hubspot_list` (dynamic/static list definitions), webhook settings & subscriptions
- **Phase 4:** `hubspot_user` (+ team/role data sources), a generic `hubspot_crm_record` escape hatch for seed/fixture records
- **Post-1.0:** `hubspot_workflow` (raw JSON, once HubSpot's Automation v4 API leaves beta), forms, currencies

Not plannable (no public HubSpot API): conditional stage properties,
pipeline rules, field-level permissions, dedupe rules, lead scoring, saved
views, team/role creation.

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

## License

This project is licensed under the [Mozilla Public License 2.0](./LICENSE).
