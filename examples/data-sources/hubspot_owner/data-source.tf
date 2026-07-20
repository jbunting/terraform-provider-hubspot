# Look up an owner by email (or set owner_id instead — exactly one).
data "hubspot_owner" "rep" {
  email = "rep@example.com"
}

# Use data.hubspot_owner.rep.id as a hubspot_owner_id property value.
