# A DYNAMIC contact list defined by a filter tree. object_type_id "0-1" is the
# contacts object. The filter_branch is passed through as JSON: HubSpot expands
# it with server-injected defaults on read-back, but the provider compares it
# semantically so an unchanged config plans empty. Editing the filter tree is an
# in-place update (update-list-filters); changing name updates in place too.
resource "hubspot_list" "engaged_contacts" {
  name            = "Engaged Contacts"
  object_type_id  = "0-1"
  processing_type = "DYNAMIC"

  filter_branch = jsonencode({
    filterBranchType = "OR"
    filterBranches = [
      {
        filterBranchType = "AND"
        filters = [
          {
            filterType = "PROPERTY"
            property   = "hs_predictivecontactscore_v2"
            operation = {
              operationType = "NUMBER"
              operator      = "IS_GREATER_THAN_OR_EQUAL_TO"
              value         = 12
            }
          },
        ]
      },
    ]
  })
}

# A MANUAL (static) list has no filter tree — membership is managed separately.
resource "hubspot_list" "vip_accounts" {
  name            = "VIP Accounts"
  object_type_id  = "0-2"
  processing_type = "MANUAL"
}
