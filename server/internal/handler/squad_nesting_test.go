package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createSquadIncludingAs creates a squad through the handler as the given
// user with the included_squad_ids payload and returns the decoded response.
// Registers cleanup for the squad + its members.
func createSquadIncludingAs(t *testing.T, userID, name, leaderID string, included []string) SquadResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := squadScopeReq(userID, "POST", "/api/squads", map[string]any{
		"name":               name,
		"leader_id":          leaderID,
		"included_squad_ids": included,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
	return resp
}

// TestCreateSquad_IncludedSquadIDs_AttachesChildren locks the v1 nesting
// happy path: creating a squad with included_squad_ids nests each included
// squad under the new one in the same request, and the create response plus
// the child's own detail both expose the parent/child relationship.
func TestCreateSquad_IncludedSquadIDs_AttachesChildren(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "nest-parent-leader", nil)

	child1 := createSquadAs(t, "", "Nest Child One", leaderID)
	child2 := createSquadAs(t, "", "Nest Child Two", leaderID)

	parent := createSquadIncludingAs(t, "", "Nest Parent", leaderID,
		[]string{child1.ID, child2.ID})

	// Create response carries child_squads with id/name/member_count.
	if len(parent.ChildSquads) != 2 {
		t.Fatalf("expected 2 child squads in create response, got %d: %+v", len(parent.ChildSquads), parent.ChildSquads)
	}
	gotChildIDs := map[string]string{}
	for _, c := range parent.ChildSquads {
		gotChildIDs[c.ID] = c.Name
	}
	if gotChildIDs[child1.ID] != "Nest Child One" || gotChildIDs[child2.ID] != "Nest Child Two" {
		t.Fatalf("unexpected child_squads: %+v", parent.ChildSquads)
	}

	// Parent row has no parent; children point back at it.
	ctx := context.Background()
	parentRow, err := testHandler.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          util.MustParseUUID(parent.ID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil || parentRow.ParentSquadID.Valid {
		t.Fatalf("parent squad should be top-level, got err=%v row=%+v", err, parentRow)
	}
	for _, childID := range []string{child1.ID, child2.ID} {
		childRow, err := testHandler.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          util.MustParseUUID(childID),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
		})
		if err != nil {
			t.Fatalf("load child %s: %v", childID, err)
		}
		if !childRow.ParentSquadID.Valid || util.UUIDToString(childRow.ParentSquadID) != parent.ID {
			t.Fatalf("child %s parent_squad_id = %v, want %s", childID, childRow.ParentSquadID, parent.ID)
		}
	}

	// GET squad (detail) also returns child_squads and the child's
	// parent_squad_id.
	w := httptest.NewRecorder()
	testHandler.GetSquad(w, squadScopeReq("", "GET", "/api/squads", nil, map[string]string{"id": parent.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("GetSquad(parent): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var parentResp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&parentResp); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	if len(parentResp.ChildSquads) != 2 {
		t.Fatalf("expected 2 child squads in GET response, got %d", len(parentResp.ChildSquads))
	}

	w = httptest.NewRecorder()
	testHandler.GetSquad(w, squadScopeReq("", "GET", "/api/squads", nil, map[string]string{"id": child1.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("GetSquad(child): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var childResp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&childResp); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if childResp.ParentSquadID == nil || *childResp.ParentSquadID != parent.ID {
		t.Fatalf("child parent_squad_id = %v, want %s", childResp.ParentSquadID, parent.ID)
	}

	// GET /api/squads (list) also carries child_squads + parent_squad_id.
	w = httptest.NewRecorder()
	testHandler.ListSquads(w, squadScopeReq("", "GET", "/api/squads", nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListSquads: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp []SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var parentInList, childInList bool
	for _, s := range listResp {
		if s.ID == parent.ID {
			parentInList = true
			if len(s.ChildSquads) != 2 {
				t.Fatalf("parent in list should carry 2 child squads, got %d", len(s.ChildSquads))
			}
		}
		if s.ID == child1.ID {
			childInList = true
			if s.ParentSquadID == nil || *s.ParentSquadID != parent.ID {
				t.Fatalf("child in list should carry parent_squad_id=%s, got %v", parent.ID, s.ParentSquadID)
			}
		}
	}
	if !parentInList || !childInList {
		t.Fatalf("list response missing parent/child entries (parentInList=%v childInList=%v)", parentInList, childInList)
	}
}

// TestCreateSquad_IncludedSquadIDs_RejectsInvalidId locks the failure
// contract: any included squad id that is not an existing, unarchived,
// not-yet-nested squad in this workspace fails the whole request with a 400
// and creates nothing.
func TestCreateSquad_IncludedSquadIDs_RejectsInvalidId(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "nest-invalid-leader", nil)

	// 1. Nonexistent id.
	w := httptest.NewRecorder()
	r := squadScopeReq("", "POST", "/api/squads", map[string]any{
		"name":               "Invalid Include Parent",
		"leader_id":          leaderID,
		"included_squad_ids": []string{"00000000-0000-0000-0000-0000000000aa"},
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("nonexistent included squad: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Archived squad.
	archived := createSquadAs(t, "", "To Be Archived", leaderID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE squad SET archived_at = now() WHERE id = $1`, archived.ID); err != nil {
		t.Fatalf("archive squad: %v", err)
	}
	w = httptest.NewRecorder()
	r = squadScopeReq("", "POST", "/api/squads", map[string]any{
		"name":               "Invalid Include Parent",
		"leader_id":          leaderID,
		"included_squad_ids": []string{archived.ID},
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("archived included squad: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// 3. Squad already nested under another parent (one-level rule).
	nested := createSquadAs(t, "", "Nested Under Something", leaderID)
	outer := createSquadIncludingAs(t, "", "Outer Parent", leaderID, []string{nested.ID})
	w = httptest.NewRecorder()
	r = squadScopeReq("", "POST", "/api/squads", map[string]any{
		"name":               "Invalid Include Parent",
		"leader_id":          leaderID,
		"included_squad_ids": []string{nested.ID},
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("already-nested included squad: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Nothing should have been created by the failed attempts.
	ctx := context.Background()
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM squad WHERE name = 'Invalid Include Parent'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed create left %d squad rows behind", count)
	}
	_ = outer // keep cleanup registration
}

// TestDeleteSquad_ArchivingParentDetachesChildren locks the lifecycle rule:
// archiving a parent squad clears its children's parent_squad_id (they become
// independent squads again) and does NOT archive the children.
func TestDeleteSquad_ArchivingParentDetachesChildren(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "nest-archive-leader", nil)
	child := createSquadAs(t, "", "Archive Child", leaderID)
	parent := createSquadIncludingAs(t, "", "Archive Parent", leaderID, []string{child.ID})

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadScopeReq("", "DELETE", "/api/squads", nil,
		map[string]string{"id": parent.ID}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSquad(parent): expected 204, got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	childRow, err := testHandler.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          util.MustParseUUID(child.ID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load child after parent archive: %v", err)
	}
	if childRow.ParentSquadID.Valid {
		t.Fatalf("child parent_squad_id should be cleared after parent archive, got %v", childRow.ParentSquadID)
	}
	if childRow.ArchivedAt.Valid {
		t.Fatalf("child must NOT be archived along with the parent")
	}
}

// TestBuildSquadRoster_IncludesChildSquadMembers locks the parent-leader
// unified-management contract: the leader briefing roster lists members of
// nested child squads (annotated with the child squad name and a usable
// mention), dedupes members who appear in both the parent and a child squad,
// and skips archived child members.
func TestBuildSquadRoster_IncludesChildSquadMembers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	parent := seedSquadForBriefing(t, leaderID, "Roster Parent", "")

	// Child squad 1 with one agent member.
	child1 := seedSquadForBriefing(t, leaderID, "Roster Child One", "")
	child1Member := createHandlerTestAgent(t, "Child One Bot", []byte("[]"))
	addAgentMember(t, child1.ID, child1Member, "implementer")

	// Child squad 2 with a human member + an archived agent member.
	child2 := seedSquadForBriefing(t, leaderID, "Roster Child Two", "")
	child2Member := createHandlerTestAgent(t, "Child Two Bot", []byte("[]"))
	addAgentMember(t, child2.ID, child2Member, "tester")
	memberRowID, userID, userName := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, child2.ID, userID, "reviewer")
	archivedBot := createHandlerTestAgent(t, "Child Archived Bot", []byte("[]"))
	addAgentMember(t, child2.ID, archivedBot, "")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, archivedBot); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	// Member shared between parent and child1 — must appear only once, under
	// the direct roster.
	shared := createHandlerTestAgent(t, "Shared Across Levels", []byte("[]"))
	addAgentMember(t, parent.ID, shared, "dev")
	addAgentMember(t, child1.ID, shared, "dev")

	// Nest the children under the parent.
	for _, child := range []db.Squad{child1, child2} {
		if _, err := testHandler.Queries.SetSquadParent(ctx, db.SetSquadParentParams{
			ID:            child.ID,
			ParentSquadID: parent.ID,
		}); err != nil {
			t.Fatalf("nest child: %v", err)
		}
	}

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, parent, true)

	for _, want := range []string{
		"## Child Squads (merged roster)",
		"`[@Child One Bot](mention://agent/" + child1Member + ")`",
		"Child One Bot (Roster Child One)",
		"`[@Child Two Bot](mention://agent/" + child2Member + ")`",
		"Child Two Bot (Roster Child Two)",
		"`[@" + userName + "](mention://member/" + userID + ")`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected roster to contain %q\n--- roster ---\n%s", want, out)
		}
	}

	// Dedup: the shared member's direct row appears exactly once and never
	// repeats in the child block (the name legitimately appears twice in the
	// direct row — display name + mention label).
	directRows := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- Shared Across Levels") {
			directRows++
		}
	}
	if directRows != 1 {
		t.Errorf("shared member direct row should appear exactly once, got %d\n--- roster ---\n%s",
			directRows, out)
	}
	if strings.Contains(out, "Shared Across Levels (Roster Child One)") {
		t.Errorf("shared member must not repeat in the child block\n--- roster ---\n%s", out)
	}
	// Archived child member must not appear at all.
	if strings.Contains(out, "Child Archived Bot") || strings.Contains(out, archivedBot) {
		t.Errorf("archived child member must not appear in roster\n--- roster ---\n%s", out)
	}

	// Mentions round-trip through util.ParseMentions.
	mentions := util.ParseMentions(out)
	got := make(map[string]string, len(mentions))
	for _, m := range mentions {
		got[m.ID] = m.Type
	}
	for _, want := range []struct{ id, kind string }{
		{child1Member, "agent"},
		{child2Member, "agent"},
		{userID, "member"},
	} {
		if got[want.id] != want.kind {
			t.Errorf("expected %s mention for id %s, got %q", want.kind, want.id, got[want.id])
		}
	}
}
