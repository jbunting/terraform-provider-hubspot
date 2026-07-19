// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &propertyResource{}
	_ resource.ResourceWithConfigure   = &propertyResource{}
	_ resource.ResourceWithImportState = &propertyResource{}
)

// propertyResource implements the hubspot_property resource: a custom CRM
// property definition managed via /crm/v3/properties/{objectType}.
type propertyResource struct {
	client *client.Client
}

// NewPropertyResource returns the hubspot_property resource.
func NewPropertyResource() resource.Resource {
	return &propertyResource{}
}

// propertyResourceModel maps the hubspot_property Terraform schema.
type propertyResourceModel struct {
	ID             types.String `tfsdk:"id"`
	ObjectType     types.String `tfsdk:"object_type"`
	Name           types.String `tfsdk:"name"`
	Label          types.String `tfsdk:"label"`
	Type           types.String `tfsdk:"type"`
	FieldType      types.String `tfsdk:"field_type"`
	GroupName      types.String `tfsdk:"group_name"`
	Description    types.String `tfsdk:"description"`
	DisplayOrder   types.Int64  `tfsdk:"display_order"`
	Hidden         types.Bool   `tfsdk:"hidden"`
	FormField      types.Bool   `tfsdk:"form_field"`
	HasUniqueValue types.Bool   `tfsdk:"has_unique_value"`
	Options        types.List   `tfsdk:"options"`
}

// propertyOptionModel maps one element of the options list. There is
// deliberately no display_order attribute: list position is the order.
type propertyOptionModel struct {
	Label       types.String `tfsdk:"label"`
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
	Hidden      types.Bool   `tfsdk:"hidden"`
}

// propertyOptionAttrTypes is the object type of one options element.
var propertyOptionAttrTypes = map[string]attr.Type{
	"label":       types.StringType,
	"value":       types.StringType,
	"description": types.StringType,
	"hidden":      types.BoolType,
}

// propertyAPI is the wire shape of a property for both requests and
// responses of the /crm/v3/properties endpoints.
type propertyAPI struct {
	Name           string              `json:"name,omitempty"`
	Label          string              `json:"label,omitempty"`
	Type           string              `json:"type,omitempty"`
	FieldType      string              `json:"fieldType,omitempty"`
	GroupName      string              `json:"groupName,omitempty"`
	Description    string              `json:"description"`
	DisplayOrder   *int64              `json:"displayOrder,omitempty"`
	Hidden         *bool               `json:"hidden,omitempty"`
	FormField      *bool               `json:"formField,omitempty"`
	HasUniqueValue *bool               `json:"hasUniqueValue,omitempty"`
	Options        []propertyOptionAPI `json:"options"`
}

// propertyOptionAPI is the wire shape of one enumeration option.
type propertyOptionAPI struct {
	Label        string `json:"label"`
	Value        string `json:"value"`
	Description  string `json:"description,omitempty"`
	DisplayOrder int64  `json:"displayOrder"`
	Hidden       bool   `json:"hidden"`
}

func (r *propertyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_property"
}

func (r *propertyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a custom CRM property definition (`/crm/v3/properties/{objectType}`). " +
			"Requires the `crm.schemas.{objectType}.write` scope for the target object type (e.g. " +
			"`crm.schemas.contacts.write`).\n\n" +
			"**Destroy archives, it does not delete**: HubSpot soft-deletes properties, and an archived " +
			"property's name stays reserved for roughly 90 days (\"name purgatory\"). Re-creating a property " +
			"with a recently archived name fails until the archived property is purged or restored in the " +
			"HubSpot UI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the form `{object_type}/{name}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"object_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "CRM object type the property belongs to (e.g. `contacts`, `companies`, " +
					"`deals`, `tickets`, or a custom object type ID). Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Internal property name. Changing this forces replacement, which archives " +
					"the old property and **destroys the data stored in it on every record**. The archived " +
					"name also stays reserved for ~90 days, blocking immediate reuse.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable label shown in the HubSpot UI.",
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Data type of the property: `string`, `number`, `bool`, `enumeration`, " +
					"`date`, or `datetime`. Changing this forces replacement (destroys record data).",
				Validators: []validator.String{
					stringOneOf("string", "number", "bool", "enumeration", "date", "datetime"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"field_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "UI control used to edit the property: `text`, `textarea`, `number`, " +
					"`date`, `select`, `radio`, `checkbox`, `booleancheckbox`, `file`, `html`, `phonenumber`, " +
					"or `calculation_equation`. Mutable within the same `type`.",
				Validators: []validator.String{
					stringOneOf("text", "textarea", "number", "date", "select", "radio", "checkbox",
						"booleancheckbox", "file", "html", "phonenumber", "calculation_equation"),
				},
			},
			"group_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the property group the property is displayed under.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description shown to users in the HubSpot UI.",
			},
			"display_order": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Position of the property within its group (lower sorts first). When " +
					"unset, HubSpot assigns `-1` (unordered). Note HubSpot rewrites `0` to `-1` server-side.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"hidden": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the property is hidden in the HubSpot UI. Defaults to `false`.",
			},
			"form_field": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the property can be used in HubSpot forms. Defaults to `false`.",
			},
			"has_unique_value": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether values must be unique across records. Defaults to `false`. " +
					"Changing this forces replacement (destroys record data).",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"options": schema.ListNestedAttribute{
				Optional: true,
				MarkdownDescription: "Ordered choices for `enumeration` properties. **List position is the " +
					"display order** — reorder the list to reorder options. Option identity is `value`; " +
					"changing a value orphans record data stored under the old value.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"label": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Label shown for this choice in the HubSpot UI.",
						},
						"value": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Internal value stored on records. Identifies the option.",
						},
						"description": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Description of this choice.",
						},
						"hidden": schema.BoolAttribute{
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
							MarkdownDescription: "Whether the choice is hidden in the HubSpot UI. Defaults to `false`.",
						},
					},
				},
			},
		},
	}
}

func (r *propertyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *propertyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan propertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := expandProperty(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var out propertyAPI
	if err := r.client.Post(ctx, propertiesPath(plan.ObjectType.ValueString()), body, &out); err != nil {
		var apiErr *client.APIError
		if client.AsAPIError(err, &apiErr) && apiErr.StatusCode == 400 &&
			strings.Contains(strings.ToLower(apiErr.Message), "archived") {
			resp.Diagnostics.AddError(
				"HubSpot property name is in post-archive purgatory",
				fmt.Sprintf("A property named %q on %q was recently archived, and HubSpot reserves archived "+
					"property names for roughly 90 days. Choose a different name, restore the archived "+
					"property in the HubSpot UI, or purge the archived property, then retry.\n\nAPI error: %s",
					plan.Name.ValueString(), plan.ObjectType.ValueString(), err),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to create HubSpot property",
			fmt.Sprintf("POST %s failed: %s", propertiesPath(plan.ObjectType.ValueString()), err),
		)
		return
	}

	resp.Diagnostics.Append(flattenProperty(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *propertyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state propertyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := propertyPath(state.ObjectType.ValueString(), state.Name.ValueString())
	var out propertyAPI
	if err := r.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read HubSpot property",
			fmt.Sprintf("GET %s failed: %s", p, err),
		)
		return
	}

	resp.Diagnostics.Append(flattenProperty(ctx, out, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *propertyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, config propertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := expandProperty(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// PATCH must carry mutable fields only: name, type and hasUniqueValue are
	// immutable (RequiresReplace), and displayOrder is sent only when it is
	// actually configured, so an unconfigured value keeps tracking the server.
	body.Name = ""
	body.Type = ""
	body.HasUniqueValue = nil
	if config.DisplayOrder.IsNull() {
		body.DisplayOrder = nil
	}

	p := propertyPath(plan.ObjectType.ValueString(), plan.Name.ValueString())
	var out propertyAPI
	if err := r.client.Patch(ctx, p, body, &out); err != nil {
		resp.Diagnostics.AddError(
			"Unable to update HubSpot property",
			fmt.Sprintf("PATCH %s failed: %s", p, err),
		)
		return
	}

	resp.Diagnostics.Append(flattenProperty(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *propertyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state propertyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := propertyPath(state.ObjectType.ValueString(), state.Name.ValueString())
	if err := r.client.Delete(ctx, p, nil); err != nil {
		if client.IsNotFound(err) {
			return // Already gone; deletion is idempotent.
		}
		resp.Diagnostics.AddError(
			"Unable to delete (archive) HubSpot property",
			fmt.Sprintf("DELETE %s failed: %s", p, err),
		)
	}
}

func (r *propertyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid hubspot_property import ID",
			fmt.Sprintf("Expected an import ID of the form \"{object_type}/{name}\" with exactly one slash, "+
				"e.g. \"contacts/customer_tier\", got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// propertiesPath is the collection path for objectType.
func propertiesPath(objectType string) string {
	return "crm/v3/properties/" + url.PathEscape(objectType)
}

// propertyPath is the item path for one named property.
func propertyPath(objectType, name string) string {
	return propertiesPath(objectType) + "/" + url.PathEscape(name)
}

// expandProperty converts the Terraform model into the API wire shape for a
// create. Option displayOrder is derived from list position; property
// displayOrder is sent only when configured (known and non-null), matching
// HubSpot's "unset means -1" server normalization.
func expandProperty(ctx context.Context, m propertyResourceModel) (propertyAPI, diag.Diagnostics) {
	var diags diag.Diagnostics

	body := propertyAPI{
		Name:        m.Name.ValueString(),
		Label:       m.Label.ValueString(),
		Type:        m.Type.ValueString(),
		FieldType:   m.FieldType.ValueString(),
		GroupName:   m.GroupName.ValueString(),
		Description: m.Description.ValueString(),
		Options:     []propertyOptionAPI{},
	}
	if !m.DisplayOrder.IsNull() && !m.DisplayOrder.IsUnknown() {
		v := m.DisplayOrder.ValueInt64()
		body.DisplayOrder = &v
	}
	if !m.Hidden.IsNull() && !m.Hidden.IsUnknown() {
		v := m.Hidden.ValueBool()
		body.Hidden = &v
	}
	if !m.FormField.IsNull() && !m.FormField.IsUnknown() {
		v := m.FormField.ValueBool()
		body.FormField = &v
	}
	if !m.HasUniqueValue.IsNull() && !m.HasUniqueValue.IsUnknown() {
		v := m.HasUniqueValue.ValueBool()
		body.HasUniqueValue = &v
	}

	if !m.Options.IsNull() && !m.Options.IsUnknown() {
		var options []propertyOptionModel
		diags.Append(m.Options.ElementsAs(ctx, &options, false)...)
		if diags.HasError() {
			return body, diags
		}
		for i, o := range options {
			body.Options = append(body.Options, propertyOptionAPI{
				Label:        o.Label.ValueString(),
				Value:        o.Value.ValueString(),
				Description:  o.Description.ValueString(),
				DisplayOrder: int64(i), // list position is the order
				Hidden:       o.Hidden.ValueBool(),
			})
		}
	}

	return body, diags
}

// flattenProperty writes the API response over the model in place. The model
// must already carry object_type (and, for ambiguity resolution, the prior
// description/options null-ness). Options are sorted by the server-assigned
// displayOrder and stored without it: list order is the order.
func flattenProperty(ctx context.Context, api propertyAPI, m *propertyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.Name = types.StringValue(api.Name)
	m.ID = types.StringValue(m.ObjectType.ValueString() + "/" + api.Name)
	m.Label = types.StringValue(api.Label)
	m.Type = types.StringValue(api.Type)
	m.FieldType = types.StringValue(api.FieldType)
	m.GroupName = types.StringValue(api.GroupName)

	// HubSpot returns "" for an absent description; keep a null config null.
	if api.Description == "" && (m.Description.IsNull() || m.Description.IsUnknown()) {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(api.Description)
	}

	if api.DisplayOrder != nil {
		m.DisplayOrder = types.Int64Value(*api.DisplayOrder)
	} else {
		m.DisplayOrder = types.Int64Value(-1)
	}
	m.Hidden = types.BoolValue(api.Hidden != nil && *api.Hidden)
	m.FormField = types.BoolValue(api.FormField != nil && *api.FormField)
	m.HasUniqueValue = types.BoolValue(api.HasUniqueValue != nil && *api.HasUniqueValue)

	optionType := types.ObjectType{AttrTypes: propertyOptionAttrTypes}
	if len(api.Options) == 0 && (m.Options.IsNull() || m.Options.IsUnknown()) {
		m.Options = types.ListNull(optionType)
		return diags
	}

	sorted := make([]propertyOptionAPI, len(api.Options))
	copy(sorted, api.Options)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DisplayOrder < sorted[j].DisplayOrder })

	elems := make([]attr.Value, 0, len(sorted))
	for _, o := range sorted {
		description := types.StringNull()
		if o.Description != "" {
			description = types.StringValue(o.Description)
		}
		obj, objDiags := types.ObjectValue(propertyOptionAttrTypes, map[string]attr.Value{
			"label":       types.StringValue(o.Label),
			"value":       types.StringValue(o.Value),
			"description": description,
			"hidden":      types.BoolValue(o.Hidden),
		})
		diags.Append(objDiags...)
		elems = append(elems, obj)
	}
	list, listDiags := types.ListValue(optionType, elems)
	diags.Append(listDiags...)
	m.Options = list

	return diags
}

// stringOneOf returns a validator that requires the configured string to be
// one of the given values. (Kept local to avoid pulling in the separate
// terraform-plugin-framework-validators module for a single check.)
func stringOneOf(values ...string) validator.String {
	return stringOneOfValidator{values: values}
}

type stringOneOfValidator struct {
	values []string
}

func (v stringOneOfValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be one of: %s", strings.Join(v.values, ", "))
}

func (v stringOneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v stringOneOfValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, want := range v.values {
		if got == want {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Attribute Value",
		fmt.Sprintf("Attribute %s %s, got: %q.", req.Path, v.Description(ctx), got),
	)
}
