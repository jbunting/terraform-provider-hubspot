// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource              = &pipelineDataSource{}
	_ datasource.DataSourceWithConfigure = &pipelineDataSource{}
)

// pipelineDataSource looks up a single CRM pipeline by object type and pipeline
// ID (GET /crm/v3/pipelines/{objectType}/{pipelineId}), exposing its stages.
// Handy for referencing HubSpot's built-in `default` pipeline and its stage IDs
// without managing them.
type pipelineDataSource struct {
	client *client.Client
}

// NewPipelineDataSource returns the hubspot_pipeline data source.
func NewPipelineDataSource() datasource.DataSource {
	return &pipelineDataSource{}
}

type pipelineDataSourceModel struct {
	ID           types.String             `tfsdk:"id"`
	ObjectType   types.String             `tfsdk:"object_type"`
	PipelineID   types.String             `tfsdk:"pipeline_id"`
	Label        types.String             `tfsdk:"label"`
	DisplayOrder types.Int64              `tfsdk:"display_order"`
	Stages       []pipelineStageDataModel `tfsdk:"stages"`
}

type pipelineStageDataModel struct {
	StageID      types.String `tfsdk:"stage_id"`
	Label        types.String `tfsdk:"label"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
	Metadata     types.Map    `tfsdk:"metadata"`
}

func (d *pipelineDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (d *pipelineDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a CRM pipeline by object type and pipeline ID, exposing its stages " +
			"(sorted by display order). Unlike the resource, all stage metadata keys HubSpot returns are shown " +
			"as-is. Requires the `crm.pipelines.read` scope.",
		Attributes: map[string]schema.Attribute{
			"object_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "CRM object type the pipeline belongs to: `deals`, `tickets`, or a custom object type ID.",
			},
			"pipeline_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The pipeline's ID (e.g. `default` for HubSpot's built-in pipeline).",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier for the data source (`{object_type}/{pipeline_id}`).",
			},
			"label": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable pipeline label.",
			},
			"display_order": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Position of the pipeline relative to other pipelines.",
			},
			"stages": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The pipeline's stages, ordered by display order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"stage_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Internal stage ID.",
						},
						"label": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Human-readable stage label.",
						},
						"display_order": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: "Position of the stage within the pipeline.",
						},
						"metadata": schema.MapAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "All stage metadata keys returned by HubSpot (e.g. `probability`, `isClosed`, `ticketState`).",
						},
					},
				},
			},
		},
	}
}

func (d *pipelineDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *pipelineDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config pipelineDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectType, pipelineID := config.ObjectType.ValueString(), config.PipelineID.ValueString()
	p := pipelinePath(objectType, pipelineID)
	var out pipelineWire
	if err := d.client.Get(ctx, p, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("HubSpot pipeline not found",
				fmt.Sprintf("No pipeline %q exists on object type %q.", pipelineID, objectType))
			return
		}
		resp.Diagnostics.AddError("Unable to read HubSpot pipeline", "HubSpot API request failed: "+err.Error())
		return
	}

	state := pipelineDataSourceModel{
		ID:         types.StringValue(objectType + "/" + out.ID),
		ObjectType: config.ObjectType,
		PipelineID: types.StringValue(out.ID),
		Label:      types.StringValue(out.Label),
		Stages:     make([]pipelineStageDataModel, 0, len(out.Stages)),
	}
	if out.DisplayOrder != nil {
		state.DisplayOrder = types.Int64Value(*out.DisplayOrder)
	} else {
		state.DisplayOrder = types.Int64Value(0)
	}

	sorted := make([]pipelineStageWire, len(out.Stages))
	copy(sorted, out.Stages)
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

	for _, s := range sorted {
		md, diags := types.MapValueFrom(ctx, types.StringType, s.Metadata)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		order := int64(0)
		if s.DisplayOrder != nil {
			order = *s.DisplayOrder
		}
		state.Stages = append(state.Stages, pipelineStageDataModel{
			StageID:      types.StringValue(s.ID),
			Label:        types.StringValue(s.Label),
			DisplayOrder: types.Int64Value(order),
			Metadata:     md,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
