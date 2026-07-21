// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource              = &associationLabelsDataSource{}
	_ datasource.DataSourceWithConfigure = &associationLabelsDataSource{}
)

// associationLabelsDataSource lists every association label defined between a
// pair of object types (GET /crm/v4/associations/{from}/{to}/labels), including
// HubSpot-defined ones. Useful for discovering the portal-specific type IDs
// that record associations reference.
type associationLabelsDataSource struct {
	client *client.Client
}

// NewAssociationLabelsDataSource returns the hubspot_association_labels data source.
func NewAssociationLabelsDataSource() datasource.DataSource {
	return &associationLabelsDataSource{}
}

type associationLabelsDataSourceModel struct {
	ID             types.String                `tfsdk:"id"`
	FromObjectType types.String                `tfsdk:"from_object_type"`
	ToObjectType   types.String                `tfsdk:"to_object_type"`
	Labels         []associationLabelItemModel `tfsdk:"labels"`
}

type associationLabelItemModel struct {
	TypeID   types.String `tfsdk:"type_id"`
	Label    types.String `tfsdk:"label"`
	Category types.String `tfsdk:"category"`
}

func (d *associationLabelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_association_labels"
}

func (d *associationLabelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every association label defined between two CRM object types, including " +
			"HubSpot-defined ones. Association `type_id`s are **portal-specific**, so this is the way to resolve " +
			"a label by name across portals. Requires the object read scopes of both sides.",
		Attributes: map[string]schema.Attribute{
			"from_object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Source CRM object type (e.g. `contacts`, `companies`, or a custom object type ID).",
			},
			"to_object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Target CRM object type.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier for the data source (`{from_object_type}/{to_object_type}`).",
			},
			"labels": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The association labels defined for the pair, in the order HubSpot returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Portal-specific association type ID.",
						},
						"label": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Human-readable label text (may be empty for an unlabeled association type).",
						},
						"category": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Association category: `HUBSPOT_DEFINED` or `USER_DEFINED`.",
						},
					},
				},
			},
		},
	}
}

func (d *associationLabelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *associationLabelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config associationLabelsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	from, to := config.FromObjectType.ValueString(), config.ToObjectType.ValueString()
	p := assocLabelsPath(from, to)
	var out assocLabelListWire
	if err := d.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("Association pair not found",
				fmt.Sprintf("No association definitions exist between %q and %q.", from, to))
			return
		}
		resp.Diagnostics.AddError("Unable to list HubSpot association labels", "HubSpot API request failed: "+err.Error())
		return
	}

	state := associationLabelsDataSourceModel{
		ID:             types.StringValue(from + "/" + to),
		FromObjectType: config.FromObjectType,
		ToObjectType:   config.ToObjectType,
		Labels:         make([]associationLabelItemModel, 0, len(out.Results)),
	}
	for _, l := range out.Results {
		state.Labels = append(state.Labels, associationLabelItemModel{
			TypeID:   types.StringValue(strconv.FormatInt(l.TypeID, 10)),
			Label:    types.StringValue(l.Label),
			Category: types.StringValue(l.Category),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
