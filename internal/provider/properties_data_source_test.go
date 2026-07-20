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

// The properties (plural) data source lists every property on an object type.
// We create two via the resource in the same config and read them all back.
func TestAccPropertiesDataSource_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
resource "hubspot_property" "a" {
  object_type = "contacts"
  name        = "zeta_list"
  label       = "Zeta"
  type        = "string"
  field_type  = "text"
  group_name  = "contactinformation"
}

resource "hubspot_property" "b" {
  object_type = "contacts"
  name        = "alpha_list"
  label       = "Alpha"
  type        = "string"
  field_type  = "text"
  group_name  = "contactinformation"
}

data "hubspot_properties" "all" {
  object_type = "contacts"
  depends_on  = [hubspot_property.a, hubspot_property.b]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("object_type"), knownvalue.StringExact("contacts")),
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts")),
					// Fake returns results sorted by name: alpha_list before zeta_list.
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("properties").AtSliceIndex(0).AtMapKey("name"),
						knownvalue.StringExact("alpha_list")),
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("properties").AtSliceIndex(0).AtMapKey("label"),
						knownvalue.StringExact("Alpha")),
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("properties").AtSliceIndex(0).AtMapKey("field_type"),
						knownvalue.StringExact("text")),
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("properties").AtSliceIndex(1).AtMapKey("name"),
						knownvalue.StringExact("zeta_list")),
					statecheck.ExpectKnownValue("data.hubspot_properties.all",
						tfjsonpath.New("properties"),
						knownvalue.ListSizeExact(2)),
				},
			},
		},
	})
}

// Archived properties are excluded by default. This mirrors the API: a plain
// list omits archived properties, so a config that only ever archived a
// property (none created) yields an empty list, not an error.
func TestAccPropertiesDataSource_empty(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "hubspot_properties" "none" {
  object_type = "tickets"
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_properties.none",
						tfjsonpath.New("properties"), knownvalue.ListSizeExact(0)),
				},
			},
		},
	})
}

func TestAccPropertiesDataSource_requiresObjectType(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(srv.URL) + `data "hubspot_properties" "bad" {}`,
				ExpectError: regexp.MustCompile(`(?i)object_type`),
			},
		},
	})
}
