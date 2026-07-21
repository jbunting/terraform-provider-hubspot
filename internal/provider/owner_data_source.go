// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource                     = &ownerDataSource{}
	_ datasource.DataSourceWithConfigure        = &ownerDataSource{}
	_ datasource.DataSourceWithConfigValidators = &ownerDataSource{}
)

// ownerDataSource looks up a CRM owner by email or owner ID
// (GET /crm/v3/owners). Owners are read-only in HubSpot's API; the returned
// `id` (not `user_id`) is what goes into `hubspot_owner_id` record properties.
type ownerDataSource struct {
	client *client.Client
}

// NewOwnerDataSource returns the hubspot_owner data source.
func NewOwnerDataSource() datasource.DataSource {
	return &ownerDataSource{}
}

type ownerDataSourceModel struct {
	OwnerID   types.String `tfsdk:"owner_id"`
	Email     types.String `tfsdk:"email"`
	ID        types.String `tfsdk:"id"`
	FirstName types.String `tfsdk:"first_name"`
	LastName  types.String `tfsdk:"last_name"`
	UserID    types.Int64  `tfsdk:"user_id"`
	Archived  types.Bool   `tfsdk:"archived"`
}

type ownerAPI struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	UserID    int64  `json:"userId"`
	Archived  bool   `json:"archived"`
}

func (d *ownerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_owner"
}

func (d *ownerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a CRM owner by email or owner ID. Owners are derived from users and are " +
			"read-only via the HubSpot API. Requires the `crm.objects.owners.read` scope.",
		Attributes: map[string]schema.Attribute{
			"owner_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The owner's ID. Exactly one of `owner_id` or `email` must be set.",
			},
			"email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The owner's email address. Exactly one of `owner_id` or `email` must be set.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The owner's ID (identical to `owner_id`). This is the value used in " +
					"`hubspot_owner_id` record properties — not `user_id`.",
			},
			"first_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The owner's first name.",
			},
			"last_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The owner's last name.",
			},
			"user_id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The ID of the HubSpot user the owner is derived from.",
			},
			"archived": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the owner is archived (deactivated).",
			},
		},
	}
}

func (d *ownerDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("owner_id"), path.MatchRoot("email")),
	}
}

func (d *ownerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *ownerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ownerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found *ownerAPI

	if !config.OwnerID.IsNull() {
		var got ownerAPI
		err := d.client.Get(ctx, "/crm/v3/owners/"+url.PathEscape(config.OwnerID.ValueString()), nil, &got)
		if err != nil {
			if client.IsNotFound(err) {
				resp.Diagnostics.AddError("No HubSpot owner found",
					fmt.Sprintf("No owner exists with ID %q.", config.OwnerID.ValueString()))
				return
			}
			resp.Diagnostics.AddError("Unable to read HubSpot owner", "HubSpot API request failed: "+err.Error())
			return
		}
		found = &got
	} else {
		q := url.Values{}
		q.Set("email", config.Email.ValueString())
		var page struct {
			Results []ownerAPI `json:"results"`
		}
		if err := d.client.Get(ctx, "/crm/v3/owners", q, &page); err != nil {
			resp.Diagnostics.AddError("Unable to read HubSpot owners", "HubSpot API request failed: "+err.Error())
			return
		}
		switch len(page.Results) {
		case 0:
			resp.Diagnostics.AddError("No HubSpot owner found",
				fmt.Sprintf("No owner exists with email %q.", config.Email.ValueString()))
			return
		case 1:
			found = &page.Results[0]
		default:
			resp.Diagnostics.AddError("Multiple HubSpot owners found",
				fmt.Sprintf("Email %q matched %d owners; expected exactly one.",
					config.Email.ValueString(), len(page.Results)))
			return
		}
	}

	state := ownerDataSourceModel{
		OwnerID:   types.StringValue(found.ID),
		Email:     types.StringValue(found.Email),
		ID:        types.StringValue(found.ID),
		FirstName: types.StringValue(found.FirstName),
		LastName:  types.StringValue(found.LastName),
		UserID:    types.Int64Value(found.UserID),
		Archived:  types.BoolValue(found.Archived),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
