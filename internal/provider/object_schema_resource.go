// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/revosai/terraform-provider-hubspot/internal/client"
)

var (
	_ resource.Resource                = &objectSchemaResource{}
	_ resource.ResourceWithConfigure   = &objectSchemaResource{}
	_ resource.ResourceWithImportState = &objectSchemaResource{}
	_ resource.ResourceWithModifyPlan  = &objectSchemaResource{}
)

// objectSchemaResource implements hubspot_object_schema: a custom object
// definition managed via /crm/v3/schemas. Enterprise-tier feature.
type objectSchemaResource struct {
	client *client.Client
}

// NewObjectSchemaResource returns the hubspot_object_schema resource.
func NewObjectSchemaResource() resource.Resource {
	return &objectSchemaResource{}
}

type objectSchemaResourceModel struct {
	ID                         types.String `tfsdk:"id"`
	Name                       types.String `tfsdk:"name"`
	Labels                     types.Object `tfsdk:"labels"`
	PrimaryDisplayProperty     types.String `tfsdk:"primary_display_property"`
	SecondaryDisplayProperties types.Set    `tfsdk:"secondary_display_properties"`
	RequiredProperties         types.Set    `tfsdk:"required_properties"`
	SearchableProperties       types.Set    `tfsdk:"searchable_properties"`
	Description                types.String `tfsdk:"description"`
	Properties                 types.List   `tfsdk:"properties"`
	AssociatedObjects          types.Set    `tfsdk:"associated_objects"`
	ForceDelete                types.Bool   `tfsdk:"force_delete"`
	ObjectTypeID               types.String `tfsdk:"object_type_id"`
	FullyQualifiedName         types.String `tfsdk:"fully_qualified_name"`
}

type schemaLabelsModel struct {
	Singular types.String `tfsdk:"singular"`
	Plural   types.String `tfsdk:"plural"`
}

var schemaLabelsAttrTypes = map[string]attr.Type{
	"singular": types.StringType,
	"plural":   types.StringType,
}

type schemaPropertyModel struct {
	Name      types.String `tfsdk:"name"`
	Label     types.String `tfsdk:"label"`
	Type      types.String `tfsdk:"type"`
	FieldType types.String `tfsdk:"field_type"`
}

// --- wire shapes ---

type objectSchemaWire struct {
	ID                         string                 `json:"id,omitempty"`
	ObjectTypeID               string                 `json:"objectTypeId,omitempty"`
	FullyQualifiedName         string                 `json:"fullyQualifiedName,omitempty"`
	Name                       string                 `json:"name,omitempty"`
	Labels                     objectSchemaLabelsWire `json:"labels"`
	PrimaryDisplayProperty     string                 `json:"primaryDisplayProperty,omitempty"`
	SecondaryDisplayProperties []string               `json:"secondaryDisplayProperties,omitempty"`
	RequiredProperties         []string               `json:"requiredProperties,omitempty"`
	SearchableProperties       []string               `json:"searchableProperties,omitempty"`
	Description                string                 `json:"description,omitempty"`
	Properties                 []schemaPropertyWire   `json:"properties,omitempty"`
	AssociatedObjects          []string               `json:"associatedObjects,omitempty"`
	Archived                   bool                   `json:"archived,omitempty"`
	// UpdatedAt is read-only server metadata (RFC3339); never sent on writes.
	// Reads use it to rank cache generations — see readSchemaConsistent.
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type objectSchemaLabelsWire struct {
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
}

type schemaPropertyWire struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	FieldType string `json:"fieldType"`
}

// objectSchemaPatchWire carries only the fields HubSpot's PATCH accepts.
type objectSchemaPatchWire struct {
	Labels                     *objectSchemaLabelsWire `json:"labels,omitempty"`
	PrimaryDisplayProperty     string                  `json:"primaryDisplayProperty,omitempty"`
	SecondaryDisplayProperties []string                `json:"secondaryDisplayProperties"`
	RequiredProperties         []string                `json:"requiredProperties"`
	SearchableProperties       []string                `json:"searchableProperties"`
	Description                string                  `json:"description"`
}

func (r *objectSchemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_schema"
}

func (r *objectSchemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a custom object schema (`/crm/v3/schemas`). Custom objects are an " +
			"**Enterprise-tier** feature; a `403` on create usually means the portal's product tier lacks " +
			"custom objects. Requires the `crm.schemas.custom.read` and `crm.schemas.custom.write` scopes.\n\n" +
			"**Destroy is data-destroying and gated.** Deleting the schema permanently removes the object type " +
			"and every record of it. The provider refuses to destroy (or replace) the resource unless " +
			"`force_delete = true` is set. Changing `name` forces replacement — set `force_delete = true` and " +
			"expect all records to be lost.\n\n" +
			"**`properties` and `associated_objects` are create-time bootstrap only.** They seed the object at " +
			"creation and are **not** reconciled afterward — edits to them are ignored on update. Manage the " +
			"object's properties over their lifetime with `hubspot_property` (referencing this schema's " +
			"`object_type_id`), and add later associations out of band.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The object type ID (`2-XXXXX`); equal to `object_type_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Internal name of the object (no spaces). **Immutable** — changing it forces " +
					"replacement, which destroys the object type and all its records.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"labels": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "Singular and plural display labels for the object. Mutable.",
				Attributes: map[string]schema.Attribute{
					"singular": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Singular label (e.g. `Car`).",
					},
					"plural": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Plural label (e.g. `Cars`).",
					},
				},
			},
			"primary_display_property": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Internal name of the property used as the object's primary display label. " +
					"Must be one of the bootstrap `properties` at creation. Mutable.",
			},
			"secondary_display_properties": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Internal names of properties shown as secondary display labels. Mutable.",
			},
			"required_properties": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Internal names of properties that must be set on every record. Mutable.",
			},
			"searchable_properties": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Internal names of properties indexed for search. Mutable. HubSpot always " +
					"indexes `primary_display_property` and adds it to this set server-side; the provider absorbs " +
					"that injection, so listing it here is optional. When unset, the attribute is computed from " +
					"the API (the primary display property).",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the object. Mutable.",
			},
			"properties": schema.ListNestedAttribute{
				Required: true,
				MarkdownDescription: "Bootstrap properties created with the object (at least one, and it must " +
					"include `primary_display_property`). **Create-time only** — not reconciled after creation; " +
					"manage properties thereafter with `hubspot_property`.",
				Validators:    []validator.List{listvalidator.SizeAtLeast(1)},
				PlanModifiers: []planmodifier.List{keepStateList{}},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Internal name of the property.",
						},
						"label": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Human-readable label.",
						},
						"type": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Data type (`string`, `number`, `bool`, `enumeration`, `date`, `datetime`).",
						},
						"field_type": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "UI field type (e.g. `text`, `number`, `select`).",
						},
					},
				},
			},
			"associated_objects": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Standard object types (e.g. `CONTACT`, `COMPANY`) to associate with this " +
					"object at creation. **Create-time only** — not reconciled after creation.",
				PlanModifiers: []planmodifier.Set{keepStateSet{}},
			},
			"force_delete": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Must be `true` to allow `terraform destroy` (or a `name`-change replacement) " +
					"to delete this schema. Deleting a schema permanently removes the object type and all its " +
					"records. Defaults to `false` as a safety guard.",
			},
			"object_type_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Portal-specific object type ID (`2-XXXXX`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"fully_qualified_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fully qualified name (`p{portalId}_{name}`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *objectSchemaResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, ok := clientFromProviderData(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected resource Configure type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	r.client = c
}

// ModifyPlan emits a plan-time warning when a name change would destroy records
// (decision #6: data-destroying replaces warn at plan time), and blocks the
// replacement outright unless force_delete is set.
func (r *objectSchemaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create or destroy
	}
	var plan, state objectSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Name.ValueString() == state.Name.ValueString() {
		return
	}
	resp.Diagnostics.AddWarning(
		"Custom object replacement destroys all records",
		fmt.Sprintf("Changing name from %q to %q replaces the custom object schema: HubSpot deletes the "+
			"existing object type and every record of it, then creates a new one. This is irreversible.",
			state.Name.ValueString(), plan.Name.ValueString()),
	)
	if !plan.ForceDelete.ValueBool() {
		resp.Diagnostics.AddError(
			"Refusing to replace custom object without force_delete",
			"Changing `name` requires deleting the existing schema and all its records. Set "+
				"`force_delete = true` to allow this destructive replacement.",
		)
	}
}

func (r *objectSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := expandSchemaCreate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var out objectSchemaWire
	if err := r.client.Post(ctx, "crm/v3/schemas", body, &out); err != nil {
		resp.Diagnostics.AddError("Unable to create HubSpot object schema",
			fmt.Sprintf("POST /crm/v3/schemas failed: %s\n\nCustom objects require an Enterprise-tier "+
				"portal and the crm.schemas.custom.write scope.", err))
		return
	}

	resp.Diagnostics.Append(flattenSchema(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The 201 echoes primaryDisplayProperty/requiredProperties, but HubSpot
	// applies them to the stored schema asynchronously (a few seconds after
	// create — observed live). Wait until at least one read reflects the
	// created state so the refresh that follows apply can find a consistent
	// read; state is already plan-consistent either way.
	r.awaitSchemaVisible(ctx, plan)

	resp.Diagnostics.Append(stampSchemaLastWrite(ctx, resp.Private)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// privateStateSetter is the slice of resource private state Create/Update
// responses expose for stamping the last-write time.
type privateStateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// stampSchemaLastWrite records "now" in private state so Read can tell a
// stale cache generation moments after our own write apart from genuine
// out-of-band drift.
func stampSchemaLastWrite(ctx context.Context, private privateStateSetter) diag.Diagnostics {
	ts, err := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil // cannot happen for a string
	}
	return private.SetKey(ctx, schemaLastWriteKey, ts)
}

func (r *objectSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	recentWrite := false
	if raw, d := req.Private.GetKey(ctx, schemaLastWriteKey); d == nil || !d.HasError() {
		var ts string
		if raw != nil && json.Unmarshal(raw, &ts) == nil {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				recentWrite = time.Since(t) < schemaWriteGracePeriod
			}
		}
	}

	out, verdict, err := r.readSchemaConsistent(ctx, &state, recentWrite)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read HubSpot object schema",
			fmt.Sprintf("GET crm/v3/schemas/%s failed: %s", state.ObjectTypeID.ValueString(), err))
		return
	}
	switch verdict {
	case schemaReadGone:
		// A missing or soft-deleted (archived) schema is effectively gone
		// from Terraform's perspective — treat it as removed so a
		// destroy/re-create converges.
		resp.State.RemoveResource(ctx)
		return
	case schemaReadChurn:
		// Reads are flip-flopping between cache generations; none of them is
		// trustworthy. Keep the state Terraform last wrote.
		return
	}

	resp.Diagnostics.Append(flattenSchema(ctx, out, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// HubSpot's GET /crm/v3/schemas/{id} is served from a load-balanced cache
// whose nodes hold different generations of the schema: reads observed
// against the live API flip-flop between the current schema and stale
// snapshots (often the just-created one, with primaryDisplayProperty
// defaulted to hs_object_id and no required properties) for minutes after a
// successful write — and stale generations carry a *bumped* updatedAt, so
// recency metadata cannot rank them. Trusting a single read makes every
// refresh a coin toss that reports spurious drift — decision #9 (semantic,
// never raw, equality) extends to reads here.
const (
	// schemaReadTimeout bounds how long a refresh keeps sampling reads while
	// deciding between agreement, churn, and unanimous drift.
	schemaReadTimeout = 30 * time.Second
	// schemaReadInterval spaces the samples.
	schemaReadInterval = time.Second
	// schemaUnanimousReads is how many consecutive identical samples promote
	// a disagreeing read to genuine out-of-band drift (or, on import, to the
	// imported value).
	schemaUnanimousReads = 5
	// schemaWriteVisibleTimeout bounds how long Create/Update wait for their
	// write to become visible to at least one read.
	schemaWriteVisibleTimeout = 90 * time.Second
	// schemaWriteGracePeriod is how long after our own write (stamped in
	// resource private state) a disagreeing read is presumed to be cache lag
	// rather than out-of-band drift. Stale generations were observed
	// circulating for over a minute after a write.
	schemaWriteGracePeriod = 10 * time.Minute
	// schemaLastWriteKey is the private-state key holding the RFC3339
	// timestamp of the provider's last write to this schema.
	schemaLastWriteKey = "schema_last_write"
)

// schemaReadVerdict is readSchemaConsistent's outcome.
type schemaReadVerdict int

const (
	schemaReadFresh schemaReadVerdict = iota // returned wire is authoritative
	schemaReadGone                           // schema not found or archived
	schemaReadChurn                          // reads flip-flopping; keep state
)

// readSchemaConsistent GETs the schema, absorbing stale cache generations:
//
//   - a read that agrees with state's mutable surface is authoritative
//     (fresh node, no drift) and returned immediately;
//   - within schemaWriteGracePeriod of our own write (recentWrite), any
//     disagreeing read is presumed to be a stale cache generation — the
//     write was already confirmed by its echo and awaitSchemaVisible — so
//     the caller keeps state untouched (schemaReadChurn);
//   - past the grace period, two samples that disagree with *each other*
//     prove cache churn (a real out-of-band change settles on one value, it
//     doesn't flip-flop) and state is kept; schemaUnanimousReads consecutive
//     identical samples are genuine out-of-band drift and are returned;
//   - with no prior state to agree with (import), sampling continues until a
//     unanimous run appears (last sample on timeout, best effort);
//   - schemaReadGone means not found or archived. A single gone read is
//     trusted: schemas are only ever archived/purged deliberately, whereas a
//     stale live read moments after a purge is routine — gone outranks live.
func (r *objectSchemaResource) readSchemaConsistent(ctx context.Context, state *objectSchemaResourceModel, recentWrite bool) (objectSchemaWire, schemaReadVerdict, error) {
	p := "crm/v3/schemas/" + url.PathEscape(state.ObjectTypeID.ValueString())
	imported := state.PrimaryDisplayProperty.IsNull() && state.Labels.IsNull()

	var prev objectSchemaWire
	unanimous := 0
	deadline := time.Now().Add(schemaReadTimeout)
	for {
		var out objectSchemaWire
		if err := r.client.Get(ctx, p, nil, &out); err != nil {
			if client.IsNotFound(err) {
				return objectSchemaWire{}, schemaReadGone, nil
			}
			return objectSchemaWire{}, schemaReadFresh, err
		}
		if out.Archived {
			return objectSchemaWire{}, schemaReadGone, nil
		}
		if !imported {
			if r.schemaReadAgreesWithState(ctx, out, *state) {
				return out, schemaReadFresh, nil
			}
			if recentWrite {
				tflog.Info(ctx, "schema read disagrees moments after our own write; keeping state",
					map[string]any{"path": p})
				return objectSchemaWire{}, schemaReadChurn, nil
			}
		}
		switch {
		case unanimous == 0 || sameSchemaSurface(prev, out):
			unanimous++
		case imported:
			unanimous = 1 // churn: restart the unanimity run on the new value
		default:
			tflog.Warn(ctx, "schema reads flip-flopping between cache generations; keeping state",
				map[string]any{"path": p})
			return objectSchemaWire{}, schemaReadChurn, nil
		}
		prev = out
		if unanimous >= schemaUnanimousReads {
			return out, schemaReadFresh, nil
		}
		// On deadline or cancellation, degrade to the last sample rather
		// than failing: an import is best-effort against a churning cache.
		if time.Now().After(deadline) || ctx.Err() != nil {
			tflog.Warn(ctx, "schema reads never became unanimous; using last sample",
				map[string]any{"path": p})
			return out, schemaReadFresh, nil //nolint:nilerr // degrade, don't fail
		}
		select {
		case <-ctx.Done():
			return out, schemaReadFresh, nil
		case <-time.After(schemaReadInterval):
		}
	}
}

// sameSchemaSurface reports whether two reads carry the same mutable schema
// surface — i.e. they are the same cache generation.
func sameSchemaSurface(a, b objectSchemaWire) bool {
	return a.Labels == b.Labels &&
		a.PrimaryDisplayProperty == b.PrimaryDisplayProperty &&
		a.Description == b.Description &&
		sameStringSet(a.SecondaryDisplayProperties, b.SecondaryDisplayProperties) &&
		sameStringSet(a.RequiredProperties, b.RequiredProperties) &&
		sameStringSet(a.SearchableProperties, b.SearchableProperties)
}

// sameStringSet compares two string slices as unordered sets.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := slices.Clone(a), slices.Clone(b)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

// schemaReadAgreesWithState reports whether flattening api over state leaves
// the mutable surface unchanged — i.e. the read reflects the state Terraform
// last wrote, so it is not a stale cache generation.
func (r *objectSchemaResource) schemaReadAgreesWithState(ctx context.Context, api objectSchemaWire, state objectSchemaResourceModel) bool {
	candidate := state
	if d := flattenSchema(ctx, api, &candidate); d.HasError() {
		return false
	}
	return candidate.Labels.Equal(state.Labels) &&
		candidate.PrimaryDisplayProperty.Equal(state.PrimaryDisplayProperty) &&
		candidate.SecondaryDisplayProperties.Equal(state.SecondaryDisplayProperties) &&
		candidate.RequiredProperties.Equal(state.RequiredProperties) &&
		candidate.SearchableProperties.Equal(state.SearchableProperties) &&
		candidate.Description.Equal(state.Description)
}

// awaitSchemaVisible polls GET until at least one read reflects the schema
// state just written (HubSpot applies create-time display/required metadata
// asynchronously). Best-effort: on timeout the caller proceeds with the
// write echo and readSchemaConsistent absorbs the lag on later refreshes.
func (r *objectSchemaResource) awaitSchemaVisible(ctx context.Context, written objectSchemaResourceModel) {
	p := "crm/v3/schemas/" + url.PathEscape(written.ObjectTypeID.ValueString())
	deadline := time.Now().Add(schemaWriteVisibleTimeout)
	for {
		var out objectSchemaWire
		if err := r.client.Get(ctx, p, nil, &out); err == nil &&
			r.schemaReadAgreesWithState(ctx, out, written) {
			return
		}
		if time.Now().After(deadline) {
			tflog.Warn(ctx, "schema write not yet visible to reads; proceeding with write echo",
				map[string]any{"path": p})
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(schemaReadInterval):
		}
	}
}

func (r *objectSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan objectSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := expandSchemaPatch(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := "crm/v3/schemas/" + url.PathEscape(plan.ObjectTypeID.ValueString())
	var out objectSchemaWire
	if err := r.client.Patch(ctx, p, body, &out); err != nil {
		resp.Diagnostics.AddError("Unable to update HubSpot object schema",
			fmt.Sprintf("PATCH %s failed: %s", p, err))
		return
	}

	resp.Diagnostics.Append(flattenSchema(ctx, out, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// PATCHes propagate to reads with the same lag as creates (the new value
	// was observed live to be absent from every read for 8+ seconds). Wait
	// for one consistent read so follow-up refreshes and imports see it.
	r.awaitSchemaVisible(ctx, plan)

	resp.Diagnostics.Append(stampSchemaLastWrite(ctx, resp.Private)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !state.ForceDelete.ValueBool() {
		resp.Diagnostics.AddError(
			"Refusing to delete custom object without force_delete",
			fmt.Sprintf("Deleting object schema %q permanently removes the object type and every record of "+
				"it. Set `force_delete = true` on the resource and apply before destroying, or run "+
				"`terraform state rm` to stop managing it without deleting anything in HubSpot.",
				state.Name.ValueString()),
		)
		return
	}

	base := "crm/v3/schemas/" + url.PathEscape(state.ObjectTypeID.ValueString())
	// Phase 1: soft delete (archive). HubSpot rejects this if records still
	// exist, so surface that actionably.
	if err := r.client.Delete(ctx, base, nil); err != nil {
		if client.IsNotFound(err) {
			return // already gone
		}
		resp.Diagnostics.AddError("Unable to delete HubSpot object schema",
			fmt.Sprintf("DELETE %s failed: %s\n\nIf HubSpot reports that records still exist, delete all "+
				"records of this object type first (the provider does not manage records).", base, err))
		return
	}
	// Phase 2: purge the archived schema so its name is freed and a re-create
	// with the same name succeeds.
	purge := url.Values{}
	purge.Set("archived", "true")
	if err := r.client.Delete(ctx, base, purge); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to purge archived HubSpot object schema",
			fmt.Sprintf("DELETE %s?archived=true failed: %s", base, err))
		return
	}
}

func (r *objectSchemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		resp.Diagnostics.AddError("Invalid hubspot_object_schema import ID",
			"Expected the object type ID (e.g. \"2-12345\"), the fully qualified name, or the object name.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_type_id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// --- expand / flatten ---

func expandSchemaCreate(ctx context.Context, m objectSchemaResourceModel) (objectSchemaWire, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := objectSchemaWire{
		Name:                   m.Name.ValueString(),
		PrimaryDisplayProperty: m.PrimaryDisplayProperty.ValueString(),
		Description:            m.Description.ValueString(),
	}

	labels, d := labelsToWire(ctx, m.Labels)
	diags.Append(d...)
	body.Labels = labels

	body.SecondaryDisplayProperties, d = setToStrings(ctx, m.SecondaryDisplayProperties)
	diags.Append(d...)
	body.RequiredProperties, d = setToStrings(ctx, m.RequiredProperties)
	diags.Append(d...)
	body.SearchableProperties, d = setToStrings(ctx, m.SearchableProperties)
	diags.Append(d...)
	body.AssociatedObjects, d = setToStrings(ctx, m.AssociatedObjects)
	diags.Append(d...)

	var props []schemaPropertyModel
	diags.Append(m.Properties.ElementsAs(ctx, &props, false)...)
	for _, p := range props {
		body.Properties = append(body.Properties, schemaPropertyWire{
			Name:      p.Name.ValueString(),
			Label:     p.Label.ValueString(),
			Type:      p.Type.ValueString(),
			FieldType: p.FieldType.ValueString(),
		})
	}
	return body, diags
}

func expandSchemaPatch(ctx context.Context, m objectSchemaResourceModel) (objectSchemaPatchWire, diag.Diagnostics) {
	var diags diag.Diagnostics
	labels, d := labelsToWire(ctx, m.Labels)
	diags.Append(d...)

	body := objectSchemaPatchWire{
		Labels:                 &labels,
		PrimaryDisplayProperty: m.PrimaryDisplayProperty.ValueString(),
		Description:            m.Description.ValueString(),
	}
	body.SecondaryDisplayProperties, d = setToStrings(ctx, m.SecondaryDisplayProperties)
	diags.Append(d...)
	body.RequiredProperties, d = setToStrings(ctx, m.RequiredProperties)
	diags.Append(d...)
	body.SearchableProperties, d = setToStrings(ctx, m.SearchableProperties)
	diags.Append(d...)
	return body, diags
}

// flattenSchema writes server-owned fields over the model, leaving create-time
// bootstrap fields (properties, associated_objects), name, and force_delete
// untouched (they are managed by config/plan, not the API response).
func flattenSchema(ctx context.Context, api objectSchemaWire, m *objectSchemaResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ObjectTypeID = types.StringValue(api.ObjectTypeID)
	m.ID = types.StringValue(api.ObjectTypeID)
	m.FullyQualifiedName = types.StringValue(api.FullyQualifiedName)
	if api.Name != "" {
		m.Name = types.StringValue(api.Name)
	}

	labels, d := types.ObjectValue(schemaLabelsAttrTypes, map[string]attr.Value{
		"singular": types.StringValue(api.Labels.Singular),
		"plural":   types.StringValue(api.Labels.Plural),
	})
	diags.Append(d...)
	m.Labels = labels

	m.PrimaryDisplayProperty = types.StringValue(api.PrimaryDisplayProperty)

	m.SecondaryDisplayProperties, d = stringsToSet(ctx, api.SecondaryDisplayProperties)
	diags.Append(d...)
	m.RequiredProperties, d = stringsToSet(ctx, api.RequiredProperties)
	diags.Append(d...)
	m.SearchableProperties, d = flattenSearchableProperties(ctx, api, m.SearchableProperties)
	diags.Append(d...)

	if api.Description == "" {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(api.Description)
	}
	return diags
}

// flattenSearchableProperties absorbs HubSpot's server-side injection of the
// primary display property into searchableProperties (it is always indexed
// for search) — decision #9: semantic, never raw, equality. When the prior
// value (plan on create/update, state on read) is a known set that does not
// list the primary display property, the injected entry is stripped so the
// attribute round-trips the practitioner's value without an inconsistent
// apply result or a perpetual diff. A null/unknown prior (unset config on
// create, import) means the attribute is computed — keep the API truth.
func flattenSearchableProperties(ctx context.Context, api objectSchemaWire, prior types.Set) (types.Set, diag.Diagnostics) {
	if prior.IsNull() || prior.IsUnknown() {
		return stringsToSet(ctx, api.SearchableProperties)
	}
	var priorVals []string
	diags := prior.ElementsAs(ctx, &priorVals, false)
	if diags.HasError() {
		return prior, diags
	}
	searchable := api.SearchableProperties
	if !slices.Contains(priorVals, api.PrimaryDisplayProperty) {
		searchable = slices.DeleteFunc(slices.Clone(searchable), func(s string) bool {
			return s == api.PrimaryDisplayProperty
		})
	}
	// Preserve an explicitly configured empty set: stringsToSet would map it
	// to null and re-break plan/apply consistency.
	if len(searchable) == 0 {
		return types.SetValueMust(types.StringType, nil), diags
	}
	out, d := types.SetValueFrom(ctx, types.StringType, searchable)
	diags.Append(d...)
	return out, diags
}

func labelsToWire(ctx context.Context, o types.Object) (objectSchemaLabelsWire, diag.Diagnostics) {
	var lm schemaLabelsModel
	diags := o.As(ctx, &lm, basetypes.ObjectAsOptions{})
	return objectSchemaLabelsWire{Singular: lm.Singular.ValueString(), Plural: lm.Plural.ValueString()}, diags
}

// setToStrings converts an optional string set into a slice, returning nil for
// a null/unknown set so omitempty drops it from create bodies.
func setToStrings(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	var out []string
	diags := s.ElementsAs(ctx, &out, false)
	return out, diags
}

// stringsToSet converts a server slice back into a set, mapping empty to null
// so an omitted optional attribute round-trips without a perpetual diff.
func stringsToSet(ctx context.Context, in []string) (types.Set, diag.Diagnostics) {
	if len(in) == 0 {
		return types.SetNull(types.StringType), nil
	}
	return types.SetValueFrom(ctx, types.StringType, in)
}
