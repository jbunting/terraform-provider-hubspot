---
page_title: "Authentication and scopes"
subcategory: ""
description: |-
  How to authenticate the HubSpot provider and which scopes each resource needs.
---

# Authentication and scopes

The provider authenticates with a single `pat-…` bearer token supplied as the
provider's `access_token` argument or, preferably, via the
`HUBSPOT_ACCESS_TOKEN` environment variable. A token set explicitly in
configuration takes precedence over the environment variable.

```shell
export HUBSPOT_ACCESS_TOKEN="pat-..."
```

```terraform
provider "hubspot" {
  # access_token is read from HUBSPOT_ACCESS_TOKEN when omitted.
}
```

## Service key vs. private app

Two credential types work identically — both are `pat-…` bearer tokens:

- **Service key** (recommended) — HubSpot's successor to private apps for
  data/config integrations. Create one under **Development → Keys → Service
  keys** as a super admin. Service keys support rotation with a 7-day grace
  period and per-key request logs. Their scopes are capped at the creating
  user's permissions.
- **Legacy private app token** — create under **Settings → Integrations →
  Private Apps**. Long-lived and portal-scoped.

OAuth is intentionally **not** supported: OAuth access tokens expire every ~30
minutes and require an interactive install flow, which does not fit
non-interactive Terraform runs.

## Least privilege

The provider **never requests or uses CRM record scopes** (`crm.objects.*`,
beyond the owner read used by the `hubspot_owner` data source). It cannot read
or modify your contacts, companies, or deals. Grant only the configuration
scopes you actually use — HubSpot evaluates schema scopes **per object type**.

## Required scopes by resource

| Resource / data source | Required scopes |
|---|---|
| `hubspot_property`, `hubspot_property_group` (contacts) | `crm.schemas.contacts.read`, `crm.schemas.contacts.write` |
| `hubspot_property`, `hubspot_property_group` (companies) | `crm.schemas.companies.read`, `crm.schemas.companies.write` |
| `hubspot_property`, `hubspot_property_group` (deals) | `crm.schemas.deals.read`, `crm.schemas.deals.write` |
| `hubspot_property`, `hubspot_property_group` (custom objects) | `crm.schemas.custom.read`, `crm.schemas.custom.write` |
| `hubspot_object_schema` | `crm.schemas.custom.read`, `crm.schemas.custom.write` (Enterprise tier) |
| `hubspot_pipeline` | `crm.pipelines.write` (+ the object write scope, e.g. `crm.objects.deals.write` / `crm.objects.tickets.write`) |
| `hubspot_association_label` / `data.hubspot_association_labels` | the read/write schema scopes of **both** object types (custom labels require Professional/Enterprise) |
| `hubspot_list` | `crm.lists.read`, `crm.lists.write` |
| `data.hubspot_property`, `data.hubspot_properties` | `crm.schemas.{objectType}.read` |
| `data.hubspot_owner` | `crm.objects.owners.read` |
| `data.hubspot_portal` | none |

## Interpreting 403 errors

A `403` from HubSpot has three common causes, and the provider surfaces which
one applies:

- **Missing scope** — the token lacks a scope listed above. Add it and
  re-issue the token.
- **Product tier** — the feature requires a higher subscription (custom objects
  need Enterprise; association labels and multiple pipelines need
  Professional/Enterprise).
- **Quota** — the daily API limit is exhausted. The provider fails fast rather
  than retrying.
