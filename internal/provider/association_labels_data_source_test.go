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

// The association_labels data source lists every label between an object-type
// pair. We create one with the resource and read the pair back, expecting both
// the seeded HubSpot-defined label and the user-defined one.
func TestAccAssociationLabelsDataSource_basic(t *testing.T) {
	f, srv := newFakeHubSpot(t)
	// HubSpot-defined labels have low, reserved type IDs; user labels get high
	// ones. Seed a low ID that won't collide with the minted user label (1).
	f.seedAssociationLabel("contacts", "companies", fakeAssocLabel{
		Category: "HUBSPOT_DEFINED", TypeID: 900, Label: "Primary",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
resource "hubspot_association_label" "custom" {
  from_object_type = "contacts"
  to_object_type   = "companies"
  name             = "decision_maker"
  label            = "Decision Maker"
}

data "hubspot_association_labels" "pair" {
  from_object_type = "contacts"
  to_object_type   = "companies"
  depends_on       = [hubspot_association_label.custom]
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_association_labels.pair",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/companies")),
					statecheck.ExpectKnownValue("data.hubspot_association_labels.pair",
						tfjsonpath.New("labels"), knownvalue.ListSizeExact(2)),
					// Seeded HubSpot-defined label is first (insertion order).
					statecheck.ExpectKnownValue("data.hubspot_association_labels.pair",
						tfjsonpath.New("labels").AtSliceIndex(0).AtMapKey("category"),
						knownvalue.StringExact("HUBSPOT_DEFINED")),
					statecheck.ExpectKnownValue("data.hubspot_association_labels.pair",
						tfjsonpath.New("labels").AtSliceIndex(1).AtMapKey("label"),
						knownvalue.StringExact("Decision Maker")),
					statecheck.ExpectKnownValue("data.hubspot_association_labels.pair",
						tfjsonpath.New("labels").AtSliceIndex(1).AtMapKey("category"),
						knownvalue.StringExact("USER_DEFINED")),
				},
			},
		},
	})
}
