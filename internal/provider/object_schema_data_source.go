// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource              = &objectSchemaDataSource{}
	_ datasource.DataSourceWithConfigure = &objectSchemaDataSource{}
)

// objectSchemaDataSource looks up a custom object schema by object type
// (GET /crm/v3/schemas/{objectType}), resolving its portal-specific
// objectTypeId (2-XXXX) and display configuration by name.
type objectSchemaDataSource struct {
	client *client.Client
}

// NewObjectSchemaDataSource returns the hubspot_object_schema data source.
func NewObjectSchemaDataSource() datasource.DataSource {
	return &objectSchemaDataSource{}
}

type objectSchemaDataSourceModel struct {
	ObjectType                 types.String `tfsdk:"object_type"`
	ID                         types.String `tfsdk:"id"`
	ObjectTypeID               types.String `tfsdk:"object_type_id"`
	FullyQualifiedName         types.String `tfsdk:"fully_qualified_name"`
	Name                       types.String `tfsdk:"name"`
	LabelSingular              types.String `tfsdk:"label_singular"`
	LabelPlural                types.String `tfsdk:"label_plural"`
	PrimaryDisplayProperty     types.String `tfsdk:"primary_display_property"`
	SecondaryDisplayProperties types.List   `tfsdk:"secondary_display_properties"`
	RequiredProperties         types.List   `tfsdk:"required_properties"`
	SearchableProperties       types.List   `tfsdk:"searchable_properties"`
	Description                types.String `tfsdk:"description"`
	Associations               types.List   `tfsdk:"associations"`
}

func (d *objectSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_schema"
}

func (d *objectSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a custom object schema by object type, resolving its portal-specific " +
			"`object_type_id` (`2-XXXX`) and display configuration. Cross-portal configs should reference " +
			"custom objects by name and resolve the ID here. Requires the `crm.schemas.custom.read` scope " +
			"(Enterprise-tier feature).",
		Attributes: map[string]schema.Attribute{
			"object_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The custom object to look up: its `objectTypeId` (`2-XXXX`), fully " +
					"qualified name, or object name.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier for the data source (the resolved `object_type_id`).",
			},
			"object_type_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Portal-specific object type ID (`2-XXXX`).",
			},
			"fully_qualified_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fully qualified name (`p{portalId}_{name}`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Internal object name.",
			},
			"label_singular": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Singular UI label.",
			},
			"label_plural": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Plural UI label.",
			},
			"primary_display_property": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The property used as the object's primary display label.",
			},
			"secondary_display_properties": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Properties shown alongside the primary display property.",
			},
			"required_properties": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Properties required when creating a record of this object.",
			},
			"searchable_properties": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Properties indexed for search.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the object schema.",
			},
			"associations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: schemaAssociationsDescription,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: schemaAssociationIDDescription,
						},
						"from_object_type_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type ID the association points from.",
						},
						"to_object_type_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type ID the association points to.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "HubSpot's internal name for the association definition.",
						},
					},
				},
			},
		},
	}
}

func (d *objectSchemaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *objectSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config objectSchemaDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectType := config.ObjectType.ValueString()
	p := "crm/v3/schemas/" + url.PathEscape(objectType)
	var out objectSchemaWire
	if err := d.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("HubSpot object schema not found",
				fmt.Sprintf("No custom object schema %q exists.", objectType))
			return
		}
		resp.Diagnostics.AddError("Unable to read HubSpot object schema", "HubSpot API request failed: "+err.Error())
		return
	}

	secondary, diags := stringListValue(ctx, out.SecondaryDisplayProperties)
	resp.Diagnostics.Append(diags...)
	required, diags := stringListValue(ctx, out.RequiredProperties)
	resp.Diagnostics.Append(diags...)
	searchable, diags := stringListValue(ctx, out.SearchableProperties)
	resp.Diagnostics.Append(diags...)
	associations, diags := flattenSchemaAssociations(out.Associations)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := objectSchemaDataSourceModel{
		ObjectType:                 config.ObjectType,
		ID:                         types.StringValue(out.ObjectTypeID),
		ObjectTypeID:               types.StringValue(out.ObjectTypeID),
		FullyQualifiedName:         types.StringValue(out.FullyQualifiedName),
		Name:                       types.StringValue(out.Name),
		LabelSingular:              types.StringValue(out.Labels.Singular),
		LabelPlural:                types.StringValue(out.Labels.Plural),
		PrimaryDisplayProperty:     types.StringValue(out.PrimaryDisplayProperty),
		SecondaryDisplayProperties: secondary,
		RequiredProperties:         required,
		SearchableProperties:       searchable,
		Description:                types.StringValue(out.Description),
		Associations:               associations,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// stringListValue converts a []string (possibly nil) into a non-null
// types.List of strings; a nil slice becomes an empty list.
func stringListValue(ctx context.Context, in []string) (types.List, diag.Diagnostics) {
	if in == nil {
		in = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, in)
}
