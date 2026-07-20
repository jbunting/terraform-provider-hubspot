// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// keepStateList is a list plan modifier for create-time bootstrap attributes:
// once the resource exists, the prior state value is always used regardless of
// configuration, so post-create edits are silently ignored (no diff, no
// update). It intentionally differs from UseStateForUnknown, which only applies
// when the planned value is unknown. Document any attribute using this as
// create-time-only so the ignored-edit behavior is not surprising.
type keepStateList struct{}

func (keepStateList) Description(_ context.Context) string {
	return "Uses the prior state value after creation; configuration edits are ignored."
}

func (m keepStateList) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepStateList) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	// On create there is no prior state to keep.
	if req.State.Raw.IsNull() {
		return
	}
	// On destroy the plan is null; nothing to hold.
	if req.Plan.Raw.IsNull() {
		return
	}
	if req.StateValue.IsNull() {
		return
	}
	resp.PlanValue = req.StateValue
}

// keepStateSet is the set counterpart of keepStateList.
type keepStateSet struct{}

func (keepStateSet) Description(_ context.Context) string {
	return "Uses the prior state value after creation; configuration edits are ignored."
}

func (m keepStateSet) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepStateSet) PlanModifySet(_ context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.State.Raw.IsNull() {
		return
	}
	if req.Plan.Raw.IsNull() {
		return
	}
	if req.StateValue.IsNull() {
		return
	}
	resp.PlanValue = req.StateValue
}
