# List every association label between contacts and companies, including
# HubSpot-defined ones. Association type IDs are portal-specific, so this is the
# way to resolve a label by name (e.g. to reference it from a record association).
data "hubspot_association_labels" "contact_company" {
  from_object_type = "contacts"
  to_object_type   = "companies"
}

# Resolve a specific label's portal-specific type_id by its display text.
locals {
  decision_maker_type_id = one([
    for l in data.hubspot_association_labels.contact_company.labels : l.type_id
    if l.label == "Decision Maker"
  ])
}
