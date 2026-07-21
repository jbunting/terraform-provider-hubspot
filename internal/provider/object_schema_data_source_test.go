// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// The object_schema data source resolves a custom object by name to its
// portal-specific object_type_id and display configuration.
func TestAccObjectSchemaDataSource_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: objectSchemaConfig(srv.URL, "Cars") + `
data "hubspot_object_schema" "car" {
  object_type = hubspot_object_schema.car.name
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("name"), knownvalue.StringExact("car")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("object_type_id"), knownvalue.StringExact("2-1")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("id"), knownvalue.StringExact("2-1")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("label_singular"), knownvalue.StringExact("Car")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("label_plural"), knownvalue.StringExact("Cars")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("primary_display_property"), knownvalue.StringExact("model")),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("searchable_properties"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.hubspot_object_schema.car",
						tfjsonpath.New("fully_qualified_name"), knownvalue.StringExact("p123456_car")),
				},
			},
		},
	})
}
