// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// filterBranchType is a string custom type holding a HubSpot list `filterBranch`
// as JSON. HubSpot expands a submitted filter tree with server-injected default
// keys on read-back (e.g. `filterBranchOperator`, `includeObjectsWithNoValueSet`),
// so a byte-for-byte comparison of the configured JSON against the returned JSON
// diffs forever. This type's semantic equality treats the configured tree as
// equal to the server tree when one is a structural subset of the other,
// suppressing those injected defaults while still surfacing real edits.
//
// Known v1 limitation (typed filter blocks are deferred — see the design doc):
// element order within `filters`/`filterBranches` arrays is significant, so a
// server that reordered array elements would show a false diff. HubSpot lists
// preserve submitted order in practice.
type filterBranchType struct {
	basetypes.StringType
}

var _ basetypes.StringTypable = filterBranchType{}

func (t filterBranchType) Equal(o attr.Type) bool {
	other, ok := o.(filterBranchType)
	if !ok {
		return false
	}
	return t.StringType.Equal(other.StringType)
}

func (t filterBranchType) String() string {
	return "filterBranchType"
}

func (t filterBranchType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return filterBranchValue{StringValue: in}, nil
}

func (t filterBranchType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	attrValue, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	sv, ok := attrValue.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T from StringType.ValueFromTerraform", attrValue)
	}
	return filterBranchValue{StringValue: sv}, nil
}

func (t filterBranchType) ValueType(_ context.Context) attr.Value {
	return filterBranchValue{}
}

// filterBranchValue is the value type for filterBranchType.
type filterBranchValue struct {
	basetypes.StringValue
}

var _ basetypes.StringValuableWithSemanticEquals = filterBranchValue{}

func (v filterBranchValue) Type(_ context.Context) attr.Type {
	return filterBranchType{}
}

func (v filterBranchValue) Equal(o attr.Value) bool {
	other, ok := o.(filterBranchValue)
	if !ok {
		return false
	}
	return v.StringValue.Equal(other.StringValue)
}

// StringSemanticEquals reports whether the two JSON filter trees are equivalent
// modulo HubSpot's server-injected defaults. Both values are known, non-null
// strings when the framework calls this. If either fails to parse as JSON, it
// returns false so the raw diff surfaces rather than being silently swallowed.
func (v filterBranchValue) StringSemanticEquals(_ context.Context, newValuable basetypes.StringValuable) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	newValue, ok := newValuable.(filterBranchValue)
	if !ok {
		diags.AddError(
			"Semantic Equality Check Error",
			fmt.Sprintf("expected value type filterBranchValue but got %T. This is a bug in the provider.", newValuable),
		)
		return false, diags
	}

	var a, b any
	if err := json.Unmarshal([]byte(v.ValueString()), &a); err != nil {
		return false, diags
	}
	if err := json.Unmarshal([]byte(newValue.ValueString()), &b); err != nil {
		return false, diags
	}
	return jsonSubset(a, b) || jsonSubset(b, a), diags
}

// jsonSubset reports whether x is a structural subset of y: every object key in
// x is present in y with a subset-equal value; arrays match by length and
// element-wise subset; scalars must be deeply equal. An empty object is NOT a
// subset of a populated object, so explicitly clearing a subtree to `{}` still
// registers as a change rather than being masked as a server default.
func jsonSubset(x, y any) bool {
	switch xv := x.(type) {
	case map[string]any:
		yv, ok := y.(map[string]any)
		if !ok {
			return false
		}
		if len(xv) == 0 {
			return len(yv) == 0
		}
		for k, xval := range xv {
			yval, present := yv[k]
			if !present || !jsonSubset(xval, yval) {
				return false
			}
		}
		return true
	case []any:
		yv, ok := y.([]any)
		if !ok || len(xv) != len(yv) {
			return false
		}
		for i := range xv {
			if !jsonSubset(xv[i], yv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(x, y)
	}
}
