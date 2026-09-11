package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// marketplace_test.go covers the F-523 (CLO-617) marketplace endpoints:
// publish / browse / detail / archive / download / download-count. The suite
// skips itself when no test database is available (see handler_test.go
// TestMain).

// marketplacePublish publishes a listing directly through the handler and
// returns the decoded response.
func marketplacePublish(t *testing.T, kind, resourceID string, template any, metadata map[string]any) (MarketplaceListing, *httptest.ResponseRecorder) {
	t.Helper()
	body := map[string]any{
		"kind":     kind,
		"metadata": metadata,
	}
	if resourceID != "" {
		body["resource_id"] = resourceID
	}
	if template != nil {
		body["template"] = template
	}
	w := httptest.NewRecorder()
	testHandler.PublishMarketplaceListing(w, newRequest("POST", "/api/marketplace/listings?workspace_id="+testWorkspaceID, body))
	var resp MarketplaceListing
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode publish response: %v (body %s)", err, w.Body.String())
		}
	}
	return resp, w
}

// cleanupMarketplaceListing removes a listing (and its stats/dedup rows) after
// a test. Deleting rows directly mirrors the application-layer cleanup rule.
func cleanupMarketplaceListing(t *testing.T, listingID string) {
	t.Helper()
	if listingID == "" {
		return
	}
	testPool.Exec(context.Background(), `DELETE FROM marketplace_download_dedup WHERE listing_id = $1`, listingID)
	testPool.Exec(context.Background(), `DELETE FROM marketplace_stats WHERE listing_id = $1`, listingID)
	testPool.Exec(context.Background(), `DELETE FROM marketplace_listings WHERE id = $1`, listingID)
}

func TestMarketplacePublishAgentFromResource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	name := uniqueName("mkt-agent")
	agentID := createHandlerTestAgent(t, name, nil)

	listing, w := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title":    name + " (market)",
		"summary":  "A marketplace listing",
		"category": "ai-agent",
		"tags":     []string{"demo", "agent"},
		"version":  "1.0.0",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("publish agent: status %d, body %s", w.Code, w.Body.String())
	}
	if listing.ID == "" {
		t.Fatal("publish agent: empty listing id")
	}
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	if listing.Kind != "agent" {
		t.Errorf("kind = %q, want agent", listing.Kind)
	}
	if listing.Downloads != 0 {
		t.Errorf("downloads = %d, want 0", listing.Downloads)
	}
	if listing.Status != "published" {
		t.Errorf("status = %q, want published", listing.Status)
	}
}

func TestMarketplacePublishSquadFromResource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderName := uniqueName("mkt-leader")
	leaderID := createHandlerTestAgent(t, leaderName, nil)
	squadName := uniqueName("mkt-squad")
	squadID := createTestSquad(t, squadName, leaderID, nil)

	listing, w := marketplacePublish(t, "squad", squadID, nil, map[string]any{
		"title":    squadName + " (market)",
		"category": "other",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("publish squad: status %d, body %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	// The stored template must carry the full embedded squad (leader +
	// members), PRD AC-2.
	detW := httptest.NewRecorder()
	testHandler.GetMarketplaceListing(detW, withURLParam(
		newRequest("GET", "/api/marketplace/listings/"+listing.ID+"?workspace_id="+testWorkspaceID, nil),
		"id", listing.ID,
	))
	if detW.Code != http.StatusOK {
		t.Fatalf("detail: status %d, body %s", detW.Code, detW.Body.String())
	}
	var det struct {
		Listing MarketplaceListing `json:"listing"`
	}
	if err := json.NewDecoder(detW.Body).Decode(&det); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	var tmpl map[string]any
	if err := json.Unmarshal(det.Listing.Template, &tmpl); err != nil {
		t.Fatalf("template is not valid JSON: %v", err)
	}
	spec, _ := tmpl["spec"].(map[string]any)
	squadSpec, ok := spec["squad"].(map[string]any)
	if !ok {
		t.Fatalf("template.spec.squad missing: %s", det.Listing.Template)
	}
	if squadSpec["name"] != squadName {
		t.Errorf("squad name = %v, want %s", squadSpec["name"], squadName)
	}
	if leaderRef, _ := squadSpec["leader_ref"].(string); leaderRef == "" {
		t.Errorf("squad template has no leader_ref: %s", det.Listing.Template)
	}
}

func TestMarketplacePublishRejectsSecretTemplate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	name := uniqueName("mkt-secret")
	// Direct-template publish carrying a plaintext credential must be
	// rejected and never persisted (PRD AC-3 / R1).
	tmpl := agentTemplate(name, map[string]any{
		"instructions": "sk-12345678901234567890abc",
	})
	_, w := marketplacePublish(t, "agent", "", tmpl, map[string]any{
		"title": name + " market",
	})
	if w.Code == http.StatusOK {
		t.Fatalf("secret template was accepted: %s", w.Body.String())
	}
}

func TestMarketplacePublishRejectsInvalidCategory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	name := uniqueName("mkt-cat")
	agentID := createHandlerTestAgent(t, name, nil)
	_, w := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title":    name + " market",
		"category": "not-a-real-category",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid category: status %d, body %s", w.Code, w.Body.String())
	}
}

func TestMarketplacePublishDuplicateTitleConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	name := uniqueName("mkt-dup")
	agentID := createHandlerTestAgent(t, name, nil)
	title := name + " (market)"
	listing, w := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": title,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("first publish: status %d, body %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	// Same title in the same workspace 鈫?409.
	agentID2 := createHandlerTestAgent(t, uniqueName("mkt-dup2"), nil)
	_, w2 := marketplacePublish(t, "agent", agentID2, nil, map[string]any{
		"title": title,
	})
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate title: status %d, body %s", w2.Code, w2.Body.String())
	}
}

func TestMarketplaceListSearchAndFilter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// Publish two agent listings + one squad listing with distinct categories.
	a1 := createHandlerTestAgent(t, uniqueName("mkt-l-a1"), nil)
	a2 := createHandlerTestAgent(t, uniqueName("mkt-l-a2"), nil)
	leader := createHandlerTestAgent(t, uniqueName("mkt-l-leader"), nil)
	sq := createTestSquad(t, uniqueName("mkt-l-sq"), leader, nil)

	l1, _ := marketplacePublish(t, "agent", a1, nil, map[string]any{
		"title": "alpha-market-1", "category": "ai-agent", "tags": []string{"searchable-tag"},
	})
	l2, _ := marketplacePublish(t, "agent", a2, nil, map[string]any{
		"title": "beta-market-2", "category": "dev-programming",
	})
	l3, _ := marketplacePublish(t, "squad", sq, nil, map[string]any{
		"title": "gamma-squad-3", "category": "other",
	})
	t.Cleanup(func() {
		cleanupMarketplaceListing(t, l1.ID)
		cleanupMarketplaceListing(t, l2.ID)
		cleanupMarketplaceListing(t, l3.ID)
	})

	listAndCount := func(query string) ([]MarketplaceListing, int64) {
		w := httptest.NewRecorder()
		testHandler.ListMarketplaceListings(w, newRequest("GET", "/api/marketplace/listings?workspace_id="+testWorkspaceID+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: status %d, body %s", query, w.Code, w.Body.String())
		}
		var resp ListMarketplaceListingsResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return resp.Items, resp.Total
	}

	// All published listings in the workspace.
	items, total := listAndCount("")
	if total < 3 {
		t.Errorf("total = %d, want >= 3", total)
	}
	for _, it := range items {
		if it.Template != nil {
			t.Errorf("list item carries template: %s", it.ID)
		}
	}

	// kind filter.
	agentItems, agentTotal := listAndCount("&kind=agent")
	if agentTotal != 2 {
		t.Errorf("kind=agent total = %d, want 2", agentTotal)
	}
	_ = agentItems

	// category filter.
	catItems, catTotal := listAndCount("&category=dev-programming")
	if catTotal != 1 || len(catItems) != 1 || catItems[0].Title != "beta-market-2" {
		t.Errorf("category filter: total=%d items=%v", catTotal, catItems)
	}

	// keyword search (title + tag).
	_, searchTotal := listAndCount("&q=alpha-market")
	if searchTotal != 1 {
		t.Errorf("q=alpha-market total = %d, want 1", searchTotal)
	}
	_, tagTotal := listAndCount("&q=searchable-tag")
	if tagTotal != 1 {
		t.Errorf("q=searchable-tag total = %d, want 1", tagTotal)
	}

	// name sort.
	nameItems, _ := listAndCount("&sort=name")
	if len(nameItems) >= 2 && nameItems[0].Title > nameItems[1].Title {
		t.Errorf("name sort not ascending: %v", nameItems[0].Title)
	}
}

func TestMarketplaceDetailVisibility(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, uniqueName("mkt-detail"), nil)
	listing, _ := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": uniqueName("mkt-detail-title"),
	})
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	// Published listing visible to a regular member with full template.
	memberID := createRegularTestMember(t)
	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "GET", "/api/marketplace/listings/"+listing.ID+"?workspace_id="+testWorkspaceID, nil)
	testHandler.GetMarketplaceListing(w, withURLParam(req, "id", listing.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("member detail: status %d, body %s", w.Code, w.Body.String())
	}
	var det struct {
		Listing MarketplaceListing `json:"listing"`
	}
	if err := json.NewDecoder(w.Body).Decode(&det); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(det.Listing.Template) == 0 {
		t.Error("detail did not return the template")
	}

	// Archive it as the publisher (owner in this fixture), then a regular
	// member must get 404 while the owner still can (PRD AC-9).
	archW := httptest.NewRecorder()
	archReq := newRequest("POST", "/api/marketplace/listings/"+listing.ID+"/archive?workspace_id="+testWorkspaceID, map[string]any{"restore": false})
	testHandler.ArchiveMarketplaceListing(archW, withURLParam(archReq, "id", listing.ID))
	if archW.Code != http.StatusOK {
		t.Fatalf("archive: status %d, body %s", archW.Code, archW.Body.String())
	}

	memberDet := httptest.NewRecorder()
	req2 := newRequestAs(memberID, "GET", "/api/marketplace/listings/"+listing.ID+"?workspace_id="+testWorkspaceID, nil)
	testHandler.GetMarketplaceListing(memberDet, withURLParam(req2, "id", listing.ID))
	if memberDet.Code != http.StatusNotFound {
		t.Errorf("archived listing visible to member: status %d", memberDet.Code)
	}

	ownerDet := httptest.NewRecorder()
	testHandler.GetMarketplaceListing(ownerDet, withURLParam(
		newRequest("GET", "/api/marketplace/listings/"+listing.ID+"?workspace_id="+testWorkspaceID, nil),
		"id", listing.ID,
	))
	if ownerDet.Code != http.StatusOK {
		t.Errorf("archived listing not visible to owner: status %d", ownerDet.Code)
	}
}

func TestMarketplaceArchivePermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// A listing published by the owner must not be archivable by a plain
	// member.
	agentID := createHandlerTestAgent(t, uniqueName("mkt-perm"), nil)
	listing, _ := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": uniqueName("mkt-perm-title"),
	})
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	memberID := createRegularTestMember(t)
	archW := httptest.NewRecorder()
	req := newRequestAs(memberID, "POST", "/api/marketplace/listings/"+listing.ID+"/archive?workspace_id="+testWorkspaceID, map[string]any{"restore": false})
	testHandler.ArchiveMarketplaceListing(archW, withURLParam(req, "id", listing.ID))
	if archW.Code != http.StatusForbidden {
		t.Fatalf("member archive: status %d, body %s", archW.Code, archW.Body.String())
	}
}

func TestMarketplaceDownloadTemplateFile(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, uniqueName("mkt-dl"), nil)
	listing, _ := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": "downloadable market item",
	})
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/marketplace/listings/"+listing.ID+"/download?workspace_id="+testWorkspaceID, nil)
	testHandler.DownloadMarketplaceListingTemplate(w, withURLParam(req, "id", listing.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("download: status %d, body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("content-disposition = %q", cd)
	}
	var tmpl map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tmpl); err != nil {
		t.Fatalf("downloaded body is not valid template JSON: %v", err)
	}
}

func TestMarketplaceDownloadCountDedup(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, uniqueName("mkt-count"), nil)
	listing, _ := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": uniqueName("mkt-count-title"),
	})
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	report := func() int64 {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/marketplace/listings/"+listing.ID+"/downloads?workspace_id="+testWorkspaceID, map[string]any{"count": 1})
		testHandler.ReportMarketplaceDownload(w, withURLParam(req, "id", listing.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("report download: status %d, body %s", w.Code, w.Body.String())
		}
		var resp struct {
			Downloads int64 `json:"downloads"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Downloads
	}

	first := report()
	if first != 1 {
		t.Errorf("first download count = %d, want 1", first)
	}
	second := report()
	if second != 1 {
		t.Errorf("second (same member) count = %d, want 1 (deduped)", second)
	}

	// A different member counts once more (PRD AC-10).
	memberID := createRegularTestMember(t)
	w2 := httptest.NewRecorder()
	req2 := newRequestAs(memberID, "POST", "/api/marketplace/listings/"+listing.ID+"/downloads?workspace_id="+testWorkspaceID, map[string]any{"count": 1})
	testHandler.ReportMarketplaceDownload(w2, withURLParam(req2, "id", listing.ID))
	if w2.Code != http.StatusOK {
		t.Fatalf("member report: status %d, body %s", w2.Code, w2.Body.String())
	}
	var resp struct {
		Downloads int64 `json:"downloads"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode member report: %v", err)
	}
	if resp.Downloads != 2 {
		t.Errorf("after second member count = %d, want 2", resp.Downloads)
	}
}

func TestMarketplaceDownloadCountDedupConcurrent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, uniqueName("mkt-conc"), nil)
	listing, _ := marketplacePublish(t, "agent", agentID, nil, map[string]any{
		"title": uniqueName("mkt-conc-title"),
	})
	t.Cleanup(func() { cleanupMarketplaceListing(t, listing.ID) })

	// N concurrent reports from the SAME member must dedupe to exactly one
	// increment. The ON CONFLICT DO NOTHING path must not poison any
	// transaction (a 23505 abort inside a tx would surface as a 500 here).
	const workers = 8
	var wg sync.WaitGroup
	results := make([]int64, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/marketplace/listings/"+listing.ID+"/downloads?workspace_id="+testWorkspaceID, map[string]any{"count": 1})
			testHandler.ReportMarketplaceDownload(w, withURLParam(req, "id", listing.ID))
			if w.Code != http.StatusOK {
				errs[i] = fmt.Errorf("status %d, body %s", w.Code, w.Body.String())
				return
			}
			var resp struct {
				Downloads int64 `json:"downloads"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				errs[i] = fmt.Errorf("decode: %v", err)
				return
			}
			results[i] = resp.Downloads
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i, got := range results {
		if got != 1 {
			t.Errorf("worker %d count = %d, want 1 (concurrent dedup)", i, got)
		}
	}
	// The persisted counter must be exactly 1.
	var dbCount int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT downloads FROM marketplace_stats WHERE listing_id = $1`, listing.ID).Scan(&dbCount); err != nil {
		t.Fatalf("read stats: %v", err)
	}
	if dbCount != 1 {
		t.Errorf("persisted downloads = %d, want 1", dbCount)
	}
}

func TestMarketplacePublishRejectsMissingResourceAndTemplate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, w := marketplacePublish(t, "agent", "", nil, map[string]any{"title": "no-source"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing source: status %d, body %s", w.Code, w.Body.String())
	}
}

func TestMarketplaceAgentActorCannotPublish(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, uniqueName("mkt-actor"), nil)
	req := newRequest("POST", "/api/marketplace/listings?workspace_id="+testWorkspaceID, map[string]any{
		"kind":        "agent",
		"resource_id": agentID,
		"metadata":    map[string]any{"title": "actor attempt"},
	})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", uuid.NewString())
	w := httptest.NewRecorder()
	testHandler.PublishMarketplaceListing(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent actor publish: status %d, body %s", w.Code, w.Body.String())
	}
}

var _ = fmt.Sprintf
