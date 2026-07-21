// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource              = &propertyDataSource{}
	_ datasource.DataSourceWithConfigure = &propertyDataSource{}
)

// propertyDataSource reads a single CRM property by object type and name,
// including HubSpot-defined properties the provider does not manage. Useful
// for referencing existing properties (e.g. lifecyclestage) from config.
type propertyDataSource struct {
	client *client.Client
}

// NewPropertyDataSource returns the hubspot_property data source.
func NewPropertyDataSource() datasource.DataSource {
	return &propertyDataSource{}
}

type propertyDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	ObjectType     types.String `tfsdk:"object_type"`
	Name           types.String `tfsdk:"name"`
	Label          types.String `tfsdk:"label"`
	Type           types.String `tfsdk:"type"`
	FieldType      types.String `tfsdk:"field_type"`
	GroupName      types.String `tfsdk:"group_name"`
	Description    types.String `tfsdk:"description"`
	Hidden         types.Bool   `tfsdk:"hidden"`
	Calculated     types.Bool   `tfsdk:"calculated"`
	HubspotDefined types.Bool   `tfsdk:"hubspot_defined"`
}

func (d *propertyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_property"
}

func (d *propertyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single CRM property definition by object type and name, including " +
			"HubSpot-defined properties this provider does not manage. Requires the " +
			"`crm.schemas.{objectType}.read` scope.",
		Attributes: map[string]schema.Attribute{
			"object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "CRM object type the property belongs to (e.g. `contacts`, `deals`).",
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Internal name of the property.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier `{object_type}/{name}`.",
			},
			"label": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable label shown in the HubSpot UI.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Data type (`string`, `number`, `bool`, `enumeration`, `date`, `datetime`).",
			},
			"field_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "UI field type (e.g. `text`, `select`, `checkbox`).",
			},
			"group_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the property group the property belongs to.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the property.",
			},
			"hidden": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the property is hidden in the HubSpot UI.",
			},
			"calculated": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the property is calculated from a formula.",
			},
			"hubspot_defined": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the property is a HubSpot-defined default (not a custom property).",
			},
		},
	}
}

func (d *propertyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

type propertyReadAPI struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	Type           string `json:"type"`
	FieldType      string `json:"fieldType"`
	GroupName      string `json:"groupName"`
	Description    string `json:"description"`
	Hidden         bool   `json:"hidden"`
	Calculated     bool   `json:"calculated"`
	HubspotDefined bool   `json:"hubspotDefined"`
}

func (d *propertyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config propertyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectType := config.ObjectType.ValueString()
	name := config.Name.ValueString()
	path := fmt.Sprintf("/crm/v3/properties/%s/%s", url.PathEscape(objectType), url.PathEscape(name))

	var got propertyReadAPI
	if err := d.client.Get(ctx, path, nil, &got); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("HubSpot property not found",
				fmt.Sprintf("No property %q exists on object type %q.", name, objectType))
			return
		}
		resp.Diagnostics.AddError("Unable to read HubSpot property", "HubSpot API request failed: "+err.Error())
		return
	}

	state := propertyDataSourceModel{
		ID:             types.StringValue(objectType + "/" + got.Name),
		ObjectType:     types.StringValue(objectType),
		Name:           types.StringValue(got.Name),
		Label:          types.StringValue(got.Label),
		Type:           types.StringValue(got.Type),
		FieldType:      types.StringValue(got.FieldType),
		GroupName:      types.StringValue(got.GroupName),
		Description:    types.StringValue(got.Description),
		Hidden:         types.BoolValue(got.Hidden),
		Calculated:     types.BoolValue(got.Calculated),
		HubspotDefined: types.BoolValue(got.HubspotDefined),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
