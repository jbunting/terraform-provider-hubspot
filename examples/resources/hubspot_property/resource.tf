# An enumeration property on contacts. Option list position is the display
# order: reorder the list to reorder the options in the HubSpot UI.
resource "hubspot_property" "example" {
  object_type = "contacts"
  name        = "customer_tier"
  label       = "Customer Tier"
  type        = "enumeration"
  field_type  = "select"
  group_name  = "contactinformation"
  description = "Commercial tier of the contact."

  options = [
    { label = "Bronze", value = "bronze" },
    { label = "Silver", value = "silver" },
    {
      label       = "Gold"
      value       = "gold"
      description = "Top tier."
    },
  ]
}
