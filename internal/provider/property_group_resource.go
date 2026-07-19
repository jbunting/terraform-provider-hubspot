// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &propertyGroupResource{}
	_ resource.ResourceWithConfigure   = &propertyGroupResource{}
	_ resource.ResourceWithImportState = &propertyGroupResource{}
)

// propertyGroupResource manages a HubSpot CRM property group
// (POST/GET/PATCH/DELETE /crm/v3/properties/{objectType}/groups).
type propertyGroupResource struct {
	client *client.Client
}

// NewPropertyGroupResource returns the hubspot_property_group resource.
func NewPropertyGroupResource() resource.Resource {
	return &propertyGroupResource{}
}

// propertyGroupResourceModel maps the resource schema to framework types.
type propertyGroupResourceModel struct {
	ID           types.String `tfsdk:"id"`
	ObjectType   types.String `tfsdk:"object_type"`
	Name         types.String `tfsdk:"name"`
	Label        types.String `tfsdk:"label"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
}

// propertyGroupAPI is the HubSpot wire representation of a property group.
type propertyGroupAPI struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	DisplayOrder int64  `json:"displayOrder"`
}

func (r *propertyGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_property_group"
}

func (r *propertyGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a HubSpot CRM property group — a named section that organizes properties " +
			"in the HubSpot UI. Property groups are referenced by `hubspot_property` via `group_name`. " +
			"Requires the `crm.schemas.{objectType}.write` scope for the target object type " +
			"(e.g. `crm.schemas.contacts.write`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Composite identifier in the form `{object_type}/{name}`, " +
					"e.g. `contacts/my_group`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"object_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "CRM object type the group belongs to — an object type name such as " +
					"`contacts`, `companies`, `deals`, `tickets`, or a custom object `objectTypeId` " +
					"(`2-XXXX`). Changing this forces replacement of the group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Internal (immutable) name of the property group. Changing this forces " +
					"replacement of the group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable label shown in the HubSpot UI. Can be changed in place.",
			},
			"display_order": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Position of the group relative to other groups in the HubSpot UI " +
					"(lower values sort first). Can be changed in place. If omitted, the server-assigned " +
					"value is used.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *propertyGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData),
		)
		return
	}
	r.client = c
}

// groupPath returns the API path for a single group, path-escaping segments.
func groupPath(objectType, name string) string {
	return fmt.Sprintf("/crm/v3/properties/%s/groups/%s", url.PathEscape(objectType), url.PathEscape(name))
}

// setFromAPI overwrites the server-owned fields of the model from the API
// response and derives the composite id.
func (m *propertyGroupResourceModel) setFromAPI(g propertyGroupAPI) {
	m.Name = types.StringValue(g.Name)
	m.Label = types.StringValue(g.Label)
	m.DisplayOrder = types.Int64Value(g.DisplayOrder)
	m.ID = types.StringValue(m.ObjectType.ValueString() + "/" + g.Name)
}

func (r *propertyGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan propertyGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{
		"name":  plan.Name.ValueString(),
		"label": plan.Label.ValueString(),
	}
	if !plan.DisplayOrder.IsNull() && !plan.DisplayOrder.IsUnknown() {
		body["displayOrder"] = plan.DisplayOrder.ValueInt64()
	}

	var created propertyGroupAPI
	createPath := fmt.Sprintf("/crm/v3/properties/%s/groups", url.PathEscape(plan.ObjectType.ValueString()))
	if err := r.client.Post(ctx, createPath, body, &created); err != nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Unable to create HubSpot property group %q on %s",
				plan.Name.ValueString(), plan.ObjectType.ValueString()),
			"HubSpot API request failed: "+err.Error()+
				"\n\nIf the group already exists (HTTP 409), import it instead: "+
				fmt.Sprintf("terraform import <address> '%s/%s'. ",
					plan.ObjectType.ValueString(), plan.Name.ValueString())+
				"A 403 usually means the private app token is missing the "+
				fmt.Sprintf("crm.schemas.%s.write scope.", plan.ObjectType.ValueString()),
		)
		return
	}

	plan.setFromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *propertyGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state propertyGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var got propertyGroupAPI
	err := r.client.Get(ctx, groupPath(state.ObjectType.ValueString(), state.Name.ValueString()), nil, &got)
	if err != nil {
		if client.IsNotFound(err) {
			// Deleted out-of-band: drop from state so Terraform plans a recreate.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			fmt.Sprintf("Unable to read HubSpot property group %q on %s",
				state.Name.ValueString(), state.ObjectType.ValueString()),
			"HubSpot API request failed: "+err.Error(),
		)
		return
	}

	state.setFromAPI(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *propertyGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan propertyGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{
		"label": plan.Label.ValueString(),
	}
	if !plan.DisplayOrder.IsNull() && !plan.DisplayOrder.IsUnknown() {
		body["displayOrder"] = plan.DisplayOrder.ValueInt64()
	}

	var updated propertyGroupAPI
	err := r.client.Patch(ctx, groupPath(plan.ObjectType.ValueString(), plan.Name.ValueString()), body, &updated)
	if err != nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Unable to update HubSpot property group %q on %s",
				plan.Name.ValueString(), plan.ObjectType.ValueString()),
			"HubSpot API request failed: "+err.Error()+
				"\n\nIf the group no longer exists, run terraform apply again to recreate it. "+
				"HubSpot-defined groups cannot be modified.",
		)
		return
	}

	plan.setFromAPI(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *propertyGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state propertyGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, groupPath(state.ObjectType.ValueString(), state.Name.ValueString()), nil)
	if err != nil && !client.IsNotFound(err) { // already gone counts as deleted
		resp.Diagnostics.AddError(
			fmt.Sprintf("Unable to delete HubSpot property group %q on %s",
				state.Name.ValueString(), state.ObjectType.ValueString()),
			"HubSpot API request failed: "+err.Error()+
				"\n\nHubSpot-defined groups cannot be deleted; if this group should no longer be "+
				"managed by Terraform, remove it with: terraform state rm <address>.",
		)
		return
	}
}

func (r *propertyGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID for hubspot_property_group",
			fmt.Sprintf("Expected an import ID of the form \"{object_type}/{name}\" with exactly one slash, "+
				"e.g. \"contacts/my_group\", got: %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
