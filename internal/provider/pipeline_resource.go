// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &pipelineResource{}
	_ resource.ResourceWithConfigure   = &pipelineResource{}
	_ resource.ResourceWithImportState = &pipelineResource{}
	_ resource.ResourceWithModifyPlan  = &pipelineResource{}
)

// pipelineResource implements the hubspot_pipeline resource: a CRM pipeline
// with inline stages, managed via /crm/v3/pipelines/{objectType}.
type pipelineResource struct {
	client *client.Client
}

// NewPipelineResource returns the hubspot_pipeline resource.
func NewPipelineResource() resource.Resource {
	return &pipelineResource{}
}

// pipelineResourceModel maps the hubspot_pipeline Terraform schema.
type pipelineResourceModel struct {
	ID           types.String `tfsdk:"id"`
	ObjectType   types.String `tfsdk:"object_type"`
	PipelineID   types.String `tfsdk:"pipeline_id"`
	Label        types.String `tfsdk:"label"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
	Stages       types.List   `tfsdk:"stages"`
}

// pipelineStageModel maps one element of the stages list.
type pipelineStageModel struct {
	Label        types.String `tfsdk:"label"`
	StageID      types.String `tfsdk:"stage_id"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
	Metadata     types.Map    `tfsdk:"metadata"`
	ID           types.String `tfsdk:"id"`
}

// pipelineStageAttrTypes is the object type of one stages element.
var pipelineStageAttrTypes = map[string]attr.Type{
	"label":         types.StringType,
	"stage_id":      types.StringType,
	"display_order": types.Int64Type,
	"metadata":      types.MapType{ElemType: types.StringType},
	"id":            types.StringType,
}

var pipelineStageObjectType = types.ObjectType{AttrTypes: pipelineStageAttrTypes}

// pipelineWire is the wire shape of a pipeline for both requests and responses
// of the /crm/v3/pipelines endpoints.
type pipelineWire struct {
	ID           string              `json:"id,omitempty"`
	Label        string              `json:"label,omitempty"`
	DisplayOrder *int64              `json:"displayOrder,omitempty"`
	Stages       []pipelineStageWire `json:"stages"`
}

// pipelineStageWire is the wire shape of one stage. stageId is only sent
// (client-pinned or carried-forward id); responses populate id.
type pipelineStageWire struct {
	ID           string            `json:"id,omitempty"`
	StageID      string            `json:"stageId,omitempty"`
	Label        string            `json:"label,omitempty"`
	DisplayOrder *int64            `json:"displayOrder,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func (r *pipelineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (r *pipelineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CRM pipeline with inline stages (`/crm/v3/pipelines/{objectType}`). " +
			"Requires the `crm.pipelines.write` scope (plus the relevant object write scope, e.g. " +
			"`crm.objects.deals.write` or `crm.objects.tickets.write`).\n\n" +
			"**Whole-pipeline replace on update**: HubSpot updates a pipeline with a full `PUT`, so the " +
			"configured stage set is authoritative — a stage omitted from `stages` is deleted. The provider " +
			"sends `validateReferencesBeforeDelete=true` and `validateDealStageUsagesBeforeDelete=true` so " +
			"HubSpot rejects deleting a stage that still has records in it, rather than orphaning them.\n\n" +
			"**Deal probability is a string** (`\"0.2\"`, `\"1.0\"`) to avoid perpetual float round-trip diffs. " +
			"HubSpot's built-in **default** pipeline cannot be deleted — adopt it with `terraform import` " +
			"rather than declaring a new one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the form `{object_type}/{pipeline_id}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"object_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "CRM object type the pipeline belongs to: `deals`, `tickets`, or a custom " +
					"object type ID (e.g. `2-12345`). Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"pipeline_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned pipeline ID (unique within the object type).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"label": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable pipeline label shown in the HubSpot UI. Mutable.",
			},
			"display_order": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Position of the pipeline relative to other pipelines (lower sorts first).",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"stages": schema.ListNestedAttribute{
				Required: true,
				MarkdownDescription: "Ordered stages of the pipeline (at least one). Stages are matched " +
					"across plan and state by `stage_id`, never by list position, so renaming or reordering " +
					"stages is an in-place update rather than a delete-and-recreate.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"label": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Human-readable stage label shown in the HubSpot UI.",
						},
						"stage_id": schema.StringAttribute{
							Optional: true,
							Computed: true,
							MarkdownDescription: "Stable internal stage ID. Leave unset to let HubSpot assign one " +
								"(stored back into state); pin it to keep a stage's identity across relabels.",
						},
						"display_order": schema.Int64Attribute{
							Required:            true,
							MarkdownDescription: "Position of the stage within the pipeline (lower sorts first).",
						},
						"metadata": schema.MapAttribute{
							Optional:    true,
							Computed:    true,
							ElementType: types.StringType,
							MarkdownDescription: "Stage metadata as string values. Deal stages require " +
								"`probability` (e.g. `\"0.2\"`, `\"1.0\"`); ticket stages use `ticketState` " +
								"(`\"OPEN\"` or `\"CLOSED\"`). Values are strings to avoid float diffs. " +
								"Only the keys you set are tracked; HubSpot-injected keys (such as " +
								"`isClosed` on deal stages) are ignored, so they never cause a perpetual " +
								"diff. An omitted `metadata` and `metadata = {}` are both valid and " +
								"round-trip without drift.",
						},
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Server stage ID (equals `stage_id`).",
						},
					},
				},
			},
		},
	}
}

func (r *pipelineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan carries server-assigned stage identifiers (stage_id, id) and
// computed metadata forward from prior state into the plan, matching config
// stages to prior-state stages by stable key rather than by list position.
// This is what makes an identical config plan empty and a reorder/relabel an
// in-place update instead of a delete-and-recreate.
func (r *pipelineResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create or destroy: nothing to carry.
	}

	var plan, state pipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A changed object_type forces replacement; carrying ids would be wrong.
	if plan.ObjectType.ValueString() != state.ObjectType.ValueString() {
		return
	}
	if plan.Stages.IsNull() || plan.Stages.IsUnknown() {
		return
	}

	var planStages, stateStages []pipelineStageModel
	resp.Diagnostics.Append(plan.Stages.ElementsAs(ctx, &planStages, false)...)
	resp.Diagnostics.Append(state.Stages.ElementsAs(ctx, &stateStages, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	matched := matchStages(planStages, stateStages)
	for i := range planStages {
		m := matched[i]
		if m == nil {
			continue // newly added stage: let the server assign ids.
		}
		if planStages[i].StageID.IsUnknown() {
			planStages[i].StageID = m.StageID
		}
		if planStages[i].ID.IsUnknown() {
			planStages[i].ID = m.ID
		}
		if planStages[i].Metadata.IsUnknown() {
			planStages[i].Metadata = m.Metadata
		}
		if planStages[i].DisplayOrder.IsUnknown() {
			planStages[i].DisplayOrder = m.DisplayOrder
		}
	}

	stages, diags := types.ListValueFrom(ctx, pipelineStageObjectType, planStages)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("stages"), stages)...)
}

func (r *pipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := expandPipeline(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := pipelinesPath(plan.ObjectType.ValueString())
	var out pipelineWire
	if err := r.client.Post(ctx, p, body, &out); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create HubSpot pipeline",
			fmt.Sprintf("POST %s failed: %s", p, err),
		)
		return
	}

	resp.Diagnostics.Append(flattenPipeline(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := pipelinePath(state.ObjectType.ValueString(), state.PipelineID.ValueString())
	var out pipelineWire
	if err := r.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read HubSpot pipeline",
			fmt.Sprintf("GET %s failed: %s", p, err),
		)
		return
	}

	resp.Diagnostics.Append(flattenPipeline(ctx, out, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *pipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The plan already carries stage ids forward (see ModifyPlan), so expand
	// sends the stable identifier for stages that already exist. HubSpot does a
	// whole-pipeline PUT: the desired stage set is authoritative.
	body, diags := expandPipeline(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := url.Values{}
	q.Set("validateReferencesBeforeDelete", "true")
	q.Set("validateDealStageUsagesBeforeDelete", "true")
	p := pipelinePath(plan.ObjectType.ValueString(), plan.PipelineID.ValueString()) + "?" + q.Encode()

	var out pipelineWire
	if err := r.client.Put(ctx, p, body, &out); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update HubSpot pipeline",
			fmt.Sprintf("PUT %s failed: %s\n\nIf HubSpot rejected the update because a stage still has "+
				"records in it, either keep that stage in configuration or move its records to another "+
				"stage before removing it.", p, err),
		)
		return
	}

	resp.Diagnostics.Append(flattenPipeline(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := pipelinePath(state.ObjectType.ValueString(), state.PipelineID.ValueString())
	if err := r.client.Delete(ctx, p, nil); err != nil {
		if client.IsNotFound(err) {
			return // Already gone; deletion is idempotent.
		}
		var apiErr *client.APIError
		if client.AsAPIError(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Message), "default") {
			resp.Diagnostics.AddError(
				"HubSpot refused to delete the default pipeline",
				fmt.Sprintf("Pipeline %q on %q cannot be deleted because HubSpot forbids deleting the default "+
					"pipeline. Remove it from configuration and run `terraform state rm` on this resource to "+
					"stop managing it, or keep it in configuration.\n\nAPI error: %s",
					state.PipelineID.ValueString(), state.ObjectType.ValueString(), err),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to delete HubSpot pipeline",
			fmt.Sprintf("DELETE %s failed: %s", p, err),
		)
	}
}

func (r *pipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid hubspot_pipeline import ID",
			fmt.Sprintf("Expected an import ID of the form \"{object_type}/{pipeline_id}\" with exactly one "+
				"slash, e.g. \"deals/default\", got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("pipeline_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// pipelinesPath is the collection path for objectType.
func pipelinesPath(objectType string) string {
	return "crm/v3/pipelines/" + url.PathEscape(objectType)
}

// pipelinePath is the item path for one pipeline.
func pipelinePath(objectType, pipelineID string) string {
	return pipelinesPath(objectType) + "/" + url.PathEscape(pipelineID)
}

// matchStages returns, for each plan stage, the prior-state stage it
// corresponds to (or nil for a newly added stage). It matches first by pinned
// stage_id, then by label, then by leftover position — so a relabel (position
// stable) and a reorder (label stable) both preserve stage identity.
func matchStages(plan, state []pipelineStageModel) []*pipelineStageModel {
	matched := make([]*pipelineStageModel, len(plan))
	used := make([]bool, len(state))

	pinnedID := func(m pipelineStageModel) string {
		if m.StageID.IsNull() || m.StageID.IsUnknown() {
			return ""
		}
		return m.StageID.ValueString()
	}

	// Pass 1: explicit stage_id match.
	for i := range plan {
		id := pinnedID(plan[i])
		if id == "" {
			continue
		}
		for j := range state {
			if !used[j] && pinnedID(state[j]) == id {
				matched[i] = &state[j]
				used[j] = true
				break
			}
		}
	}
	// Pass 2: label match.
	for i := range plan {
		if matched[i] != nil {
			continue
		}
		for j := range state {
			if !used[j] && state[j].Label.ValueString() == plan[i].Label.ValueString() {
				matched[i] = &state[j]
				used[j] = true
				break
			}
		}
	}
	// Pass 3: leftover position match.
	for i := range plan {
		if matched[i] != nil {
			continue
		}
		for j := range state {
			if !used[j] {
				matched[i] = &state[j]
				used[j] = true
				break
			}
		}
	}
	return matched
}

// expandPipeline converts the Terraform model into the API wire shape. Stages
// carry a stageId when one is known (pinned in config or carried forward from
// state via ModifyPlan) so the server keeps existing stages instead of
// recreating them.
func expandPipeline(ctx context.Context, m pipelineResourceModel) (pipelineWire, diag.Diagnostics) {
	var diags diag.Diagnostics

	body := pipelineWire{
		Label:  m.Label.ValueString(),
		Stages: []pipelineStageWire{},
	}
	if !m.DisplayOrder.IsNull() && !m.DisplayOrder.IsUnknown() {
		v := m.DisplayOrder.ValueInt64()
		body.DisplayOrder = &v
	}

	var stages []pipelineStageModel
	diags.Append(m.Stages.ElementsAs(ctx, &stages, false)...)
	if diags.HasError() {
		return body, diags
	}

	for _, s := range stages {
		order := s.DisplayOrder.ValueInt64()
		wire := pipelineStageWire{
			Label:        s.Label.ValueString(),
			DisplayOrder: &order,
		}
		if !s.StageID.IsNull() && !s.StageID.IsUnknown() && s.StageID.ValueString() != "" {
			wire.StageID = s.StageID.ValueString()
		} else if !s.ID.IsNull() && !s.ID.IsUnknown() && s.ID.ValueString() != "" {
			wire.StageID = s.ID.ValueString()
		}
		if !s.Metadata.IsNull() && !s.Metadata.IsUnknown() {
			md := map[string]string{}
			diags.Append(s.Metadata.ElementsAs(ctx, &md, false)...)
			if diags.HasError() {
				return body, diags
			}
			wire.Metadata = md
		}
		body.Stages = append(body.Stages, wire)
	}

	return body, diags
}

// flattenPipeline writes the API response over the model in place. The model
// must already carry object_type. Stages are sorted by the server-assigned
// displayOrder so a stable order is presented; stage_id and id both adopt the
// server stage id.
//
// Metadata is reconciled semantically (design decision #9): HubSpot injects
// server-managed keys the client never sent (e.g. `isClosed` on deal stages),
// so tracking the raw server map would perpetually diff against config. For
// each stage we keep ONLY the keys the user manages — the keys present in the
// corresponding stage's metadata in the prior model (the plan for
// Create/Update, the state for Read) — while taking their VALUES from the
// server response so server normalization of managed keys is still captured.
// Server-injected keys are dropped. When there is no prior model (Import),
// the full server map is stored best-effort; a first plan after import may
// reconcile metadata against the user's config.
func flattenPipeline(ctx context.Context, api pipelineWire, m *pipelineResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Capture the prior stages before we overwrite m.Stages, so we can learn
	// which metadata keys the user manages.
	var priorStages []pipelineStageModel
	if !m.Stages.IsNull() && !m.Stages.IsUnknown() {
		diags.Append(m.Stages.ElementsAs(ctx, &priorStages, false)...)
		if diags.HasError() {
			return diags
		}
	}
	hasPrior := len(priorStages) > 0

	m.PipelineID = types.StringValue(api.ID)
	m.ID = types.StringValue(m.ObjectType.ValueString() + "/" + api.ID)
	m.Label = types.StringValue(api.Label)
	if api.DisplayOrder != nil {
		m.DisplayOrder = types.Int64Value(*api.DisplayOrder)
	} else {
		m.DisplayOrder = types.Int64Value(0)
	}

	sorted := make([]pipelineStageWire, len(api.Stages))
	copy(sorted, api.Stages)
	sort.SliceStable(sorted, func(i, j int) bool {
		oi, oj := int64(0), int64(0)
		if sorted[i].DisplayOrder != nil {
			oi = *sorted[i].DisplayOrder
		}
		if sorted[j].DisplayOrder != nil {
			oj = *sorted[j].DisplayOrder
		}
		return oi < oj
	})

	// Match each server stage to its prior-model stage (by the same stable key
	// matching used elsewhere) so we can scope metadata to managed keys.
	var matched []*pipelineStageModel
	if hasPrior {
		serverModels := make([]pipelineStageModel, len(sorted))
		for i, s := range sorted {
			serverModels[i] = pipelineStageModel{
				Label:   types.StringValue(s.Label),
				StageID: types.StringValue(s.ID),
			}
		}
		matched = matchStages(serverModels, priorStages)
	}

	elems := make([]attr.Value, 0, len(sorted))
	for i, s := range sorted {
		order := int64(0)
		if s.DisplayOrder != nil {
			order = *s.DisplayOrder
		}

		var prior *pipelineStageModel
		if hasPrior {
			prior = matched[i]
		}
		metadata, mdDiags := reconcileStageMetadata(ctx, hasPrior, prior, s.Metadata)
		diags.Append(mdDiags...)

		obj, objDiags := types.ObjectValue(pipelineStageAttrTypes, map[string]attr.Value{
			"label":         types.StringValue(s.Label),
			"stage_id":      types.StringValue(s.ID),
			"display_order": types.Int64Value(order),
			"metadata":      metadata,
			"id":            types.StringValue(s.ID),
		})
		diags.Append(objDiags...)
		elems = append(elems, obj)
	}
	list, listDiags := types.ListValue(pipelineStageObjectType, elems)
	diags.Append(listDiags...)
	m.Stages = list

	return diags
}

// reconcileStageMetadata returns the metadata value to store for one stage,
// dropping server-injected keys. When there is no prior model at all (import),
// the full server map is returned best-effort. Otherwise only the keys the
// user manages in the prior stage are kept (values sourced from the server),
// preserving the null-vs-empty distinction so `metadata = {}` round-trips.
func reconcileStageMetadata(ctx context.Context, hasPrior bool, prior *pipelineStageModel, serverMeta map[string]string) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Import (no prior stages): store the server map best-effort.
	if !hasPrior {
		if len(serverMeta) == 0 {
			return types.MapNull(types.StringType), diags
		}
		return types.MapValueFrom(ctx, types.StringType, serverMeta)
	}

	// A stage the server returned but that did not match any prior stage: keep
	// the full server map so no user-visible data is silently dropped.
	if prior == nil {
		if len(serverMeta) == 0 {
			return types.MapNull(types.StringType), diags
		}
		return types.MapValueFrom(ctx, types.StringType, serverMeta)
	}

	// A null prior means the user manages no metadata keys on this stage; keep
	// it null so config-null and state-null agree.
	if prior.Metadata.IsNull() || prior.Metadata.IsUnknown() {
		return types.MapNull(types.StringType), diags
	}

	var priorMeta map[string]string
	diags.Append(prior.Metadata.ElementsAs(ctx, &priorMeta, false)...)
	if diags.HasError() {
		return types.MapNull(types.StringType), diags
	}

	// Keep only managed keys, taking values from the server response.
	managed := make(map[string]string, len(priorMeta))
	for k := range priorMeta {
		if v, ok := serverMeta[k]; ok {
			managed[k] = v
		} else {
			managed[k] = priorMeta[k]
		}
	}
	return types.MapValueFrom(ctx, types.StringType, managed)
}
