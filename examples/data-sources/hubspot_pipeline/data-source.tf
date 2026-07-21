# Look up HubSpot's built-in default deal pipeline and its stages without
# managing it. Useful for referencing a stage_id elsewhere in configuration.
data "hubspot_pipeline" "default_deals" {
  object_type = "deals"
  pipeline_id = "default"
}

# The stage_id of the "Closed Won" stage, resolved by label.
locals {
  closed_won_stage_id = one([
    for s in data.hubspot_pipeline.default_deals.stages : s.stage_id
    if s.label == "Closed Won"
  ])
}
