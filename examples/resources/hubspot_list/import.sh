# Lists are imported using their ILS list ID.
# The configured filter_branch is reconciled semantically on the next plan, so a
# textual difference from HubSpot's normalized form does not force a change.
terraform import hubspot_list.engaged_contacts '611'
