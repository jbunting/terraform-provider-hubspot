// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func objectSchemaConfig(url, plural string) string {
	return providerConfig(url) + `
resource "hubspot_object_schema" "car" {
  name         = "car"
  force_delete = true

  labels = {
    singular = "Car"
    plural   = "` + plural + `"
  }

  primary_display_property     = "model"
  secondary_display_properties = ["vin"]
  required_properties          = ["model"]
  searchable_properties        = ["model", "vin"]
  description                  = "Cars in the fleet."

  properties = [
    {
      name       = "model"
      label      = "Model"
      type       = "string"
      field_type = "text"
    },
    {
      name       = "vin"
      label      = "VIN"
      type       = "string"
      field_type = "text"
    },
  ]

  associated_objects = ["CONTACT"]
}`
}

// Full lifecycle: create → perpetual-diff guard → update mutable fields →
// import round-trip. Exercised against the hermetic fake.
func TestAccObjectSchema_lifecycle(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: objectSchemaConfig(srv.URL, "Cars"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("object_type_id"), knownvalue.StringExact("2-1")),
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("id"), knownvalue.StringExact("2-1")),
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("fully_qualified_name"), knownvalue.StringExact("p123456_car")),
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("labels").AtMapKey("singular"), knownvalue.StringExact("Car")),
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("primary_display_property"), knownvalue.StringExact("model")),
				},
			},
			// Identical config must plan empty (no perpetual diff from server
			// normalization or bootstrap-property refresh).
			{
				Config: objectSchemaConfig(srv.URL, "Cars"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// Update the plural label in place (PATCH, not replace).
			{
				Config: objectSchemaConfig(srv.URL, "Automobiles"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_object_schema.car", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_object_schema.car",
						tfjsonpath.New("labels").AtMapKey("plural"), knownvalue.StringExact("Automobiles")),
				},
			},
			// Import round-trip by objectTypeId. Bootstrap-only fields and
			// force_delete aren't returned by the API, so they're not verified.
			{
				ResourceName:            "hubspot_object_schema.car",
				ImportState:             true,
				ImportStateId:           "2-1",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"properties", "associated_objects", "force_delete"},
			},
		},
	})
}

// Out-of-band deletion (someone deletes the schema in HubSpot) is detected on
// the next refresh and the resource is recreated.
func TestAccObjectSchema_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: objectSchemaConfig(srv.URL, "Cars"),
				Check: func(*terraform.State) error {
					f.deleteSchema("2-1") // vanish out of band
					return nil
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Bootstrap `properties` are create-time only: editing one in configuration
// after creation produces no plan (the edit is intentionally ignored).
func TestAccObjectSchema_bootstrapPropertiesIgnored(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	withProp := func(label string) string {
		return providerConfig(srv.URL) + `
resource "hubspot_object_schema" "b" {
  name         = "boat"
  force_delete = true
  labels = { singular = "Boat", plural = "Boats" }
  primary_display_property = "hull"
  properties = [{ name = "hull", label = "` + label + `", type = "string", field_type = "text" }]
}`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: withProp("Hull")},
			{
				// Change the bootstrap property's label; expect NO diff.
				Config: withProp("Hull Number"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// Changing the immutable name plans as a replacement (RequiresReplace).
func TestAccObjectSchema_nameRequiresReplace(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	cfg := func(name string) string {
		return providerConfig(srv.URL) + `
resource "hubspot_object_schema" "x" {
  name         = "` + name + `"
  force_delete = true
  labels = { singular = "X", plural = "Xs" }
  primary_display_property = "n"
  properties = [{ name = "n", label = "N", type = "string", field_type = "text" }]
}`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("widget")},
			{
				Config: cfg("gadget"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_object_schema.x", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

// Destroying without force_delete = true must fail with actionable guidance,
// because a delete permanently removes the object type and all its records.
func TestAccObjectSchema_deleteRequiresForce(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	cfg := func(force bool) string {
		f := ""
		if force {
			f = "force_delete = true"
		}
		return providerConfig(srv.URL) + `
resource "hubspot_object_schema" "guard" {
  name = "safe"
  ` + f + `
  labels = { singular = "Safe", plural = "Safes" }
  primary_display_property = "n"
  properties = [{ name = "n", label = "N", type = "string", field_type = "text" }]
}`
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg(false)},
			{
				Config:      cfg(false),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?i)force_delete`),
			},
			// Flip force_delete on so the harness's final cleanup destroy
			// succeeds instead of hitting the guard again.
			{Config: cfg(true)},
		},
	})
}
