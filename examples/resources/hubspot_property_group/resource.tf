# Manage a custom property group on contacts. Properties created with
# hubspot_property can reference it via group_name.
resource "hubspot_property_group" "example" {
  object_type = "contacts"
  name        = "my_group"
  label       = "My Group"

  # Optional: position of the group in the HubSpot UI (lower sorts first).
  display_order = 5
}
