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
	_ datasource.DataSource              = &propertiesDataSource{}
	_ datasource.DataSourceWithConfigure = &propertiesDataSource{}
)

// propertiesDataSource lists every property defined on a CRM object type,
// including HubSpot-defined defaults. Useful for discovering property names,
// asserting a group's contents, or filtering with HCL expressions.
type propertiesDataSource struct {
	client *client.Client
}

// NewPropertiesDataSource returns the hubspot_properties data source.
func NewPropertiesDataSource() datasource.DataSource {
	return &propertiesDataSource{}
}

type propertiesDataSourceModel struct {
	ID              types.String            `tfsdk:"id"`
	ObjectType      types.String            `tfsdk:"object_type"`
	IncludeArchived types.Bool              `tfsdk:"include_archived"`
	Properties      []propertyListItemModel `tfsdk:"properties"`
}

type propertyListItemModel struct {
	Name           types.String `tfsdk:"name"`
	Label          types.String `tfsdk:"label"`
	Type           types.String `tfsdk:"type"`
	FieldType      types.String `tfsdk:"field_type"`
	GroupName      types.String `tfsdk:"group_name"`
	Description    types.String `tfsdk:"description"`
	Hidden         types.Bool   `tfsdk:"hidden"`
	Calculated     types.Bool   `tfsdk:"calculated"`
	HubspotDefined types.Bool   `tfsdk:"hubspot_defined"`
	Archived       types.Bool   `tfsdk:"archived"`
}

func (d *propertiesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_properties"
}

func (d *propertiesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every property defined on a CRM object type, including HubSpot-defined " +
			"defaults. Archived properties are excluded unless `include_archived` is set. Filter the " +
			"returned list with HCL expressions (e.g. `[for p in data.hubspot_properties.x.properties : " +
			"p.name if !p.hubspot_defined]`). Requires the `crm.schemas.{objectType}.read` scope.",
		Attributes: map[string]schema.Attribute{
			"object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "CRM object type to list properties for (e.g. `contacts`, `deals`).",
			},
			"include_archived": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "When `true`, returns archived properties **instead of** active ones " +
					"(mirrors the HubSpot API's `archived` query parameter). Defaults to `false`.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier for the data source (the object type).",
			},
			"properties": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The properties defined on the object type, ordered by internal name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Internal name of the property.",
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
						"archived": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether the property is archived.",
						},
					},
				},
			},
		},
	}
}

func (d *propertiesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

type propertiesListAPI struct {
	Results []propertyReadAPIExt `json:"results"`
}

// propertyReadAPIExt extends propertyReadAPI with the archived flag, which the
// list endpoint returns per property.
type propertyReadAPIExt struct {
	propertyReadAPI
	Archived bool `json:"archived"`
}

func (d *propertiesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config propertiesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectType := config.ObjectType.ValueString()
	query := url.Values{}
	if config.IncludeArchived.ValueBool() {
		query.Set("archived", "true")
	}
	path := fmt.Sprintf("/crm/v3/properties/%s", url.PathEscape(objectType))

	var got propertiesListAPI
	if err := d.client.Get(ctx, path, query, &got); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("HubSpot object type not found",
				fmt.Sprintf("No CRM object type %q exists, or it exposes no properties.", objectType))
			return
		}
		resp.Diagnostics.AddError("Unable to list HubSpot properties", "HubSpot API request failed: "+err.Error())
		return
	}

	state := propertiesDataSourceModel{
		ID:              types.StringValue(objectType),
		ObjectType:      config.ObjectType,
		IncludeArchived: config.IncludeArchived,
		Properties:      make([]propertyListItemModel, 0, len(got.Results)),
	}
	for _, p := range got.Results {
		state.Properties = append(state.Properties, propertyListItemModel{
			Name:           types.StringValue(p.Name),
			Label:          types.StringValue(p.Label),
			Type:           types.StringValue(p.Type),
			FieldType:      types.StringValue(p.FieldType),
			GroupName:      types.StringValue(p.GroupName),
			Description:    types.StringValue(p.Description),
			Hidden:         types.BoolValue(p.Hidden),
			Calculated:     types.BoolValue(p.Calculated),
			HubspotDefined: types.BoolValue(p.HubspotDefined),
			Archived:       types.BoolValue(p.Archived),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
