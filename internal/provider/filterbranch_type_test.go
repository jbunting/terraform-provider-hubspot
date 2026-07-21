// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// TestFilterBranchSemanticEquals exercises the JSON subset-equality that
// suppresses HubSpot's server-injected filterBranch defaults while still
// catching genuine edits.
func TestFilterBranchSemanticEquals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{
			name:  "identical",
			a:     `{"filterBranchType":"OR","filters":[]}`,
			b:     `{"filterBranchType":"OR","filters":[]}`,
			equal: true,
		},
		{
			name:  "key order irrelevant",
			a:     `{"filterBranchType":"OR","filterBranchOperator":"OR"}`,
			b:     `{"filterBranchOperator":"OR","filterBranchType":"OR"}`,
			equal: true,
		},
		{
			name:  "server injects default keys",
			a:     `{"filters":[{"filterType":"PROPERTY","property":"email","operation":{"operator":"IS_KNOWN"}}]}`,
			b:     `{"filters":[{"filterType":"PROPERTY","property":"email","operation":{"operator":"IS_KNOWN","includeObjectsWithNoValueSet":false,"operationType":"ALL_PROPERTY"}}],"filterBranchOperator":"AND"}`,
			equal: true,
		},
		{
			name:  "changed scalar is a diff",
			a:     `{"filters":[{"property":"email","operation":{"operator":"IS_KNOWN"}}]}`,
			b:     `{"filters":[{"property":"email","operation":{"operator":"IS_NOT_KNOWN"}}]}`,
			equal: false,
		},
		{
			name:  "added array element is a diff",
			a:     `{"filters":[{"property":"email"}]}`,
			b:     `{"filters":[{"property":"email"},{"property":"firstname"}]}`,
			equal: false,
		},
		{
			name:  "clearing to empty object is a diff",
			a:     `{}`,
			b:     `{"filterBranchType":"OR","filters":[]}`,
			equal: false,
		},
		{
			name:  "invalid JSON falls back to not-equal",
			a:     `{not json`,
			b:     `{"filterBranchType":"OR"}`,
			equal: false,
		},
		{
			name:  "numbers compare structurally",
			a:     `{"filters":[{"operation":{"value":12}}]}`,
			b:     `{"filters":[{"operation":{"value":12,"operationType":"NUMBER"}}]}`,
			equal: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := filterBranchValue{StringValue: basetypes.NewStringValue(tc.a)}
			b := filterBranchValue{StringValue: basetypes.NewStringValue(tc.b)}
			got, diags := a.StringSemanticEquals(context.Background(), b)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.equal {
				t.Errorf("StringSemanticEquals(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.equal)
			}
			// Semantic equality must be symmetric.
			rev, _ := b.StringSemanticEquals(context.Background(), a)
			if rev != tc.equal {
				t.Errorf("reverse StringSemanticEquals(%s, %s) = %v, want %v", tc.b, tc.a, rev, tc.equal)
			}
		})
	}
}
