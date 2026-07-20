// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

// A stateful in-memory fake of the HubSpot Properties v3 API (property
// groups + properties), sufficient to run full Terraform lifecycles
// hermetically. It emulates the HubSpot behaviors the provider must handle:
//   - bearer-token auth (401 without it),
//   - archive-not-delete on properties (DELETE archives; GET 404s unless
//     ?archived=true),
//   - "name purgatory": creating a property whose name matches an archived
//     property fails with 400 until the archived one is purged,
//   - 409 on duplicate create,
//   - server-side normalization: option displayOrder is rewritten to the
//     list index, property displayOrder defaults to -1.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeGroup struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	DisplayOrder int64  `json:"displayOrder"`
	Archived     bool   `json:"archived"`
}

type fakeOption struct {
	Label        string `json:"label"`
	Value        string `json:"value"`
	Description  string `json:"description,omitempty"`
	DisplayOrder int64  `json:"displayOrder"`
	Hidden       bool   `json:"hidden"`
}

type fakeProperty struct {
	Name           string       `json:"name"`
	Label          string       `json:"label"`
	Type           string       `json:"type"`
	FieldType      string       `json:"fieldType"`
	GroupName      string       `json:"groupName"`
	Description    string       `json:"description,omitempty"`
	DisplayOrder   int64        `json:"displayOrder"`
	Hidden         bool         `json:"hidden"`
	FormField      bool         `json:"formField"`
	HasUniqueValue bool         `json:"hasUniqueValue"`
	Options        []fakeOption `json:"options"`
	Archived       bool         `json:"archived"`
	HubspotDefined bool         `json:"hubspotDefined"`
	Calculated     bool         `json:"calculated"`
}

type fakeOwner struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	UserID    int64  `json:"userId"`
	Archived  bool   `json:"archived"`
}

// fakeStage is one stage of a pipeline as stored/returned by the fake.
type fakeStage struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	DisplayOrder int64             `json:"displayOrder"`
	Metadata     map[string]string `json:"metadata"`
}

// fakePipeline is a pipeline as stored/returned by the fake.
type fakePipeline struct {
	ID           string      `json:"id"`
	Label        string      `json:"label"`
	DisplayOrder int64       `json:"displayOrder"`
	Stages       []fakeStage `json:"stages"`
	Default      bool        `json:"-"` // non-deletable default pipeline
}

// fakeStageInput is the wire shape the fake decodes from create/update
// requests: it honors a client-pinned stageId (or id) when present.
type fakeStageInput struct {
	ID           string            `json:"id"`
	StageID      string            `json:"stageId"`
	Label        string            `json:"label"`
	DisplayOrder int64             `json:"displayOrder"`
	Metadata     map[string]string `json:"metadata"`
}

// fakePipelineInput is the wire shape the fake decodes from create/update.
type fakePipelineInput struct {
	Label        string           `json:"label"`
	DisplayOrder int64            `json:"displayOrder"`
	Stages       []fakeStageInput `json:"stages"`
}

type fakeHubSpot struct {
	mu              sync.Mutex
	groups          map[string]map[string]*fakeGroup    // objectType -> name -> group
	properties      map[string]map[string]*fakeProperty // objectType -> name -> property
	pipelines       map[string]map[string]*fakePipeline // objectType -> pipelineId -> pipeline
	owners          []*fakeOwner
	portalID        int64
	pipelineCounter int
	stageCounter    int
}

func newFakeHubSpot(t *testing.T) (*fakeHubSpot, *httptest.Server) {
	t.Helper()
	f := &fakeHubSpot{
		groups:     map[string]map[string]*fakeGroup{},
		properties: map[string]map[string]*fakeProperty{},
		pipelines:  map[string]map[string]*fakePipeline{},
		portalID:   123456,
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// seedOwner registers an owner so the hubspot_owner data source can find it.
func (f *fakeHubSpot) seedOwner(o fakeOwner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := o
	f.owners = append(f.owners, &owner)
}

// deleteProperty simulates out-of-band deletion (for _disappears tests).
func (f *fakeHubSpot) deleteProperty(objectType, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.properties[objectType], name)
}

// deleteGroup simulates out-of-band deletion (for _disappears tests).
func (f *fakeHubSpot) deleteGroup(objectType, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.groups[objectType], name)
}

// deletePipeline simulates out-of-band deletion (for _disappears tests).
func (f *fakeHubSpot) deletePipeline(objectType, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pipelines[objectType], id)
}

func (f *fakeHubSpot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		writeHubSpotError(w, http.StatusUnauthorized, "AUTHENTICATION_FAILED", "missing bearer token")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	// account-info/v3/details — portal identity (data source hubspot_portal).
	if len(parts) == 3 && parts[0] == "account-info" && parts[1] == "v3" && parts[2] == "details" {
		f.accountInfo(w, r)
		return
	}
	// crm/v3/owners[/{ownerId}] — read-only (data source hubspot_owner).
	if len(parts) >= 3 && parts[0] == "crm" && parts[1] == "v3" && parts[2] == "owners" {
		f.ownersRoute(w, r, parts[3:])
		return
	}
	// crm/v3/pipelines/{objectType}[/{pipelineId}] — resource hubspot_pipeline.
	if len(parts) >= 3 && parts[0] == "crm" && parts[1] == "v3" && parts[2] == "pipelines" {
		f.pipelinesRoute(w, r, parts[3:])
		return
	}

	// Expected property shapes:
	//   crm/v3/properties/{objectType}
	//   crm/v3/properties/{objectType}/{propertyName}
	//   crm/v3/properties/{objectType}/groups
	//   crm/v3/properties/{objectType}/groups/{groupName}
	if len(parts) < 4 || parts[0] != "crm" || parts[1] != "v3" || parts[2] != "properties" {
		writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "unknown path "+r.URL.Path)
		return
	}
	objectType := parts[3]
	rest := parts[4:]

	switch {
	case len(rest) == 1 && rest[0] == "groups" && r.Method == http.MethodPost:
		f.createGroup(w, r, objectType)
	case len(rest) == 2 && rest[0] == "groups":
		f.groupByName(w, r, objectType, rest[1])
	case len(rest) == 0 && r.Method == http.MethodPost:
		f.createProperty(w, r, objectType)
	case len(rest) == 1 && rest[0] != "groups":
		f.propertyByName(w, r, objectType, rest[0])
	default:
		writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "unhandled route "+r.Method+" "+r.URL.Path)
	}
}

func (f *fakeHubSpot) createGroup(w http.ResponseWriter, r *http.Request, objectType string) {
	var in fakeGroup
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" || in.Label == "" {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name and label are required")
		return
	}
	if f.groups[objectType] == nil {
		f.groups[objectType] = map[string]*fakeGroup{}
	}
	if _, exists := f.groups[objectType][in.Name]; exists {
		writeHubSpotError(w, http.StatusConflict, "CONFLICT", "property group "+in.Name+" already exists")
		return
	}
	g := in
	f.groups[objectType][in.Name] = &g
	writeJSON(w, http.StatusCreated, g)
}

func (f *fakeHubSpot) groupByName(w http.ResponseWriter, r *http.Request, objectType, name string) {
	g := f.groups[objectType][name]
	switch r.Method {
	case http.MethodGet:
		if g == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "group not found")
			return
		}
		writeJSON(w, http.StatusOK, g)
	case http.MethodPatch:
		if g == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "group not found")
			return
		}
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON")
			return
		}
		if v, ok := patch["label"].(string); ok {
			g.Label = v
		}
		if v, ok := patch["displayOrder"].(float64); ok {
			g.DisplayOrder = int64(v)
		}
		writeJSON(w, http.StatusOK, g)
	case http.MethodDelete:
		if g == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "group not found")
			return
		}
		delete(f.groups[objectType], name)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeHubSpotError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
	}
}

func (f *fakeHubSpot) createProperty(w http.ResponseWriter, r *http.Request, objectType string) {
	var in fakeProperty
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" || in.Label == "" || in.Type == "" || in.FieldType == "" || in.GroupName == "" {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name, label, type, fieldType and groupName are required")
		return
	}
	if f.properties[objectType] == nil {
		f.properties[objectType] = map[string]*fakeProperty{}
	}
	if existing, ok := f.properties[objectType][in.Name]; ok {
		if existing.Archived {
			// Name purgatory: archived property blocks the name until purged.
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"a property with this name was recently archived; the name cannot be reused until it is purged")
			return
		}
		writeHubSpotError(w, http.StatusConflict, "CONFLICT", "property "+in.Name+" already exists")
		return
	}
	p := in
	normalizeProperty(&p)
	f.properties[objectType][in.Name] = &p
	writeJSON(w, http.StatusCreated, p)
}

func (f *fakeHubSpot) propertyByName(w http.ResponseWriter, r *http.Request, objectType, name string) {
	p := f.properties[objectType][name]
	wantArchived := r.URL.Query().Get("archived") == "true"
	switch r.Method {
	case http.MethodGet:
		if p == nil || p.Archived != wantArchived {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "property not found")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch:
		if p == nil || p.Archived {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "property not found")
			return
		}
		var patch fakeProperty
		raw, _ := json.Marshal(p)
		_ = json.Unmarshal(raw, &patch) // start from current values
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON")
			return
		}
		if patch.Name != p.Name || patch.Type != p.Type {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name and type cannot be changed")
			return
		}
		normalizeProperty(&patch)
		*p = patch
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if p == nil || p.Archived {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "property not found")
			return
		}
		p.Archived = true // archive, not delete
		w.WriteHeader(http.StatusNoContent)
	default:
		writeHubSpotError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
	}
}

// normalizeProperty emulates HubSpot server-side normalization.
func normalizeProperty(p *fakeProperty) {
	for i := range p.Options {
		p.Options[i].DisplayOrder = int64(i)
	}
	if p.DisplayOrder == 0 {
		p.DisplayOrder = -1
	}
}

// accountInfo emulates GET /account-info/v3/details.
func (f *fakeHubSpot) accountInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHubSpotError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"portalId":            f.portalID,
		"accountType":         "STANDARD",
		"timeZone":            "US/Eastern",
		"companyCurrency":     "USD",
		"uiDomain":            "app.hubspot.com",
		"dataHostingLocation": "na1",
	})
}

// ownersRoute emulates GET /crm/v3/owners and /crm/v3/owners/{ownerId}.
func (f *fakeHubSpot) ownersRoute(w http.ResponseWriter, r *http.Request, rest []string) {
	if r.Method != http.MethodGet {
		writeHubSpotError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	// GET /crm/v3/owners/{ownerId}
	if len(rest) == 1 {
		for _, o := range f.owners {
			if o.ID == rest[0] {
				writeJSON(w, http.StatusOK, o)
				return
			}
		}
		writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "owner not found")
		return
	}
	// GET /crm/v3/owners?email=
	emailFilter := r.URL.Query().Get("email")
	results := make([]*fakeOwner, 0, len(f.owners))
	for _, o := range f.owners {
		if emailFilter != "" && o.Email != emailFilter {
			continue
		}
		results = append(results, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// pipelinesRoute dispatches /crm/v3/pipelines/{objectType}[/{pipelineId}].
func (f *fakeHubSpot) pipelinesRoute(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 0 {
		writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "missing object type")
		return
	}
	objectType := rest[0]
	switch {
	case len(rest) == 1 && r.Method == http.MethodPost:
		f.createPipeline(w, r, objectType)
	case len(rest) == 2:
		f.pipelineByID(w, r, objectType, rest[1])
	default:
		writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "unhandled route "+r.Method+" "+r.URL.Path)
	}
}

// assignStages converts input stages into stored stages, honoring a
// client-pinned stageId/id and otherwise minting a deterministic "stg_N".
func (f *fakeHubSpot) assignStages(in []fakeStageInput) []fakeStage {
	stages := make([]fakeStage, 0, len(in))
	for _, s := range in {
		id := s.StageID
		if id == "" {
			id = s.ID
		}
		if id == "" {
			f.stageCounter++
			id = fmt.Sprintf("stg_%d", f.stageCounter)
		}
		stages = append(stages, fakeStage{
			ID:           id,
			Label:        s.Label,
			DisplayOrder: s.DisplayOrder,
			Metadata:     s.Metadata,
		})
	}
	return stages
}

func (f *fakeHubSpot) createPipeline(w http.ResponseWriter, r *http.Request, objectType string) {
	var in fakePipelineInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON: "+err.Error())
		return
	}
	if in.Label == "" {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "label is required")
		return
	}
	if len(in.Stages) == 0 {
		writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "at least one stage is required")
		return
	}
	if f.pipelines[objectType] == nil {
		f.pipelines[objectType] = map[string]*fakePipeline{}
	}
	f.pipelineCounter++
	p := &fakePipeline{
		ID:           fmt.Sprintf("pl_%d", f.pipelineCounter),
		Label:        in.Label,
		DisplayOrder: in.DisplayOrder,
		Stages:       f.assignStages(in.Stages),
	}
	f.pipelines[objectType][p.ID] = p
	writeJSON(w, http.StatusCreated, p)
}

func (f *fakeHubSpot) pipelineByID(w http.ResponseWriter, r *http.Request, objectType, id string) {
	p := f.pipelines[objectType][id]
	switch r.Method {
	case http.MethodGet:
		if p == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "pipeline not found")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPut:
		if p == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "pipeline not found")
			return
		}
		var in fakePipelineInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON: "+err.Error())
			return
		}
		if in.Label == "" || len(in.Stages) == 0 {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR", "label and at least one stage are required")
			return
		}
		p.Label = in.Label
		p.DisplayOrder = in.DisplayOrder
		p.Stages = f.assignStages(in.Stages)
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if p == nil {
			writeHubSpotError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "pipeline not found")
			return
		}
		if p.Default {
			writeHubSpotError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"the default pipeline cannot be deleted")
			return
		}
		delete(f.pipelines[objectType], id)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeHubSpotError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeHubSpotError(w http.ResponseWriter, status int, category, message string) {
	writeJSON(w, status, map[string]string{
		"status":        "error",
		"message":       message,
		"category":      category,
		"correlationId": "fake-correlation-id",
	})
}
