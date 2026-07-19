terraform {
  required_providers {
    hubspot = {
      source = "revosai/hubspot"
    }
  }
}

provider "hubspot" {
  # The private app access token is read from the
  # HUBSPOT_ACCESS_TOKEN environment variable.
}
