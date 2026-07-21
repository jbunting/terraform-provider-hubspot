# A one-directional (unpaired) association label between contacts and companies.
# `name` is the immutable internal identifier used at creation; HubSpot never
# returns it, so changing it forces replacement and it is not recoverable on
# import. `label` is the human-readable text shown in the UI and is mutable.
resource "hubspot_association_label" "decision_maker" {
  from_object_type = "contacts"
  to_object_type   = "companies"
  name             = "decision_maker"
  label            = "Decision Maker"
}

# A paired (two-directional) label: setting inverse_label makes HubSpot mint two
# association type IDs — one per direction (type_id and inverse_type_id). Adding
# or removing inverse_label later forces replacement, because HubSpot cannot
# convert an unpaired label into a paired one in place. Editing either label's
# text is an in-place update.
resource "hubspot_association_label" "manager" {
  from_object_type = "contacts"
  to_object_type   = "contacts"
  name             = "manager_report"
  label            = "Manager"
  inverse_label    = "Report"
}
