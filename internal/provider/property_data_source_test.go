// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// The property data source reads any property — including HubSpot-defined
// ones the provider never manages. Here we create one via the resource in
// the same config and read it back through the data source.
func TestAccPropertyDataSource_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
resource "hubspot_property" "src" {
  object_type = "contacts"
  name        = "tier_ds"
  label       = "Tier DS"
  type        = "string"
  field_type  = "text"
  group_name  = "contactinformation"
  description = "Read me back."
}

data "hubspot_property" "src" {
  object_type = "contacts"
  name        = hubspot_property.src.name
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_property.src",
						tfjsonpath.New("label"), knownvalue.StringExact("Tier DS")),
					statecheck.ExpectKnownValue("data.hubspot_property.src",
						tfjsonpath.New("type"), knownvalue.StringExact("string")),
					statecheck.ExpectKnownValue("data.hubspot_property.src",
						tfjsonpath.New("field_type"), knownvalue.StringExact("text")),
					statecheck.ExpectKnownValue("data.hubspot_property.src",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/tier_ds")),
					statecheck.ExpectKnownValue("data.hubspot_property.src",
						tfjsonpath.New("hubspot_defined"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

func TestAccPropertyDataSource_notFound(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "hubspot_property" "missing" {
  object_type = "contacts"
  name        = "does_not_exist"
}`,
				ExpectError: regexp.MustCompile(`(?i)not found`),
			},
		},
	})
}
