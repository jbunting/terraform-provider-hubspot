---
page_title: "Sandbox to production promotion"
subcategory: ""
description: |-
  Apply the same HubSpot configuration to a sandbox portal first, then promote it to production with workspaces or provider aliases.
---

# Sandbox to production promotion

HubSpot's built-in sandbox deploy feature only moves *new* assets (max 300
changes per deploy) and cannot promote **edits** to assets that already exist
in production. Terraform removes that limit: define the configuration once,
apply it to the sandbox portal, review, and apply the identical configuration
to production. `plan` shows exactly what promotion will change, and edits
promote the same way creations do.

## Reference portal-specific IDs by name, never by ID

The one rule that makes a configuration portable across portals: HubSpot IDs
are **portal-specific**. A custom object's `object_type_id` (`2-XXXX`) and a
`USER_DEFINED` association label's `type_id` differ between your sandbox and
production even when the objects are otherwise identical.

- Reference custom objects by resource attribute
  (`hubspot_object_schema.machine.object_type_id`) or resolve them by name
  with the `hubspot_object_schema` data source.
- Resolve association `type_id`s with the `hubspot_association_labels` data
  source instead of hardcoding numbers.
- Owner IDs also differ per portal — look them up by email with the
  `hubspot_owner` data source.

A configuration that contains no literal `2-XXXX` or numeric `type_id` values
applies cleanly to any portal.

## Option A: workspaces

One configuration, one workspace per portal, with the token supplied per run:

```shell
terraform workspace new sandbox
terraform workspace new production

terraform workspace select sandbox
HUBSPOT_ACCESS_TOKEN="pat-...sandbox..." terraform apply

# After review:
terraform workspace select production
HUBSPOT_ACCESS_TOKEN="pat-...production..." terraform apply
```

(Substitute `tofu` for `terraform` under OpenTofu.) Each workspace keeps its
own state, so the same resources track the sandbox copy and the production
copy independently.

## Option B: provider aliases

Both portals in a single configuration — useful when promotion should be one
`apply` and the portals evolve in lockstep:

```terraform
provider "hubspot" {
  alias        = "sandbox"
  access_token = var.sandbox_token
}

provider "hubspot" {
  alias        = "production"
  access_token = var.production_token
}

module "crm_schema_sandbox" {
  source    = "./modules/crm-schema"
  providers = { hubspot = hubspot.sandbox }
}

module "crm_schema_production" {
  source    = "./modules/crm-schema"
  providers = { hubspot = hubspot.production }
}
```

Wrap the shared configuration in a module so both portals consume the same
definitions. Prefer workspaces when production applies must be gated behind
a separate review step; prefer aliases for keeping several portals (agency /
multi-brand setups) identical.

## Guard against applying to the wrong portal

A wrong token silently targets the wrong portal. Assert the portal identity
with the `hubspot_portal` data source before anything else applies:

```terraform
data "hubspot_portal" "current" {}

check "portal_guard" {
  assert {
    condition     = data.hubspot_portal.current.portal_id == var.expected_portal_id
    error_message = "HUBSPOT_ACCESS_TOKEN belongs to portal ${data.hubspot_portal.current.portal_id}, expected ${var.expected_portal_id}."
  }
}
```

## What does not promote

- **Records.** The provider manages configuration, not CRM data — promoting a
  pipeline does not move the deals in it.
- **Archived-name collisions.** Destroying a property in production reserves
  its name for ~90 days ("name purgatory"); a promotion that re-creates a
  recently deleted property name fails with an actionable error. Plan
  renames as renames (replace), not delete-then-recreate across runs.
- **HubSpot-defined defaults.** Default pipelines and properties exist in
  every portal already — adopt them via `terraform import` in *each* portal
  rather than declaring new ones.
