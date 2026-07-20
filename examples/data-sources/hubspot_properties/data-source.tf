# List every property defined on the contacts object.
data "hubspot_properties" "contacts" {
  object_type = "contacts"
}

# Filter with HCL: the internal names of the custom (non-HubSpot-defined)
# properties only.
output "custom_contact_properties" {
  value = [
    for p in data.hubspot_properties.contacts.properties : p.name
    if !p.hubspot_defined
  ]
}
