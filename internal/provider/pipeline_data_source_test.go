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

// The pipeline data source reads a pipeline by object_type + pipeline_id and
// exposes its stages, sorted by display order, with all server metadata keys.
func TestAccPipelineDataSource_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	create := []stageCfg{
		{label: "Appointment Scheduled", order: 0, meta: map[string]string{"probability": "0.2"}},
		{label: "Closed Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline", create) + `
data "hubspot_pipeline" "sales" {
  object_type = "deals"
  pipeline_id = hubspot_pipeline.test.pipeline_id
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("id"), knownvalue.StringExact("deals/pl_1")),
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("label"), knownvalue.StringExact("Sales Pipeline")),
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("stages"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("label"),
						knownvalue.StringExact("Appointment Scheduled")),
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_1")),
					// Data source surfaces the server-injected isClosed key, unlike
					// the resource (which scopes to managed keys).
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("metadata").AtMapKey("probability"),
						knownvalue.StringExact("1.0")),
					statecheck.ExpectKnownValue("data.hubspot_pipeline.sales",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("metadata").AtMapKey("isClosed"),
						knownvalue.StringExact("true")),
				},
			},
		},
	})
}
