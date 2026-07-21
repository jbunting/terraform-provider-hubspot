// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &associationLabelResource{}
	_ resource.ResourceWithConfigure   = &associationLabelResource{}
	_ resource.ResourceWithImportState = &associationLabelResource{}
)

// associationLabelResource implements hubspot_association_label: a USER_DEFINED
// association label definition between two object types, managed via the
// Associations v4 schema endpoints (/crm/v4/associations/{from}/{to}/labels).
type associationLabelResource struct {
	client *client.Client
}

// NewAssociationLabelResource returns the hubspot_association_label resource.
func NewAssociationLabelResource() resource.Resource {
	return &associationLabelResource{}
}

// associationLabelResourceModel maps the hubspot_association_label schema.
type associationLabelResourceModel struct {
	ID             types.String `tfsdk:"id"`
	FromObjectType types.String `tfsdk:"from_object_type"`
	ToObjectType   types.String `tfsdk:"to_object_type"`
	Name           types.String `tfsdk:"name"`
	Label          types.String `tfsdk:"label"`
	InverseLabel   types.String `tfsdk:"inverse_label"`
	TypeID         types.String `tfsdk:"type_id"`
	InverseTypeID  types.String `tfsdk:"inverse_type_id"`
	Category       types.String `tfsdk:"category"`
}

// assocLabelWire is one label object in an Associations v4 response. HubSpot
// returns only category, typeId and label — never the `name` sent on create.
type assocLabelWire struct {
	Category string `json:"category,omitempty"`
	TypeID   int64  `json:"typeId,omitempty"`
	Label    string `json:"label,omitempty"`
}

// assocLabelListWire wraps a label list (GET and POST responses).
type assocLabelListWire struct {
	Results []assocLabelWire `json:"results"`
}

// assocLabelCreateWire is the POST create body.
type assocLabelCreateWire struct {
	Label        string `json:"label"`
	Name         string `json:"name,omitempty"`
	InverseLabel string `json:"inverseLabel,omitempty"`
}

// assocLabelUpdateWire is the PUT update body.
type assocLabelUpdateWire struct {
	AssociationTypeID int64  `json:"associationTypeId"`
	Label             string `json:"label"`
	InverseLabel      string `json:"inverseLabel,omitempty"`
}

func (r *associationLabelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_association_label"
}

func (r *associationLabelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a custom association **label** between two CRM object types " +
			"(`/crm/v4/associations/{fromObjectType}/{toObjectType}/labels`). Requires the object read/write " +
			"scopes of both sides; custom labels require a **Professional or Enterprise** subscription.\n\n" +
			"**`name` is write-only and immutable.** HubSpot uses `name` only at creation and never returns it, " +
			"so the provider cannot read it back: changing `name` forces replacement, and `name` is **not " +
			"populated on import** (add it to configuration afterwards). `label` (and `inverse_label`) hold the " +
			"human-readable UI text and are updated in place.\n\n" +
			"**Paired vs unpaired.** Setting `inverse_label` makes the label *paired*: HubSpot mints two " +
			"association type IDs, one per direction (`type_id` and `inverse_type_id`). Adding or removing " +
			"`inverse_label` later **forces replacement**, because HubSpot cannot convert a label's paired-ness " +
			"in place.\n\n" +
			"**Deleting a label removes it from every record association that uses it** — a potentially wide " +
			"blast radius. HubSpot-defined labels (e.g. `Primary`) are not manageable and cannot be imported.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the form `{from_object_type}/{to_object_type}/{type_id}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"from_object_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Source CRM object type (e.g. `contacts`, `companies`, or a custom object " +
					"type ID like `2-12345`). Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"to_object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Target CRM object type. Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Immutable internal identifier used only when creating the label; HubSpot " +
					"never returns it. Changing it forces replacement, and it is not populated on import.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable label text shown in the HubSpot UI. Mutable.",
			},
			"inverse_label": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Human-readable text for the reverse direction. Setting this makes the label " +
					"paired (two `type_id`s). Adding or removing it forces replacement; editing the text is an " +
					"in-place update.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
							// Flipping between paired and unpaired cannot be done in
							// place; only text edits (both non-null) update in place.
							resp.RequiresReplace = req.StateValue.IsNull() != req.ConfigValue.IsNull()
						},
						"Adding or removing inverse_label forces replacement.",
						"Adding or removing `inverse_label` forces replacement.",
					),
				},
			},
			"type_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned association type ID for the forward direction (portal-specific).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"inverse_type_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Server-assigned association type ID for the reverse direction; null for an " +
					"unpaired label.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"category": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Association category. Managed labels are always `USER_DEFINED`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *associationLabelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *associationLabelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan associationLabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := assocLabelCreateWire{
		Label: plan.Label.ValueString(),
		Name:  plan.Name.ValueString(),
	}
	paired := !plan.InverseLabel.IsNull() && !plan.InverseLabel.IsUnknown()
	if paired {
		body.InverseLabel = plan.InverseLabel.ValueString()
	}

	from, to := plan.FromObjectType.ValueString(), plan.ToObjectType.ValueString()
	p := assocLabelsPath(from, to)
	var out assocLabelListWire
	if err := r.client.Post(ctx, p, body, &out); err != nil {
		resp.Diagnostics.AddError(
			"Unable to create HubSpot association label",
			fmt.Sprintf("POST %s failed: %s", p, err),
		)
		return
	}
	if len(out.Results) == 0 {
		resp.Diagnostics.AddError(
			"Unexpected HubSpot response creating association label",
			fmt.Sprintf("POST %s returned no label objects.", p),
		)
		return
	}
	if paired && len(out.Results) < 2 {
		resp.Diagnostics.AddError(
			"Unexpected HubSpot response creating paired association label",
			fmt.Sprintf("POST %s returned %d label object(s); expected 2 for a paired label.", p, len(out.Results)),
		)
		return
	}

	fwd := out.Results[0]
	plan.TypeID = types.StringValue(strconv.FormatInt(fwd.TypeID, 10))
	plan.Label = types.StringValue(fwd.Label)
	plan.Category = types.StringValue(fwd.Category)
	if paired {
		inv := out.Results[1]
		plan.InverseTypeID = types.StringValue(strconv.FormatInt(inv.TypeID, 10))
		plan.InverseLabel = types.StringValue(inv.Label)
	} else {
		plan.InverseTypeID = types.StringNull()
	}
	plan.ID = types.StringValue(labelID(from, to, plan.TypeID.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *associationLabelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state associationLabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	from, to := state.FromObjectType.ValueString(), state.ToObjectType.ValueString()
	fwd, found, diags := r.findLabel(ctx, from, to, state.TypeID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	// HubSpot-defined labels are not manageable (this also gates import, which
	// runs Read immediately after setting the composite id).
	if fwd.Category == "HUBSPOT_DEFINED" {
		resp.Diagnostics.AddError(
			"Association label is HubSpot-defined",
			fmt.Sprintf("The association label type_id %s between %q and %q is HubSpot-defined and cannot be "+
				"managed by Terraform. Remove it from configuration (and `terraform state rm` it if imported).",
				state.TypeID.ValueString(), from, to),
		)
		return
	}

	state.Label = types.StringValue(fwd.Label)
	state.Category = types.StringValue(fwd.Category)
	state.ID = types.StringValue(labelID(from, to, state.TypeID.ValueString()))

	// For a paired label the reverse text lives on the reverse pair's own label
	// entry, so it takes a second lookup.
	if !state.InverseTypeID.IsNull() && !state.InverseTypeID.IsUnknown() {
		inv, invFound, invDiags := r.findLabel(ctx, to, from, state.InverseTypeID.ValueString())
		resp.Diagnostics.Append(invDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if invFound {
			state.InverseLabel = types.StringValue(inv.Label)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *associationLabelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state associationLabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typeID, err := strconv.ParseInt(state.TypeID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid association label type_id in state",
			fmt.Sprintf("Could not parse type_id %q: %s", state.TypeID.ValueString(), err),
		)
		return
	}

	body := assocLabelUpdateWire{
		AssociationTypeID: typeID,
		Label:             plan.Label.ValueString(),
	}
	if !plan.InverseLabel.IsNull() && !plan.InverseLabel.IsUnknown() {
		body.InverseLabel = plan.InverseLabel.ValueString()
	}

	from, to := plan.FromObjectType.ValueString(), plan.ToObjectType.ValueString()
	p := assocLabelsPath(from, to)
	if err := r.client.Put(ctx, p, body, nil); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update HubSpot association label",
			fmt.Sprintf("PUT %s failed: %s", p, err),
		)
		return
	}

	// Only label text is mutable; type_id/inverse_type_id/category carry forward
	// from prior state via UseStateForUnknown, so the plan is fully known.
	plan.TypeID = state.TypeID
	plan.InverseTypeID = state.InverseTypeID
	plan.Category = state.Category
	plan.ID = types.StringValue(labelID(from, to, state.TypeID.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *associationLabelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state associationLabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	from, to := state.FromObjectType.ValueString(), state.ToObjectType.ValueString()
	p := assocLabelPath(from, to, state.TypeID.ValueString())
	if err := r.client.Delete(ctx, p, nil); err != nil {
		if client.IsNotFound(err) {
			return // Already gone; deletion is idempotent.
		}
		resp.Diagnostics.AddError(
			"Unable to delete HubSpot association label",
			fmt.Sprintf("DELETE %s failed: %s\n\nNote: deleting a label removes it from every record "+
				"association that uses it.", p, err),
		)
	}
}

func (r *associationLabelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Invalid hubspot_association_label import ID",
			fmt.Sprintf("Expected an import ID of the form \"{from_object_type}/{to_object_type}/{type_id}\" "+
				"with exactly two slashes, e.g. \"contacts/companies/145\", got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("from_object_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("to_object_type"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type_id"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// findLabel fetches the labels for the from/to pair and returns the one whose
// typeId matches typeIDStr. found is false when no such label exists.
func (r *associationLabelResource) findLabel(ctx context.Context, from, to, typeIDStr string) (assocLabelWire, bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	var out assocLabelListWire
	p := assocLabelsPath(from, to)
	if err := r.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			return assocLabelWire{}, false, diags
		}
		diags.AddError(
			"Unable to read HubSpot association labels",
			fmt.Sprintf("GET %s failed: %s", p, err),
		)
		return assocLabelWire{}, false, diags
	}
	for _, l := range out.Results {
		if strconv.FormatInt(l.TypeID, 10) == typeIDStr {
			return l, true, diags
		}
	}
	return assocLabelWire{}, false, diags
}

// assocLabelsPath is the collection path for the from/to object-type pair.
func assocLabelsPath(from, to string) string {
	return "crm/v4/associations/" + url.PathEscape(from) + "/" + url.PathEscape(to) + "/labels"
}

// assocLabelPath is the item path for one label typeId.
func assocLabelPath(from, to, typeID string) string {
	return assocLabelsPath(from, to) + "/" + url.PathEscape(typeID)
}

// labelID builds the composite import/state identifier.
func labelID(from, to, typeID string) string {
	return from + "/" + to + "/" + typeID
}
