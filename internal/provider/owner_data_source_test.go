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

func TestAccOwnerDataSource_byEmail(t *testing.T) {
	f, srv := newFakeHubSpot(t)
	f.seedOwner(fakeOwner{ID: "10", Email: "rep@example.com", FirstName: "Ada", LastName: "Rep", UserID: 99})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "hubspot_owner" "rep" {
  email = "rep@example.com"
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_owner.rep",
						tfjsonpath.New("id"), knownvalue.StringExact("10")),
					statecheck.ExpectKnownValue("data.hubspot_owner.rep",
						tfjsonpath.New("first_name"), knownvalue.StringExact("Ada")),
					statecheck.ExpectKnownValue("data.hubspot_owner.rep",
						tfjsonpath.New("user_id"), knownvalue.Int64Exact(99)),
				},
			},
		},
	})
}

func TestAccOwnerDataSource_byID(t *testing.T) {
	f, srv := newFakeHubSpot(t)
	f.seedOwner(fakeOwner{ID: "20", Email: "mgr@example.com", FirstName: "Grace", LastName: "Mgr", UserID: 42})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "hubspot_owner" "mgr" {
  owner_id = "20"
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_owner.mgr",
						tfjsonpath.New("email"), knownvalue.StringExact("mgr@example.com")),
				},
			},
		},
	})
}

func TestAccOwnerDataSource_notFound(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "hubspot_owner" "missing" {
  email = "nobody@example.com"
}`,
				ExpectError: regexp.MustCompile(`(?i)no.*owner`),
			},
		},
	})
}

func TestAccOwnerDataSource_requiresExactlyOne(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(srv.URL) + `data "hubspot_owner" "bad" {}`,
				ExpectError: regexp.MustCompile(`(?i)(exactly one|email|owner_id)`),
			},
		},
	})
}
