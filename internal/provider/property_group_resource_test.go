// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// propertyGroupConfig renders a full config (provider + one group resource).
func propertyGroupConfig(baseURL, name, label string) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_property_group" "test" {
  object_type = "contacts"
  name        = %q
  label       = %q
}
`, name, label)
}

// checkPropertyGroupExists asserts a contacts group really exists in the
// fake HubSpot by querying it over HTTP, independent of Terraform state.
func checkPropertyGroupExists(serverURL, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		req, err := http.NewRequest(http.MethodGet,
			fmt.Sprintf("%s/crm/v3/properties/contacts/groups/%s", serverURL, name), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer pat-na1-test")
		httpResp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = httpResp.Body.Close() }()
		if httpResp.StatusCode != http.StatusOK {
			return fmt.Errorf("property group contacts/%s not found in fake HubSpot (status %d)",
				name, httpResp.StatusCode)
		}
		return nil
	}
}

func TestAccPropertyGroup_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and verify all attributes land in state.
			{
				Config: propertyGroupConfig(srv.URL, "my_group", "My Group"),
				Check:  checkPropertyGroupExists(srv.URL, "my_group"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("name"), knownvalue.StringExact("my_group")),
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("label"), knownvalue.StringExact("My Group")),
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/my_group")),
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("display_order"), knownvalue.Int64Exact(0)),
				},
			},
			// Same config again: no perpetual diff.
			{
				Config: propertyGroupConfig(srv.URL, "my_group", "My Group"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// Change label: in-place update, not replace.
			{
				Config: propertyGroupConfig(srv.URL, "my_group", "My Group (renamed)"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_property_group.test",
							plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("label"), knownvalue.StringExact("My Group (renamed)")),
				},
			},
			// Import round-trips the full state.
			{
				ResourceName:      "hubspot_property_group.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "contacts/my_group",
			},
		},
	})
}

func TestAccPropertyGroup_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: propertyGroupConfig(srv.URL, "vanishing_group", "Vanishing Group"),
				Check:  checkPropertyGroupExists(srv.URL, "vanishing_group"),
			},
			// Delete the group out-of-band; a refresh must remove it from
			// state and plan a recreate instead of erroring.
			{
				PreConfig: func() {
					f.deleteGroup("contacts", "vanishing_group")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccPropertyGroup_replaceOnRename(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: propertyGroupConfig(srv.URL, "group_before", "Renamable Group"),
				Check:  checkPropertyGroupExists(srv.URL, "group_before"),
			},
			// Changing the immutable name must plan a replace.
			{
				Config: propertyGroupConfig(srv.URL, "group_after", "Renamable Group"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_property_group.test",
							plancheck.ResourceActionReplace),
					},
				},
				Check: checkPropertyGroupExists(srv.URL, "group_after"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/group_after")),
				},
			},
		},
	})
}
