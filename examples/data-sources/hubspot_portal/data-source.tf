# Identify the portal the provider is authenticated against.
data "hubspot_portal" "current" {}

output "portal_id" {
  value = data.hubspot_portal.current.portal_id
}
