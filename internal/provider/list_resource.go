// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &listResource{}
	_ resource.ResourceWithConfigure   = &listResource{}
	_ resource.ResourceWithImportState = &listResource{}
)

// listResource implements hubspot_list: a CRM list (MANUAL/DYNAMIC/SNAPSHOT)
// managed via the Lists v3 API (/crm/v3/lists). Dynamic-list membership is
// never tracked — only the filter definition.
type listResource struct {
	client *client.Client
}

// NewListResource returns the hubspot_list resource.
func NewListResource() resource.Resource {
	return &listResource{}
}

type listResourceModel struct {
	ID             types.String      `tfsdk:"id"`
	ListID         types.String      `tfsdk:"list_id"`
	Name           types.String      `tfsdk:"name"`
	ObjectTypeID   types.String      `tfsdk:"object_type_id"`
	ProcessingType types.String      `tfsdk:"processing_type"`
	FilterBranch   filterBranchValue `tfsdk:"filter_branch"`
}

type listWire struct {
	ListID         string          `json:"listId,omitempty"`
	Name           string          `json:"name,omitempty"`
	ObjectTypeID   string          `json:"objectTypeId,omitempty"`
	ProcessingType string          `json:"processingType,omitempty"`
	FilterBranch   json.RawMessage `json:"filterBranch,omitempty"`
}

type listEnvelope struct {
	List listWire `json:"list"`
}

type listCreateWire struct {
	Name           string          `json:"name"`
	ObjectTypeID   string          `json:"objectTypeId"`
	ProcessingType string          `json:"processingType"`
	FilterBranch   json.RawMessage `json:"filterBranch,omitempty"`
}

func (r *listResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_list"
}

func (r *listResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CRM list (`/crm/v3/lists`). Requires the `crm.lists.write` scope. " +
			"A list is `DYNAMIC` (membership computed from a filter tree), `MANUAL` (static membership), or " +
			"`SNAPSHOT` (one-time filter evaluation).\n\n" +
			"**Only the list definition is managed, never its membership.** Dynamic membership is computed by " +
			"HubSpot and is deliberately not tracked (it would churn state on every record change).\n\n" +
			"**`filter_branch` is passed through as JSON.** HubSpot expands the submitted tree with " +
			"server-injected defaults (`filterBranchOperator`, `includeObjectsWithNoValueSet`, …); the provider " +
			"compares it **semantically**, so an unchanged configuration plans empty. For `DYNAMIC` lists, " +
			"editing the tree is an in-place update. For `SNAPSHOT` lists the filter is evaluated once at " +
			"creation, so changing `filter_branch` **forces replacement**. `MANUAL` lists omit `filter_branch`.\n\n" +
			"Changing `object_type_id` or `processing_type` forces replacement. Deleting a list archives it " +
			"(restorable within 90 days).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The list's ILS ID (identical to `list_id`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"list_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned ILS list ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable list name. Mutable (in-place `update-list-name`).",
			},
			"object_type_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Object type the list is built on (e.g. `0-1` for contacts, `0-2` for " +
					"companies). Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"processing_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "List processing type: `DYNAMIC`, `MANUAL`, or `SNAPSHOT`. Changing this " +
					"forces replacement.",
				Validators: []validator.String{
					stringvalidator.OneOf("DYNAMIC", "MANUAL", "SNAPSHOT"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"filter_branch": schema.StringAttribute{
				CustomType: filterBranchType{},
				Optional:   true,
				MarkdownDescription: "The filter tree as JSON (use `jsonencode(...)`). Required for `DYNAMIC` " +
					"and `SNAPSHOT` lists; omit for `MANUAL`. Compared semantically to absorb HubSpot's " +
					"server-injected defaults. For `SNAPSHOT` lists, changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					// SNAPSHOT filters are evaluated once at creation; a change
					// cannot be applied in place, so force replacement. DYNAMIC
					// filter edits update in place (handled in Update).
					stringplanmodifier.RequiresReplaceIf(
						r.snapshotFilterRequiresReplace,
						"Changing filter_branch on a SNAPSHOT list forces replacement.",
						"Changing `filter_branch` on a `SNAPSHOT` list forces replacement.",
					),
				},
			},
		},
	}
}

// snapshotFilterRequiresReplace forces replacement when filter_branch changes on
// a SNAPSHOT list. It uses semantic equality so server-injected defaults don't
// trigger a spurious replace.
func (r *listResource) snapshotFilterRequiresReplace(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	var processingType types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("processing_type"), &processingType)...)
	if resp.Diagnostics.HasError() || processingType.ValueString() != "SNAPSHOT" {
		return
	}
	// Both null → no change. One null → change.
	if req.StateValue.IsNull() != req.ConfigValue.IsNull() {
		resp.RequiresReplace = true
		return
	}
	if req.StateValue.IsNull() {
		return
	}
	stateV := filterBranchValue{StringValue: req.StateValue}
	configV := filterBranchValue{StringValue: req.ConfigValue}
	equal, diags := stateV.StringSemanticEquals(ctx, configV)
	resp.Diagnostics.Append(diags...)
	if !equal {
		resp.RequiresReplace = true
	}
}

func (r *listResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *listResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := listCreateWire{
		Name:           plan.Name.ValueString(),
		ObjectTypeID:   plan.ObjectTypeID.ValueString(),
		ProcessingType: plan.ProcessingType.ValueString(),
	}
	if !plan.FilterBranch.IsNull() && !plan.FilterBranch.IsUnknown() {
		body.FilterBranch = json.RawMessage(plan.FilterBranch.ValueString())
	}

	var out listEnvelope
	if err := r.client.Post(ctx, "crm/v3/lists", body, &out); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create HubSpot list",
			fmt.Sprintf("POST /crm/v3/lists failed: %s", err),
		)
		return
	}

	// The create response does not echo filterBranch, so the configured value is
	// kept in state; a later read reconciles it semantically against the server.
	plan.ListID = types.StringValue(out.List.ListID)
	plan.ID = types.StringValue(out.List.ListID)
	plan.Name = types.StringValue(out.List.Name)
	plan.ObjectTypeID = types.StringValue(out.List.ObjectTypeID)
	plan.ProcessingType = types.StringValue(out.List.ProcessingType)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := url.Values{}
	q.Set("includeFilters", "true")
	p := "crm/v3/lists/" + url.PathEscape(state.ListID.ValueString())
	var out listEnvelope
	if err := r.client.Get(ctx, p, q, &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read HubSpot list",
			fmt.Sprintf("GET %s failed: %s", p, err),
		)
		return
	}

	state.ListID = types.StringValue(out.List.ListID)
	state.ID = types.StringValue(out.List.ListID)
	state.Name = types.StringValue(out.List.Name)
	state.ObjectTypeID = types.StringValue(out.List.ObjectTypeID)
	state.ProcessingType = types.StringValue(out.List.ProcessingType)
	if len(out.List.FilterBranch) > 0 {
		// Semantic equality keeps the prior configured value if equivalent.
		state.FilterBranch = filterBranchValue{StringValue: types.StringValue(string(out.List.FilterBranch))}
	} else {
		state.FilterBranch = filterBranchValue{StringValue: types.StringNull()}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	listID := state.ListID.ValueString()

	if plan.Name.ValueString() != state.Name.ValueString() {
		q := url.Values{}
		q.Set("listName", plan.Name.ValueString())
		p := "crm/v3/lists/" + url.PathEscape(listID) + "/update-list-name?" + q.Encode()
		if err := r.client.Put(ctx, p, nil, nil); err != nil {
			resp.Diagnostics.AddError(
				"Unable to update HubSpot list name",
				fmt.Sprintf("PUT %s failed: %s", p, err),
			)
			return
		}
	}

	// Filter edits are in-place only for DYNAMIC lists; SNAPSHOT changes force
	// replacement (see the plan modifier) and never reach Update.
	if r.filterChanged(ctx, plan.FilterBranch, state.FilterBranch, &resp.Diagnostics) {
		if resp.Diagnostics.HasError() {
			return
		}
		body := map[string]json.RawMessage{"filterBranch": json.RawMessage(plan.FilterBranch.ValueString())}
		p := "crm/v3/lists/" + url.PathEscape(listID) + "/update-list-filters"
		if err := r.client.Put(ctx, p, body, nil); err != nil {
			resp.Diagnostics.AddError(
				"Unable to update HubSpot list filters",
				fmt.Sprintf("PUT %s failed: %s", p, err),
			)
			return
		}
	}

	plan.ListID = state.ListID
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// filterChanged reports whether the plan's filter_branch differs semantically
// from state's, treating null↔non-null as a change.
func (r *listResource) filterChanged(ctx context.Context, plan, state filterBranchValue, diags *diag.Diagnostics) bool {
	if plan.IsNull() != state.IsNull() {
		return true
	}
	if plan.IsNull() {
		return false
	}
	equal, d := state.StringSemanticEquals(ctx, plan)
	diags.Append(d...)
	return !equal
}

func (r *listResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state listResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := "crm/v3/lists/" + url.PathEscape(state.ListID.ValueString())
	if err := r.client.Delete(ctx, p, nil); err != nil {
		if client.IsNotFound(err) {
			return // Already gone; deletion is idempotent.
		}
		resp.Diagnostics.AddError(
			"Unable to delete HubSpot list",
			fmt.Sprintf("DELETE %s failed: %s", p, err),
		)
	}
}

func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("list_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
