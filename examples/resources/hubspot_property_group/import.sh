# Property groups are imported using "{object_type}/{name}".
terraform import hubspot_property_group.example 'contacts/my_group'

# OpenTofu uses the same ID (quote the composite ID here too):
tofu import hubspot_property_group.example 'contacts/my_group'
