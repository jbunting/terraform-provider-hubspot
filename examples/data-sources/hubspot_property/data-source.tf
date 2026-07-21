# Read an existing property (including HubSpot-defined ones like lifecyclestage).
data "hubspot_property" "lifecycle" {
  object_type = "contacts"
  name        = "lifecyclestage"
}
