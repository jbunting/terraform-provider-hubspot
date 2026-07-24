# Properties are imported using "{object_type}/{name}".
terraform import hubspot_property.example 'contacts/customer_tier'

# OpenTofu uses the same ID (quote the composite ID here too):
tofu import hubspot_property.example 'contacts/customer_tier'
