// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

// Real-portal acceptance tests: the same lifecycle coverage as the hermetic
// suite, but executed against a live HubSpot portal. Their purpose is to
// catch real-API normalization behavior the fake cannot fully emulate
// (perpetual diffs, import round-trip gaps).
//
// Env contract (all three required to run; anything missing => skip):
//
//	TF_ACC=1                 standard acceptance-test gate (resource.Test).
//	HUBSPOT_ACCESS_TOKEN     private app token for the DEDICATED TEST portal
//	                         (also read natively by the provider itself).
//	HUBSPOT_TEST_PORTAL_ID   the expected hub/portal ID (digits).
//
// Safety guard: before any mutation, requireRealPortal calls
// GET /account-info/v3/details with the bearer token and t.Fatal-s unless the
// returned portalId equals HUBSPOT_TEST_PORTAL_ID. This prevents ever running
// destructive tests against a production portal when the wrong token leaks
// into the environment (Datadog's isTestOrg pattern). The check runs once per
// process (sync.Once) and its verdict is cached.
//
// All resource names are randomized with the tf_acc_test_ prefix: HubSpot
// keeps archived property names reserved for ~90 days, so fixed names would
// brick reruns. Sweepers (make sweep / go test -sweep=all) delete leaked
// tf_acc_test_* contacts properties and groups; they no-op with a log message
// when the env contract is not satisfied.
//
// Real tests never use t.Parallel: they share one portal and its rate limits.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	realAPIBase = "https://api.hubapi.com"

	// realTestPrefix marks every resource created by this layer so the
	// sweepers can identify (and only ever touch) test leftovers.
	realTestPrefix = "tf_acc_test_"
)

var realHTTPClient = &http.Client{Timeout: 30 * time.Second}

// TestMain enables the -sweep/-sweep-run flags for the sweepers registered
// below; without -sweep it just runs the test binary as usual.
func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("hubspot_property", &resource.Sweeper{
		Name: "hubspot_property",
		F:    sweepRealContactProperties,
	})
	resource.AddTestSweepers("hubspot_property_group", &resource.Sweeper{
		Name:         "hubspot_property_group",
		Dependencies: []string{"hubspot_property"}, // empty groups delete cleanly
		F:            sweepRealContactPropertyGroups,
	})
}

// ---------------------------------------------------------------------------
// Safety guard
// ---------------------------------------------------------------------------

var (
	realPortalOnce sync.Once
	realPortalErr  error
)

// requireRealPortal skips the test unless the real-portal env contract is
// satisfied, then verifies (once per process) that HUBSPOT_ACCESS_TOKEN
// really belongs to portal HUBSPOT_TEST_PORTAL_ID, t.Fatal-ing on any
// mismatch so no mutation can ever reach an unexpected portal.
func requireRealPortal(t *testing.T) {
	t.Helper()
	token := os.Getenv("HUBSPOT_ACCESS_TOKEN")
	want := os.Getenv("HUBSPOT_TEST_PORTAL_ID")
	if token == "" || want == "" {
		t.Skip("skipping real-portal acceptance test: set HUBSPOT_ACCESS_TOKEN and " +
			"HUBSPOT_TEST_PORTAL_ID (dedicated test portal only) to run it")
	}
	if os.Getenv("TF_ACC") == "" {
		// resource.Test would skip anyway; skipping here first avoids the
		// network round-trip below.
		t.Skip("skipping real-portal acceptance test: TF_ACC is not set")
	}
	realPortalOnce.Do(func() {
		realPortalErr = verifyRealPortal(token, want)
	})
	if realPortalErr != nil {
		t.Fatalf("SAFETY GUARD: refusing to run destructive tests: %v", realPortalErr)
	}
}

// verifyRealPortal confirms the token's portal identity via the read-only
// account-info endpoint.
func verifyRealPortal(token, wantPortalID string) error {
	req, err := http.NewRequest(http.MethodGet, realAPIBase+"/account-info/v3/details", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := realHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("querying account-info/v3/details: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("account-info/v3/details returned status %d; cannot confirm the "+
			"token belongs to test portal %s", resp.StatusCode, wantPortalID)
	}
	var details struct {
		PortalID json.Number `json:"portalId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return fmt.Errorf("decoding account-info/v3/details: %w", err)
	}
	if got := details.PortalID.String(); got != wantPortalID {
		return fmt.Errorf("HUBSPOT_ACCESS_TOKEN belongs to portal %s but HUBSPOT_TEST_PORTAL_ID "+
			"is %s — never point these tests at anything but the dedicated test portal", got, wantPortalID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Real-API HTTP helpers (plain net/http; test code stays independent of the
// provider's client package)
// ---------------------------------------------------------------------------

// realAPIStatus performs a GET against the real API and returns the HTTP
// status code, draining and closing the body.
func realAPIStatus(token, path string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, realAPIBase+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := realHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// realAPIListNames GETs a properties-API list endpoint and returns the name
// of every result.
func realAPIListNames(token, path string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, realAPIBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := realHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", path, resp.StatusCode)
	}
	var body struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding GET %s: %w", path, err)
	}
	names := make([]string, 0, len(body.Results))
	for _, r := range body.Results {
		names = append(names, r.Name)
	}
	return names, nil
}

// realAPIDelete issues a DELETE and treats 2xx and 404 (already gone) as
// success.
func realAPIDelete(token, path string) error {
	req, err := http.NewRequest(http.MethodDelete, realAPIBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := realHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("DELETE %s returned status %d", path, resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Config renderers: real tests configure the provider from the environment
// only (the provider reads HUBSPOT_ACCESS_TOKEN natively; no base_url).
// ---------------------------------------------------------------------------

func realProviderConfig() string {
	return `
provider "hubspot" {}
`
}

func realPropertyGroupConfig(name, label string) string {
	return realProviderConfig() + fmt.Sprintf(`
resource "hubspot_property_group" "test" {
  object_type = "contacts"
  name        = %q
  label       = %q
}
`, name, label)
}

// realEnumPropertyConfig renders a tf_acc_test_ group plus an enumeration
// property inside it; the group_name reference makes Terraform create the
// group first and destroy it last.
func realEnumPropertyConfig(groupName, propertyName, label string, options [][2]string) string {
	optionsHCL := ""
	for _, o := range options {
		optionsHCL += fmt.Sprintf("    { label = %q, value = %q },\n", o[0], o[1])
	}
	return realProviderConfig() + fmt.Sprintf(`
resource "hubspot_property_group" "test" {
  object_type = "contacts"
  name        = %q
  label       = "TF Acc Test Group"
}

resource "hubspot_property" "test" {
  object_type = "contacts"
  name        = %q
  label       = %q
  type        = "enumeration"
  field_type  = "select"
  group_name  = hubspot_property_group.test.name

  options = [
%s  ]
}
`, groupName, propertyName, label, optionsHCL)
}

// randomRealName returns a HubSpot-legal (lowercase letters, digits,
// underscores) randomized name: tf_acc_test_<infix><random>.
func randomRealName(infix string) string {
	return realTestPrefix + infix + acctest.RandStringFromCharSet(12, acctest.CharSetAlpha)
}

// ---------------------------------------------------------------------------
// Destroy verification
// ---------------------------------------------------------------------------

// checkRealResourcesDestroyed asserts against the real API that every
// hubspot_property_group in state is gone (plain GET 404s — groups are hard
// deleted) and every hubspot_property is archived (plain GET 404s, GET with
// ?archived=true still finds it: HubSpot soft-deletes properties and reserves
// the name ~90 days, which is why every name here is randomized).
func checkRealResourcesDestroyed(s *terraform.State) error {
	token := os.Getenv("HUBSPOT_ACCESS_TOKEN")
	if token == "" {
		return fmt.Errorf("HUBSPOT_ACCESS_TOKEN disappeared mid-test; cannot verify destroy")
	}
	for addr, rs := range s.RootModule().Resources {
		parts := strings.SplitN(rs.Primary.ID, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("%s: unexpected ID format %q", addr, rs.Primary.ID)
		}
		objectType, name := parts[0], parts[1]
		switch rs.Type {
		case "hubspot_property_group":
			status, err := realAPIStatus(token,
				fmt.Sprintf("/crm/v3/properties/%s/groups/%s", objectType, name))
			if err != nil {
				return fmt.Errorf("%s: checking destroy: %w", addr, err)
			}
			if status != http.StatusNotFound {
				return fmt.Errorf("%s: property group %s/%s still exists after destroy (status %d)",
					addr, objectType, name, status)
			}
		case "hubspot_property":
			path := fmt.Sprintf("/crm/v3/properties/%s/%s", objectType, name)
			status, err := realAPIStatus(token, path)
			if err != nil {
				return fmt.Errorf("%s: checking destroy: %w", addr, err)
			}
			if status != http.StatusNotFound {
				return fmt.Errorf("%s: property %s/%s still live after destroy (status %d)",
					addr, objectType, name, status)
			}
			status, err = realAPIStatus(token, path+"?archived=true")
			if err != nil {
				return fmt.Errorf("%s: checking archived state: %w", addr, err)
			}
			if status != http.StatusOK {
				return fmt.Errorf("%s: property %s/%s not found even with archived=true (status %d); "+
					"expected destroy to archive it", addr, objectType, name, status)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests (no t.Parallel: shared portal, shared rate limits)
// ---------------------------------------------------------------------------

// TestAccReal_portalDataSource reads the real portal's account info and
// asserts the reported portal ID matches HUBSPOT_TEST_PORTAL_ID — a
// read-only sanity check that the data source works against the live API.
func TestAccReal_portalDataSource(t *testing.T) {
	requireRealPortal(t)

	wantPortalID := os.Getenv("HUBSPOT_TEST_PORTAL_ID")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: realProviderConfig() + `data "hubspot_portal" "current" {}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.hubspot_portal.current",
						tfjsonpath.New("portal_id"), knownvalue.StringExact(wantPortalID)),
				},
			},
		},
	})
}

// TestAccReal_propertyGroupLifecycle runs the property-group lifecycle
// against the real portal: create with randomized name, perpetual-diff guard,
// import round-trip, automatic destroy + CheckDestroy.
func TestAccReal_propertyGroupLifecycle(t *testing.T) {
	requireRealPortal(t)

	name := randomRealName("grp_")
	label := "TF Acc Test Group"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkRealResourcesDestroyed,
		Steps: []resource.TestStep{
			{
				Config: realPropertyGroupConfig(name, label),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/"+name)),
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("name"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue("hubspot_property_group.test",
						tfjsonpath.New("label"), knownvalue.StringExact(label)),
				},
			},
			// Identical config: any real-API normalization the provider fails
			// to absorb shows up here as a perpetual diff.
			{
				Config: realPropertyGroupConfig(name, label),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      "hubspot_property_group.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "contacts/" + name,
			},
		},
	})
}

// TestAccReal_propertyLifecycle runs the enumeration-property lifecycle
// against the real portal: create (group + property in one config),
// perpetual-diff guard, in-place label update, import round-trip, destroy.
func TestAccReal_propertyLifecycle(t *testing.T) {
	requireRealPortal(t)

	groupName := randomRealName("grp_")
	propertyName := randomRealName("prop_")
	options := [][2]string{{"Bronze", "bronze"}, {"Silver", "silver"}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkRealResourcesDestroyed,
		Steps: []resource.TestStep{
			{
				Config: realEnumPropertyConfig(groupName, propertyName, "TF Acc Test Tier", options),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("id"), knownvalue.StringExact("contacts/"+propertyName)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("label"), knownvalue.StringExact("TF Acc Test Tier")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("group_name"), knownvalue.StringExact(groupName)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(0).AtMapKey("value"),
						knownvalue.StringExact("bronze")),
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("options").AtSliceIndex(1).AtMapKey("value"),
						knownvalue.StringExact("silver")),
				},
			},
			// Identical config must plan empty — the core reason this layer
			// exists: it catches real-API normalization (option displayOrder,
			// property displayOrder, description round-trip) the fake might
			// model imperfectly.
			{
				Config: realEnumPropertyConfig(groupName, propertyName, "TF Acc Test Tier", options),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// Label change is an in-place update.
			{
				Config: realEnumPropertyConfig(groupName, propertyName, "TF Acc Test Tier v2", options),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("hubspot_property.test",
							plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("hubspot_property.test",
						tfjsonpath.New("label"), knownvalue.StringExact("TF Acc Test Tier v2")),
				},
			},
			{
				ResourceName:      "hubspot_property.test",
				ImportState:       true,
				ImportStateVerify: true,
				// ImportStateVerifyIgnore intentionally empty: grow it only if
				// the real API demonstrably fails to round-trip an attribute.
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Sweepers: delete leaked tf_acc_test_* resources from the real test portal.
// Run via `make sweep` (go test -sweep=all). They apply the same portal
// guard as the tests and no-op with a log message when env is missing.
// ---------------------------------------------------------------------------

// sweeperEnv returns the access token when the env contract is satisfied and
// the token verifiably belongs to the test portal; ok=false means "no-op".
func sweeperEnv(name string) (token string, ok bool, err error) {
	token = os.Getenv("HUBSPOT_ACCESS_TOKEN")
	want := os.Getenv("HUBSPOT_TEST_PORTAL_ID")
	if token == "" || want == "" {
		log.Printf("[INFO] sweeper %s: HUBSPOT_ACCESS_TOKEN and/or HUBSPOT_TEST_PORTAL_ID not set; nothing to do", name)
		return "", false, nil
	}
	if err := verifyRealPortal(token, want); err != nil {
		return "", false, fmt.Errorf("sweeper %s: SAFETY GUARD: %w", name, err)
	}
	return token, true, nil
}

func sweepRealContactProperties(_ string) error {
	token, ok, err := sweeperEnv("hubspot_property")
	if err != nil || !ok {
		return err
	}
	names, err := realAPIListNames(token, "/crm/v3/properties/contacts")
	if err != nil {
		return fmt.Errorf("listing contacts properties: %w", err)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, realTestPrefix) {
			continue
		}
		log.Printf("[INFO] sweeper hubspot_property: archiving leaked contacts property %q", name)
		if err := realAPIDelete(token, "/crm/v3/properties/contacts/"+name); err != nil {
			return fmt.Errorf("sweeping property %s: %w", name, err)
		}
	}
	return nil
}

func sweepRealContactPropertyGroups(_ string) error {
	token, ok, err := sweeperEnv("hubspot_property_group")
	if err != nil || !ok {
		return err
	}
	names, err := realAPIListNames(token, "/crm/v3/properties/contacts/groups")
	if err != nil {
		return fmt.Errorf("listing contacts property groups: %w", err)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, realTestPrefix) {
			continue
		}
		log.Printf("[INFO] sweeper hubspot_property_group: deleting leaked contacts group %q", name)
		if err := realAPIDelete(token, "/crm/v3/properties/contacts/groups/"+name); err != nil {
			return fmt.Errorf("sweeping property group %s: %w", name, err)
		}
	}
	return nil
}
