// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ datasource.DataSource              = &portalDataSource{}
	_ datasource.DataSourceWithConfigure = &portalDataSource{}
)

// portalDataSource exposes the authenticated portal's identity and defaults
// (GET /account-info/v3/details). Useful for constructing fully-qualified
// names and asserting which portal a run targets.
type portalDataSource struct {
	client *client.Client
}

// NewPortalDataSource returns the hubspot_portal data source.
func NewPortalDataSource() datasource.DataSource {
	return &portalDataSource{}
}

type portalDataSourceModel struct {
	ID                  types.String `tfsdk:"id"`
	PortalID            types.String `tfsdk:"portal_id"`
	AccountType         types.String `tfsdk:"account_type"`
	TimeZone            types.String `tfsdk:"time_zone"`
	CompanyCurrency     types.String `tfsdk:"company_currency"`
	UIDomain            types.String `tfsdk:"ui_domain"`
	DataHostingLocation types.String `tfsdk:"data_hosting_location"`
}

type accountInfoAPI struct {
	PortalID            int64  `json:"portalId"`
	AccountType         string `json:"accountType"`
	TimeZone            string `json:"timeZone"`
	CompanyCurrency     string `json:"companyCurrency"`
	UIDomain            string `json:"uiDomain"`
	DataHostingLocation string `json:"dataHostingLocation"`
}

func (d *portalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_portal"
}

func (d *portalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Information about the HubSpot portal (account) the provider is authenticated " +
			"against. Read-only; takes no arguments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The portal (hub) ID as a string; identical to `portal_id`.",
			},
			"portal_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The numeric portal (hub) ID.",
			},
			"account_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Account type, e.g. `STANDARD`, `DEVELOPER_TEST`, `SANDBOX`.",
			},
			"time_zone": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The portal's configured time zone.",
			},
			"company_currency": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The portal's default company currency (ISO 4217 code).",
			},
			"ui_domain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The HubSpot UI domain for the portal (e.g. `app.hubspot.com`).",
			},
			"data_hosting_location": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The data residency region for the portal (e.g. `na1`, `eu1`).",
			},
		},
	}
}

func (d *portalDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source Configure type",
			fmt.Sprintf("Expected *client.Client, got: %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *portalDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var info accountInfoAPI
	if err := d.client.Get(ctx, "/account-info/v3/details", nil, &info); err != nil {
		resp.Diagnostics.AddError("Unable to read HubSpot account info",
			"HubSpot API request failed: "+err.Error())
		return
	}

	portalID := fmt.Sprintf("%d", info.PortalID)
	state := portalDataSourceModel{
		ID:                  types.StringValue(portalID),
		PortalID:            types.StringValue(portalID),
		AccountType:         types.StringValue(info.AccountType),
		TimeZone:            types.StringValue(info.TimeZone),
		CompanyCurrency:     types.StringValue(info.CompanyCurrency),
		UIDomain:            types.StringValue(info.UIDomain),
		DataHostingLocation: types.StringValue(info.DataHostingLocation),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
