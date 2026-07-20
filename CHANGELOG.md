# Changelog

## 0.1.0 (Unreleased)

FEATURES:

* **New Provider:** `hubspot` — manage HubSpot portal configuration (config plane, not CRM records) as code
* **New Resource:** `hubspot_property_group` — manage CRM property groups (create/update/import; replace on `object_type`/`name` change)
* **New Resource:** `hubspot_property` — manage CRM object properties incl. enumeration options (list order = display order), archive-on-destroy with name-purgatory error handling, `{object_type}/{name}` import
* **New Data Source:** `hubspot_property` — read any property (including HubSpot-defined defaults) by object type and name
* **New Data Source:** `hubspot_owner` — look up a CRM owner by email or owner ID
* **New Data Source:** `hubspot_portal` — the authenticated portal's ID and account defaults
