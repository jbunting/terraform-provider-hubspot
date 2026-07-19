// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// enumPropertyConfig renders an enumeration property on contacts whose
// options are given as "label:value" pairs; list position is the order.
func enumPropertyConfig(baseURL, name, label string, options [][2]string) string {
	optionsHCL := ""
	for _, o := range options {
		optionsHCL += fmt.Sprintf("    { label = %q, value = %q },\n", o[0], o[1])
	}
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_property" "test" {
  object_type = "contacts"
  name        = %q
  label       = %q
  type        = "enumeration"
  field_type  = "select"
  group_name  = "contactinformation"

  options = [
%s  ]
}
`, name, label, optionsHCL)
}

// stringPropertyConfig renders a minimal string property on contacts.
func stringPropertyConfig(baseURL, name, label string) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_property" "test" {
  object_type = "contacts"
  name        = %q
  label       = %q
  type        = "string"
  field_type  = "text"
  group_name  = "contactinformation"
}
`, name, label)
}

// TestAccProperty_basic covers the full lifecycle of an enumeration property
// with ordered options: create + exact normalized state, a perpetual-diff
// guard step (identical config must plan empty — catches option displayOrder
// and property displayOrder normalization bugs), an in-place update that
// changes the label and reorders options, and import round-trip.
func TestAccProperty_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	optionsCreate := [][2]string{{"Bronze", "bronze"}, {"Silver", "silver"}, {"Gold", "gold"}}
	optionsReordered := [][2]string{{"Silver", "silver"}, {"Bronze", "bronze"}, {"Gold", "gold"}}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enumPropertyConfig(srv.URL, "customer_tier", "Customer Tier", optionsCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/customer_tier")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Customer Tier")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("type"), knownvalue.StringExact("enumeration")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("field_type"), knownvalue.StringExact("select")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("group_name"), knownvalue.StringExact("contactinformation")),
					// Server normalization: displayOrder 0 is rewritten to -1.
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("display_order"), knownvalue.Int64Exact(-1)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("hidden"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("form_field"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("has_unique_value"), knownvalue.Bool(false)),
					// Option round-trip: 3 options come back in list order,
					// without any display_order attribute in state.
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options"), knownvalue.ListSizeExact(3)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(0).AtMapKey("value"), knownvalue.StringExact("bronze")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(1).AtMapKey("value"), knownvalue.StringExact("silver")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(2).AtMapKey("value"), knownvalue.StringExact("gold")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(0).AtMapKey("hidden"), knownvalue.Bool(false)),
				},
			},
			{
				// Identical config: any normalization the provider fails to
				// absorb (option displayOrder, property displayOrder 0 -> -1)
				// shows up here as a non-empty plan.
				Config: enumPropertyConfig(srv.URL, "customer_tier", "Customer Tier", optionsCreate),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Mutate label and swap the first two options: must be an
				// in-place update, and the new order must land in state.
				Config: enumPropertyConfig(srv.URL, "customer_tier", "Customer Tier v2", optionsReordered),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_property.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Customer Tier v2")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(0).AtMapKey("value"), knownvalue.StringExact("silver")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(1).AtMapKey("value"), knownvalue.StringExact("bronze")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(2).AtMapKey("value"), knownvalue.StringExact("gold")),
				},
			},
			{
				ResourceName:      "hubspot_property.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccProperty_normalization pins the server-normalization contract for a
// minimal string property: displayOrder is never sent when unconfigured, the
// server default -1 is adopted into state, and an identical second config
// plans empty (no perpetual diff from the 0 -> -1 rewrite).
func TestAccProperty_normalization(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: stringPropertyConfig(srv.URL, "plain_text", "Plain Text"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/plain_text")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("display_order"), knownvalue.Int64Exact(-1)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("description"), knownvalue.Null()),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options"), knownvalue.Null()),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("hidden"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("form_field"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("has_unique_value"), knownvalue.Bool(false)),
				},
			},
			{
				Config: stringPropertyConfig(srv.URL, "plain_text", "Plain Text"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccProperty_disappears verifies drift handling: when the property is
// deleted out-of-band, a refresh removes it from state and plans recreate.
func TestAccProperty_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: stringPropertyConfig(srv.URL, "vanishing", "Vanishing"),
			},
			{
				PreConfig: func() {
					f.deleteProperty("contacts", "vanishing")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccProperty_archiveBlocksRecreate pins HubSpot's "name purgatory":
// destroy archives the property, and re-creating the same name must fail
// with an actionable error explaining the ~90-day name lock.
func TestAccProperty_archiveBlocksRecreate(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: stringPropertyConfig(srv.URL, "locked_name", "Locked Name"),
			},
			{
				// Removing the resource applies a destroy, which archives the
				// property in HubSpot (and in the fake).
				Config: providerConfig(srv.URL),
			},
			{
				// Same name again: create must surface the purgatory error.
				Config:      stringPropertyConfig(srv.URL, "locked_name", "Locked Name"),
				ExpectError: regexp.MustCompile(`(?s)archived`),
			},
		},
	})
}

// TestAccProperty_replaceOnTypeChange verifies that changing the immutable
// `type` plans a destroy-and-recreate. Plan-only: applying the replace would
// hit name purgatory in the fake (delete archives the name).
func TestAccProperty_replaceOnTypeChange(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enumPropertyConfig(srv.URL, "morphing", "Morphing",
					[][2]string{{"One", "one"}, {"Two", "two"}}),
			},
			{
				// Same name, different type: must plan a replace. PlanOnly
				// steps skip PreApply plan checks, so assert the replace in
				// PostApplyPreRefresh (which PlanOnly steps do run).
				Config:             stringPropertyConfig(srv.URL, "morphing", "Morphing"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_property.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}
