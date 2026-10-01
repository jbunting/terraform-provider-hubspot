# A custom object schema (Enterprise tier). The inline `properties` seed the
# object at creation; manage them over their lifetime with hubspot_property.
resource "hubspot_object_schema" "car" {
  name = "car"

  labels = {
    singular = "Car"
    plural   = "Cars"
  }

  primary_display_property     = "model"
  secondary_display_properties = ["vin"]
  required_properties          = ["model"]
  searchable_properties        = ["model", "vin"]
  description                  = "Vehicles in the fleet."

  properties = [
    {
      name       = "model"
      label      = "Model"
      type       = "string"
      field_type = "text"
    },
    {
      name       = "vin"
      label      = "VIN"
      type       = "string"
      field_type = "text"
    },
  ]

  associated_objects = ["CONTACT"]

  # Required before `terraform destroy` (or a name change) will delete the
  # object type and all its records. Omit to keep the safety guard.
  force_delete = false
}

# Add more properties over time with hubspot_property, referencing the schema's
# object_type_id (2-XXXXX).
resource "hubspot_property" "color" {
  object_type = hubspot_object_schema.car.object_type_id
  name        = "color"
  label       = "Color"
  type        = "string"
  field_type  = "text"
  group_name  = "carinformation"
}

# Association type IDs are portal-specific. Look one up by direction from
# `associations` (here: car -> contact, for linking records via the
# Associations API).
output "car_to_contact_association_type_id" {
  value = one([
    for a in hubspot_object_schema.car.associations : a.id
    if a.to_object_type_id == "0-1"
  ])
}
