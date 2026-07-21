# Association labels are imported using "{from_object_type}/{to_object_type}/{type_id}".
# The type_id is HubSpot's portal-specific association type ID; look it up with
# the hubspot_association_labels data source. Note: HubSpot never returns the
# label's `name`, so it is not populated on import — set it in configuration
# afterwards (it is immutable and only used at creation).
terraform import hubspot_association_label.decision_maker 'contacts/companies/145'
