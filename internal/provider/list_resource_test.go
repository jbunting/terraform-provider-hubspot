// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// dynamicListConfig renders a DYNAMIC hubspot_list whose single numeric filter
// uses the given threshold, so tests can mutate the filter tree.
func dynamicListConfig(baseURL, name string, threshold int) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_list" "test" {
  name            = %q
  object_type_id  = "0-1"
  processing_type = "DYNAMIC"
  filter_branch = jsonencode({
    filterBranchType = "OR"
    filterBranches = [{
      filterBranchType = "AND"
      filters = [{
        filterType = "PROPERTY"
        property   = "hs_predictivecontactscore_v2"
        operation  = { operationType = "NUMBER", operator = "IS_GREATER_THAN_OR_EQUAL_TO", value = %d }
      }]
    }]
  })
}
`, name, threshold)
}

func manualListConfig(baseURL, name string) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_list" "test" {
  name            = %q
  object_type_id  = "0-2"
  processing_type = "MANUAL"
}
`, name)
}

// TestAccList_dynamicLifecycle is the core test: create a dynamic list, prove an
// identical config plans empty (the semantic-equality type must absorb the
// server-injected filterBranch defaults), edit the filter tree in place, rename
// in place, and import.
func TestAccList_dynamicLifecycle(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: dynamicListConfig(srv.URL, "Engaged Contacts", 12),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("list_id"), knownvalue.StringExact("1")),
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("id"), knownvalue.StringExact("1")),
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("processing_type"), knownvalue.StringExact("DYNAMIC")),
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("object_type_id"), knownvalue.StringExact("0-1")),
				},
			},
			{
				// Identical config must plan empty despite the server injecting
				// filterBranchOperator / includeObjectsWithNoValueSet on read-back.
				Config: dynamicListConfig(srv.URL, "Engaged Contacts", 12),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Change the filter threshold: in-place update (update-list-filters).
				Config: dynamicListConfig(srv.URL, "Engaged Contacts", 20),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_list.test", plancheck.ResourceActionUpdate),
					},
				},
			},
			{
				// Rename: in-place update (update-list-name), list_id preserved.
				Config: dynamicListConfig(srv.URL, "Highly Engaged Contacts", 20),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_list.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("name"), knownvalue.StringExact("Highly Engaged Contacts")),
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("list_id"), knownvalue.StringExact("1")),
				},
			},
			{
				ResourceName:      "hubspot_list.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The server returns filterBranch in its normalized (expanded)
				// form, which differs textually from the configured JSON even
				// though it is semantically equal; skip the raw string compare.
				ImportStateVerifyIgnore: []string{"filter_branch"},
			},
		},
	})
}

// TestAccList_manualLifecycle covers a MANUAL (static) list: no filter_branch,
// identical config plans empty.
func TestAccList_manualLifecycle(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: manualListConfig(srv.URL, "VIP Accounts"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("processing_type"), knownvalue.StringExact("MANUAL")),
					statecheck.ExpectKnownValue("hubspot_list.test",
						tfjsonpath.New("filter_branch"), knownvalue.Null()),
				},
			},
			{
				Config: manualListConfig(srv.URL, "VIP Accounts"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccList_snapshotFilterForcesReplace asserts that editing filter_branch on
// a SNAPSHOT list plans a replacement rather than an in-place update.
func TestAccList_snapshotFilterForcesReplace(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	snapshot := func(threshold int) string {
		return providerConfig(srv.URL) + fmt.Sprintf(`
resource "hubspot_list" "test" {
  name            = "Snapshot"
  object_type_id  = "0-1"
  processing_type = "SNAPSHOT"
  filter_branch = jsonencode({
    filterBranchType = "AND"
    filters = [{
      filterType = "PROPERTY"
      property   = "score"
      operation  = { operationType = "NUMBER", operator = "IS_GREATER_THAN_OR_EQUAL_TO", value = %d }
    }]
  })
}
`, threshold)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: snapshot(5),
			},
			{
				Config:             snapshot(10),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_list.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// TestAccList_replaceOnProcessingTypeChange asserts processing_type is immutable.
func TestAccList_replaceOnProcessingTypeChange(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: manualListConfig(srv.URL, "Mutable Type"),
			},
			{
				Config:             dynamicListConfig(srv.URL, "Mutable Type", 3),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_list.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// TestAccList_disappears verifies drift handling on out-of-band delete.
func TestAccList_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: dynamicListConfig(srv.URL, "Vanishing", 1),
			},
			{
				PreConfig:          func() { f.deleteListOOB("1") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
