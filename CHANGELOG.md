# Changelog

## 0.1.0 (Unreleased)

FEATURES:

* **New Provider:** `hubspot` — manage HubSpot portal configuration (config plane, not CRM records) as code
* **New Resource:** `hubspot_property_group` — manage CRM property groups (create/update/import; replace on `object_type`/`name` change)
* **New Resource:** `hubspot_property` — manage CRM object properties incl. enumeration options (list order = display order), archive-on-destroy with name-purgatory error handling, `{object_type}/{name}` import
