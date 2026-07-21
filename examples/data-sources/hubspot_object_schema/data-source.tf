# Resolve a custom object's portal-specific object_type_id (2-XXXX) by name.
# Cross-portal configurations should reference custom objects by name and look
# up the ID here, since object_type_ids differ between portals.
data "hubspot_object_schema" "car" {
  object_type = "car"
}

# Use the resolved object_type_id to configure a pipeline on the custom object.
resource "hubspot_pipeline" "car_lifecycle" {
  object_type = data.hubspot_object_schema.car.object_type_id
  label       = "Car Lifecycle"

  stages = [
    { label = "In Stock", display_order = 0 },
    { label = "Sold", display_order = 1 },
  ]
}
