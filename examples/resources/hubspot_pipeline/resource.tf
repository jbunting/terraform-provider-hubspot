# A deal pipeline with inline stages. Stages are matched across plan and state
# by stage_id (never by list position), so reordering or relabeling a stage is
# an in-place update rather than a delete-and-recreate. Deal probability is a
# string to avoid perpetual float round-trip diffs.
resource "hubspot_pipeline" "sales" {
  object_type = "deals"
  label       = "Sales Pipeline"

  stages = [
    {
      label         = "Appointment Scheduled"
      display_order = 0
      metadata      = { probability = "0.2" }
    },
    {
      label         = "Contract Sent"
      display_order = 1
      metadata      = { probability = "0.8" }
    },
    {
      label         = "Closed Won"
      display_order = 2
      metadata      = { probability = "1.0" }
    },
  ]
}

# A ticket pipeline. Ticket stages use ticketState (OPEN or CLOSED) instead of
# probability.
resource "hubspot_pipeline" "support" {
  object_type = "tickets"
  label       = "Support Pipeline"

  stages = [
    {
      label         = "New"
      display_order = 0
      metadata      = { ticketState = "OPEN" }
    },
    {
      label         = "Resolved"
      display_order = 1
      metadata      = { ticketState = "CLOSED" }
    },
  ]
}
