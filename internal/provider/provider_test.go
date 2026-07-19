// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/revosai/terraform-provider-hubspot/internal/provider"
)

// testAccProtoV6ProviderFactories instantiates the provider in-process for
// acceptance tests (hermetic: pointed at the fake HubSpot via base_url).
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"hubspot": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// providerConfig renders a provider block pointing at the fake server.
func providerConfig(baseURL string) string {
	return fmt.Sprintf(`
provider "hubspot" {
  access_token = "pat-na1-00000000-0000-0000-0000-000000000000"
  base_url     = %q
}
`, baseURL)
}

// TestProviderSchemas validates the provider schema and every registered
// resource schema against the framework's implementation rules — the
// cheapest red/green gate for schema wiring bugs.
func TestProviderSchemas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	p, ok := provider.New("test")().(interface {
		Resources(context.Context) []func() fwresource.Resource
	})
	if !ok {
		t.Fatal("provider does not expose Resources")
	}

	for _, newResource := range p.Resources(ctx) {
		r := newResource()

		metaResp := &fwresource.MetadataResponse{}
		r.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "hubspot"}, metaResp)
		if metaResp.TypeName == "" {
			t.Errorf("resource %T has empty type name", r)
		}

		schemaResp := &fwresource.SchemaResponse{}
		r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Errorf("resource %s schema diagnostics: %v", metaResp.TypeName, schemaResp.Diagnostics)
			continue
		}
		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("resource %s schema validation: %v", metaResp.TypeName, diags)
		}
	}
}
