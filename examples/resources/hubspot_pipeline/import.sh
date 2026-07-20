# Pipelines are imported using "{object_type}/{pipeline_id}". This is also how
# you adopt HubSpot's non-deletable default pipeline instead of declaring a new
# one.
terraform import hubspot_pipeline.sales 'deals/default'
