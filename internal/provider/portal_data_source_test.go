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

func TestAccPortalDataSource_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `data "hubspot_portal" "current" {}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("portal_id"), knownvalue.StringExact("123456")),
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("id"), knownvalue.StringExact("123456")),
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("account_type"), knownvalue.StringExact("STANDARD")),
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("time_zone"), knownvalue.StringExact("US/Eastern")),
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("ui_domain"), knownvalue.StringExact("app.hubspot.com")),
				},
			},
		},
	})
}
