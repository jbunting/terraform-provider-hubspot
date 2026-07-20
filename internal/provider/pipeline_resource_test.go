// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// stageCfg is one stage in a rendered pipeline config: a label, a
// display_order, and a metadata map (probability for deals, ticketState for
// tickets). Metadata values are always strings to avoid float round-trips.
type stageCfg struct {
	label string
	order int
	meta  map[string]string
}

// pipelineConfig renders a hubspot_pipeline resource "test" with the given
// object type, label and ordered stages.
func pipelineConfig(baseURL, objectType, label string, stages []stageCfg) string {
	stagesHCL := "  stages = [\n"
	for _, s := range stages {
		stagesHCL += "    {\n"
		stagesHCL += fmt.Sprintf("      label         = %q\n", s.label)
		stagesHCL += fmt.Sprintf("      display_order = %d\n", s.order)
		if len(s.meta) > 0 {
			keys := make([]string, 0, len(s.meta))
			for k := range s.meta {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			pairs := make([]string, 0, len(keys))
			for _, k := range keys {
				pairs = append(pairs, fmt.Sprintf("%s = %q", k, s.meta[k]))
			}
			stagesHCL += fmt.Sprintf("      metadata      = { %s }\n", strings.Join(pairs, ", "))
		}
		stagesHCL += "    },\n"
	}
	stagesHCL += "  ]\n"

	return providerConfig(baseURL) + fmt.Sprintf(`
resource "hubspot_pipeline" "test" {
  object_type = %q
  label       = %q
%s}
`, objectType, label, stagesHCL)
}

// testAccCheckPipelineExists asserts the pipeline recorded in state is present
// in the fake HubSpot by fetching it over HTTP.
func testAccCheckPipelineExists(baseURL, resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		objectType := rs.Primary.Attributes["object_type"]
		pipelineID := rs.Primary.Attributes["pipeline_id"]
		if objectType == "" || pipelineID == "" {
			return fmt.Errorf("resource %s missing object_type/pipeline_id in state", resourceName)
		}

		url := fmt.Sprintf("%s/crm/v3/pipelines/%s/%s", baseURL, objectType, pipelineID)
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
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		if out.ID != pipelineID {
			return fmt.Errorf("fetched pipeline id %q != state pipeline_id %q", out.ID, pipelineID)
		}
		return nil
	}
}

// TestAccPipeline_basic covers the full deal-pipeline lifecycle: create with
// two stages, a perpetual-diff guard (identical config must plan empty —
// catches stage-id/display_order/metadata normalization bugs), an in-place
// update of the pipeline label and a stage label, and an import round-trip.
func TestAccPipeline_basic(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	create := []stageCfg{
		{label: "Appointment Scheduled", order: 0, meta: map[string]string{"probability": "0.2"}},
		{label: "Closed Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}
	updated := []stageCfg{
		{label: "Appointment Booked", order: 0, meta: map[string]string{"probability": "0.2"}},
		{label: "Closed Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline", create),
				Check:  testAccCheckPipelineExists(srv.URL, "hubspot_pipeline.test"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("object_type"), knownvalue.StringExact("deals")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Sales Pipeline")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("pipeline_id"), knownvalue.StringExact("pl_1")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("id"), knownvalue.StringExact("deals/pl_1")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("label"),
						knownvalue.StringExact("Appointment Scheduled")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("metadata").AtMapKey("probability"),
						knownvalue.StringExact("1.0")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_1")),
				},
			},
			{
				// Identical config: any un-absorbed normalization (stage ids,
				// display_order, metadata) surfaces here as a non-empty plan.
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline", create),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Mutate the pipeline label and the first stage's label: must be
				// an in-place update, stage ids preserved by match-by-label.
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline v2", updated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_pipeline.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("label"), knownvalue.StringExact("Sales Pipeline v2")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("label"),
						knownvalue.StringExact("Appointment Booked")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_1")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_2")),
				},
			},
			{
				ResourceName:      "hubspot_pipeline.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccPipeline_reorderStages swaps the display_order (and list position) of
// two stages and asserts it plans as an in-place update, not a replace, with
// each stage's server id preserved (match-by-stage_id, never by index).
func TestAccPipeline_reorderStages(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	create := []stageCfg{
		{label: "Appointment Scheduled", order: 0, meta: map[string]string{"probability": "0.2"}},
		{label: "Closed Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}
	reordered := []stageCfg{
		{label: "Closed Won", order: 0, meta: map[string]string{"probability": "1.0"}},
		{label: "Appointment Scheduled", order: 1, meta: map[string]string{"probability": "0.2"}},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline", create),
			},
			{
				Config: pipelineConfig(srv.URL, "deals", "Sales Pipeline", reordered),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_pipeline.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// Sorted by display_order: Closed Won is now first, but keeps
					// its original server id (stg_2); Appointment keeps stg_1.
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("label"),
						knownvalue.StringExact("Closed Won")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(0).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_2")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("label"),
						knownvalue.StringExact("Appointment Scheduled")),
					statecheck.ExpectKnownValue("hubspot_pipeline.test",
						tfjsonpath.New("stages").AtSliceIndex(1).AtMapKey("stage_id"),
						knownvalue.StringExact("stg_1")),
				},
			},
		},
	})
}

// TestAccPipeline_disappears verifies drift handling: an out-of-band delete
// makes a refresh drop the resource and plan a recreate.
func TestAccPipeline_disappears(t *testing.T) {
	f, srv := newFakeHubSpot(t)

	stages := []stageCfg{
		{label: "New", order: 0, meta: map[string]string{"probability": "0.1"}},
		{label: "Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(srv.URL, "deals", "Vanishing Pipeline", stages),
			},
			{
				PreConfig: func() {
					f.deletePipeline("deals", "pl_1")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccPipeline_replaceOnObjectTypeChange asserts that changing object_type
// forces a destroy-and-recreate. Plan-only so no ticket stages are applied.
func TestAccPipeline_replaceOnObjectTypeChange(t *testing.T) {
	_, srv := newFakeHubSpot(t)

	dealStages := []stageCfg{
		{label: "New", order: 0, meta: map[string]string{"probability": "0.1"}},
		{label: "Won", order: 1, meta: map[string]string{"probability": "1.0"}},
	}
	ticketStages := []stageCfg{
		{label: "Open", order: 0, meta: map[string]string{"ticketState": "OPEN"}},
		{label: "Closed", order: 1, meta: map[string]string{"ticketState": "CLOSED"}},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: pipelineConfig(srv.URL, "deals", "Support", dealStages),
			},
			{
				Config:             pipelineConfig(srv.URL, "tickets", "Support", ticketStages),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_pipeline.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}
