// Copyright (c) terraform-provider-hubspot authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

// TEMPORARY diagnostic probe for the /crm/v3/schemas eventual-consistency
// failure in TestAccReal_objectSchemaLifecycle. It exercises the raw API
// directly (no Terraform) and logs exactly what POST/GET/PATCH/DELETE return
// over time, so the fix can be based on observed behavior instead of guesses.
// DELETE THIS FILE once the object-schema fix is validated.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

type probeSchema struct {
	ObjectTypeID           string   `json:"objectTypeId"`
	Name                   string   `json:"name"`
	PrimaryDisplayProperty string   `json:"primaryDisplayProperty"`
	RequiredProperties     []string `json:"requiredProperties"`
	SearchableProperties   []string `json:"searchableProperties"`
	Archived               bool     `json:"archived"`
	UpdatedAt              string   `json:"updatedAt"`
	Labels                 struct {
		Singular string `json:"singular"`
		Plural   string `json:"plural"`
	} `json:"labels"`
}

func probeDo(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	token := os.Getenv("HUBSPOT_ACCESS_TOKEN")
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s: %v", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, realAPIBase+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := realHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

func probeSummarize(raw []byte) string {
	var s probeSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Sprintf("unparseable: %.300s", raw)
	}
	return fmt.Sprintf("id=%s primaryDisplay=%q required=%v searchable=%v archived=%t labels=%s/%s updatedAt=%s",
		s.ObjectTypeID, s.PrimaryDisplayProperty, s.RequiredProperties, s.SearchableProperties,
		s.Archived, s.Labels.Singular, s.Labels.Plural, s.UpdatedAt)
}

// probeGetSeries GETs the schema at increasing offsets and logs each read.
func probeGetSeries(t *testing.T, label, objectTypeID string, offsets []time.Duration) {
	t.Helper()
	start := time.Now()
	for _, off := range offsets {
		if d := time.Until(start.Add(off)); d > 0 {
			time.Sleep(d)
		}
		status, raw := probeDo(t, http.MethodGet, "/crm/v3/schemas/"+objectTypeID, nil)
		if status == http.StatusOK {
			t.Logf("PROBE %s GET +%v: %d %s", label, off, status, probeSummarize(raw))
		} else {
			t.Logf("PROBE %s GET +%v: %d %.300s", label, off, status, raw)
		}
	}
}

// probeListSeries GETs the schemas LIST endpoint at increasing offsets and
// logs our schema's entry — to learn whether the list is served from the
// same stale cache as GET-by-id or is a consistent alternative read path.
func probeListSeries(t *testing.T, label, objectTypeID string, offsets []time.Duration) {
	t.Helper()
	start := time.Now()
	for _, off := range offsets {
		if d := time.Until(start.Add(off)); d > 0 {
			time.Sleep(d)
		}
		status, raw := probeDo(t, http.MethodGet, "/crm/v3/schemas", nil)
		if status != http.StatusOK {
			t.Logf("PROBE %s LIST +%v: %d %.300s", label, off, status, raw)
			continue
		}
		var envelope struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Logf("PROBE %s LIST +%v: unparseable envelope: %v", label, off, err)
			continue
		}
		found := false
		for _, res := range envelope.Results {
			var s probeSchema
			if json.Unmarshal(res, &s) == nil && s.ObjectTypeID == objectTypeID {
				t.Logf("PROBE %s LIST +%v: 200 %s", label, off, probeSummarize(res))
				found = true
				break
			}
		}
		if !found {
			t.Logf("PROBE %s LIST +%v: 200 schema %s ABSENT from list (%d results)",
				label, off, objectTypeID, len(envelope.Results))
		}
	}
}

// TestAccReal_schemaProbe documents live /crm/v3/schemas behavior:
//
//  1. create with primaryDisplayProperty+requiredProperties set, then watch
//     GET converge (or not) over 60s;
//  2. PATCH the same values in after the schema has settled, watch again;
//  3. archive + purge, watch the read-back.
func TestAccReal_schemaProbe(t *testing.T) {
	requireRealPortal(t)

	name := randomRealName("probe_")
	createBody := map[string]any{
		"name":                   name,
		"labels":                 map[string]string{"singular": "TF Probe", "plural": "TF Probes"},
		"primaryDisplayProperty": "acc_name",
		"requiredProperties":     []string{"acc_name"},
		"properties": []map[string]string{
			{"name": "acc_name", "label": "Name", "type": "string", "fieldType": "text"},
		},
	}

	status, raw := probeDo(t, http.MethodPost, "/crm/v3/schemas", createBody)
	t.Logf("PROBE create POST: %d %s", status, probeSummarize(raw))
	t.Logf("PROBE create POST raw (first 2000): %.2000s", raw)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create failed: %d %s", status, raw)
	}
	var created probeSchema
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	id := created.ObjectTypeID
	defer func() {
		// Best-effort cleanup: archive then purge.
		st1, _ := probeDo(t, http.MethodDelete, "/crm/v3/schemas/"+id, nil)
		st2, _ := probeDo(t, http.MethodDelete, "/crm/v3/schemas/"+id+"?archived=true", nil)
		t.Logf("PROBE cleanup: archive=%d purge=%d", st1, st2)
	}()

	probeGetSeries(t, "post-create", id, []time.Duration{
		0, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second,
	})
	probeListSeries(t, "post-create", id, []time.Duration{0, 2 * time.Second, 5 * time.Second})

	// PATCH the same values in now that the schema has had 60s to settle.
	patchBody := map[string]any{
		"primaryDisplayProperty": "acc_name",
		"requiredProperties":     []string{"acc_name"},
	}
	status, raw = probeDo(t, http.MethodPatch, "/crm/v3/schemas/"+id, patchBody)
	t.Logf("PROBE settle PATCH: %d %s", status, probeSummarize(raw))
	t.Logf("PROBE settle PATCH raw (first 2000): %.2000s", raw)

	probeGetSeries(t, "post-patch", id, []time.Duration{
		0, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second,
	})
	probeListSeries(t, "post-patch", id, []time.Duration{0, 2 * time.Second, 5 * time.Second})

	// A label-only PATCH (what lifecycle step 3 does) for comparison.
	status, raw = probeDo(t, http.MethodPatch, "/crm/v3/schemas/"+id, map[string]any{
		"labels": map[string]string{"singular": "TF Probe", "plural": "TF Probes v2"},
	})
	t.Logf("PROBE label PATCH: %d %s", status, probeSummarize(raw))
	probeGetSeries(t, "post-label-patch", id, []time.Duration{
		0, time.Second, 3 * time.Second, 8 * time.Second,
	})

	// Archive, watch, purge, watch.
	status, raw = probeDo(t, http.MethodDelete, "/crm/v3/schemas/"+id, nil)
	t.Logf("PROBE archive DELETE: %d %.300s", status, raw)
	probeGetSeries(t, "post-archive", id, []time.Duration{
		0, time.Second, 3 * time.Second, 8 * time.Second, 15 * time.Second,
	})
	status, raw = probeDo(t, http.MethodDelete, "/crm/v3/schemas/"+id+"?archived=true", nil)
	t.Logf("PROBE purge DELETE: %d %.300s", status, raw)
	probeGetSeries(t, "post-purge", id, []time.Duration{
		0, time.Second, 3 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second,
	})
}
