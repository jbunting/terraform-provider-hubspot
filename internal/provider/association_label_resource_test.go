// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// associationLabelConfig renders a hubspot_association_label resource "test"
// from `contacts` to the given `to` object type. inverseLabel is emitted only
// when non-empty (unpaired otherwise).
func associationLabelConfig(baseURL, to, name, label, inverseLabel string) string {
	inv := ""
	if inverseLabel != "" {
		inv = fmt.Sprintf("  inverse_label    = %q\n", inverseLabel)
	}
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_association_label" "test" {
  from_object_type = "contacts"
  to_object_type   = %q
  name             = %q
  label            = %q
%s}
`, to, name, label, inv)
}

// testAccCheckAssociationLabelExists asserts the label recorded in state is
// present in the fake HubSpot by fetching the label list for its pair.
func testAccCheckAssociationLabelExists(baseURL, resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		from := rs.Primary.Attributes["from_object_type"]
		to := rs.Primary.Attributes["to_object_type"]
		typeID := rs.Primary.Attributes["type_id"]
		if from == "" || to == "" || typeID == "" {
			return fmt.Errorf("resource %s missing from/to/type_id in state", resourceName)
		}

		url := fmt.Sprintf("%s/crm/v4/associations/%s/%s/labels", baseURL, from, to)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: expected 200, got %d", url, resp.StatusCode)
		}
		var out struct {
			Results []struct {
				TypeID int64 `json:"typeId"`
			} `json:"results"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		for _, l := range out.Results {
			if fmt.Sprintf("%d", l.TypeID) == typeID {
				return nil
			}
		}
		return fmt.Errorf("label type_id %q not found between %s and %s", typeID, from, to)
	}
}

// TestAccAssociationLabel_basic covers the unpaired-label lifecycle: create,
// a perpetual-diff guard (identical config must plan empty), an in-place label
// edit, and an import round-trip. `name` is ignored on import because HubSpot
// never returns it.
func TestAccAssociationLabel_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: associationLabelConfig(srv.URL, "companies", "decision_maker", "Decision Maker", ""),
				Check:  testAccCheckAssociationLabelExists(srv.URL, "hubspot_association_label.test"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("from_object_type"), knownvalue.StringExact("contacts")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("to_object_type"), knownvalue.StringExact("companies")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Decision Maker")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("type_id"), knownvalue.StringExact("1")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("category"), knownvalue.StringExact("USER_DEFINED")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("inverse_type_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/companies/1")),
				},
			},
			{
				// Identical config must plan empty.
				Config: associationLabelConfig(srv.URL, "companies", "decision_maker", "Decision Maker", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Edit the label text: in-place update, type_id preserved.
				Config: associationLabelConfig(srv.URL, "companies", "decision_maker", "Primary Decision Maker", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_association_label.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Primary Decision Maker")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("type_id"), knownvalue.StringExact("1")),
				},
			},
			{
				ResourceName:      "hubspot_association_label.test",
				ImportState:       true,
				ImportStateId:     "contacts/companies/1",
				ImportStateVerify: true,
				// HubSpot never returns `name`, so it cannot round-trip on import.
				ImportStateVerifyIgnore: []string{"name"},
			},
		},
	})
}

// TestAccAssociationLabel_paired covers a paired label: create with
// inverse_label mints two type IDs, an identical config plans empty, and
// editing both label texts is an in-place update.
func TestAccAssociationLabel_paired(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: associationLabelConfig(srv.URL, "contacts", "manager_report", "Manager", "Report"),
				Check:  testAccCheckAssociationLabelExists(srv.URL, "hubspot_association_label.test"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Manager")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("inverse_label"), knownvalue.StringExact("Report")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("type_id"), knownvalue.StringExact("1")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("inverse_type_id"), knownvalue.StringExact("2")),
				},
			},
			{
				Config: associationLabelConfig(srv.URL, "contacts", "manager_report", "Manager", "Report"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: associationLabelConfig(srv.URL, "contacts", "manager_report", "Line Manager", "Direct Report"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_association_label.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Line Manager")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("inverse_label"), knownvalue.StringExact("Direct Report")),
					statecheck.ExpectKnownValue("hubspot_association_label.test",
						tfjsonpath.New("inverse_type_id"), knownvalue.StringExact("2")),
				},
			},
		},
	})
}

// TestAccAssociationLabel_replaceOnInverseFlip asserts that adding inverse_label
// to a previously unpaired label forces replacement (HubSpot cannot convert a
// label's paired-ness in place).
func TestAccAssociationLabel_replaceOnInverseFlip(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: associationLabelConfig(srv.URL, "companies", "advisor", "Advisor", ""),
			},
			{
				Config:             associationLabelConfig(srv.URL, "companies", "advisor", "Advisor", "Advisee"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_association_label.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// TestAccAssociationLabel_disappears verifies drift handling: an out-of-band
// delete makes a refresh drop the resource and plan a recreate.
func TestAccAssociationLabel_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: associationLabelConfig(srv.URL, "companies", "vanishing", "Vanishing", ""),
			},
			{
				PreConfig: func() {
					f.deleteAssociationLabelOOB("contacts", "companies", 1)
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccAssociationLabel_hubspotDefinedImportError asserts that importing a
// HUBSPOT_DEFINED label surfaces an actionable error rather than adopting an
// unmanageable label.
func TestAccAssociationLabel_hubspotDefinedImportError(t *testing.T) {
	f, srv := newFakeHubSpot(t)
	f.seedAssociationLabel("contacts", "companies", fakeAssocLabel{
		Category: "HUBSPOT_DEFINED",
		TypeID:   1,
		Label:    "Primary",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:        associationLabelConfig(srv.URL, "companies", "primary", "Primary", ""),
				ResourceName:  "hubspot_association_label.test",
				ImportState:   true,
				ImportStateId: "contacts/companies/1",
				ExpectError:   regexp.MustCompile(`(?s)HubSpot-defined`),
			},
		},
	})
}
