package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// F1 (LIU-9 子任务A) atomic create-with-members tests (AC-1).
//
// POST /api/squads accepts an optional members array and creates the squad +
// all members in ONE transaction (I1). Any invalid member fails the whole
// request with a 400 carrying failed_members — no half-created squad (AC-1.3).

// createSquadWithMembers hits the CreateSquad handler with the given members
// payload and returns the recorder, so tests can assert status + body.
func createSquadWithMembers(t *testing.T, name, leaderID string, members any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := squadScopeReq("", "POST", "/api/squads", map[string]any{
		"name":      name,
		"leader_id": leaderID,
		"members":   members,
	}, nil)
	testHandler.CreateSquad(w, r)
	return w
}

func countSquadRows(t *testing.T, name string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM squad WHERE name = $1`, name).Scan(&count); err != nil {
		t.Fatalf("count squads %q: %v", name, err)
	}
	return count
}

// TestCreateSquad_WithMembers_AddsAtomically locks the F1 happy path: agents +
// humans passed in members are all present in the member list after a single
// request, the leader is role="leader", and each member carries its optional
// role (F2).
func TestCreateSquad_WithMembers_AddsAtomically(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "F1 Members Leader", nil)
	agentMember := createHandlerTestAgent(t, "F1 Members Agent", nil)
	_, humanID, _ := seededHumanMember(t)

	w := createSquadWithMembers(t, "F1 Atomic Squad", leaderID, []map[string]any{
		{"member_type": "agent", "member_id": agentMember, "role": "dev"},
		{"member_type": "member", "member_id": humanID, "role": "reviewer"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	// The create response's member preview must include leader + both members.
	roles := map[string]string{}
	for _, p := range resp.MemberPreview {
		roles[p.MemberType+":"+p.MemberID] = p.Role
	}
	if roles["agent:"+leaderID] != "leader" {
		t.Fatalf("leader must auto-join with role=leader, got %+v", roles)
	}
	if roles["agent:"+agentMember] != "dev" {
		t.Fatalf("agent member should carry role 'dev', got %+v", roles)
	}
	if roles["member:"+humanID] != "reviewer" {
		t.Fatalf("human member should carry role 'reviewer', got %+v", roles)
	}

	// The persisted member list agrees with the preview.
	membersResp := httptest.NewRecorder()
	testHandler.ListSquadMembers(membersResp, squadScopeReq("", "GET", "/api/squads", nil, map[string]string{"id": resp.ID}))
	if membersResp.Code != http.StatusOK {
		t.Fatalf("ListSquadMembers: expected 200, got %d: %s", membersResp.Code, membersResp.Body.String())
	}
	var members []SquadMemberResponse
	if err := json.NewDecoder(membersResp.Body).Decode(&members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members (leader + 2), got %d: %+v", len(members), members)
	}
}

// TestCreateSquad_WithMembers_InvalidMemberRollsBack locks AC-1.3: any member
// that fails validation (agent not in workspace) rejects the WHOLE request
// with a 400 carrying failed_members, and no squad row is created.
func TestCreateSquad_WithMembers_InvalidMemberRollsBack(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "F1 Fail Leader", nil)
	unknownAgent := "00000000-0000-0000-0000-000000000099"

	w := createSquadWithMembers(t, "F1 Must Not Exist", leaderID, []map[string]any{
		{"member_type": "agent", "member_id": unknownAgent},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		FailedMembers []struct {
			MemberType string `json:"member_type"`
			MemberID   string `json:"member_id"`
			Reason     string `json:"reason"`
		} `json:"failed_members"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode failure body: %v", err)
	}
	if len(body.FailedMembers) != 1 {
		t.Fatalf("expected 1 failed member, got %+v", body.FailedMembers)
	}
	if body.FailedMembers[0].MemberID != unknownAgent || body.FailedMembers[0].Reason == "" {
		t.Fatalf("unexpected failed member entry: %+v", body.FailedMembers[0])
	}
	if countSquadRows(t, "F1 Must Not Exist") != 0 {
		t.Fatalf("failed create must not leave a squad row behind")
	}
}

// TestCreateSquad_WithMembers_DuplicateIsConflict locks AC-1.5: the same
// member listed twice in members is a conflict, not a silent merge.
func TestCreateSquad_WithMembers_DuplicateIsConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "F1 Dup Leader", nil)
	agentMember := createHandlerTestAgent(t, "F1 Dup Agent", nil)

	w := createSquadWithMembers(t, "F1 Dup Squad", leaderID, []map[string]any{
		{"member_type": "agent", "member_id": agentMember, "role": "dev"},
		{"member_type": "agent", "member_id": agentMember, "role": "dev"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate member, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "duplicate member") {
		t.Fatalf("expected duplicate-member reason in body, got %s", w.Body.String())
	}
	if countSquadRows(t, "F1 Dup Squad") != 0 {
		t.Fatalf("duplicate member create must not leave a squad row behind")
	}
}

// TestCreateSquad_WithMembers_LeaderConflict locks AC-1.5/1.2: naming the
// leader inside members is a conflict because the leader auto-joins.
func TestCreateSquad_WithMembers_LeaderConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "F1 Leader Conflict Leader", nil)

	w := createSquadWithMembers(t, "F1 Leader Conflict Squad", leaderID, []map[string]any{
		{"member_type": "agent", "member_id": leaderID, "role": "dev"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for leader-in-members, got %d: %s", w.Code, w.Body.String())
	}
	if countSquadRows(t, "F1 Leader Conflict Squad") != 0 {
		t.Fatalf("leader-conflict create must not leave a squad row behind")
	}
}

// TestCreateSquad_NoMembers_BackwardCompatible locks AC-1.4: omitting members
// behaves exactly like before — squad created with the leader as the only
// member.
func TestCreateSquad_NoMembers_BackwardCompatible(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "F1 No Members Leader", nil)

	w := httptest.NewRecorder()
	r := squadScopeReq("", "POST", "/api/squads", map[string]any{
		"name":      "F1 No Members Squad",
		"leader_id": leaderID,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	membersResp := httptest.NewRecorder()
	testHandler.ListSquadMembers(membersResp, squadScopeReq("", "GET", "/api/squads", nil, map[string]string{"id": resp.ID}))
	var members []SquadMemberResponse
	if err := json.NewDecoder(membersResp.Body).Decode(&members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 1 || members[0].MemberID != leaderID || members[0].Role != "leader" {
		t.Fatalf("expected only the leader member, got %+v", members)
	}
	// Default switch is on (I1 default true).
	if !resp.UpgradeOnMemberMention {
		t.Fatalf("upgrade_on_member_mention must default to true, got %v", resp.UpgradeOnMemberMention)
	}
}

// TestUpdateSquad_UpgradeOnMemberMention locks I3: PUT /api/squads/{id} can
// toggle upgrade_on_member_mention (subtask A owns only this field; phases is
// subtask B).
func TestUpdateSquad_UpgradeOnMemberMention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "I3 Toggle Leader", nil)
	squad := createSquadAs(t, "", "I3 Toggle Squad", leaderID)

	turnOff := httptest.NewRecorder()
	testHandler.UpdateSquad(turnOff, squadScopeReq("", "PUT", "/api/squads", map[string]any{
		"upgrade_on_member_mention": false,
	}, map[string]string{"id": squad.ID}))
	if turnOff.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", turnOff.Code, turnOff.Body.String())
	}
	var off SquadResponse
	if err := json.NewDecoder(turnOff.Body).Decode(&off); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if off.UpgradeOnMemberMention {
		t.Fatalf("expected upgrade_on_member_mention=false after update")
	}

	// The squad row itself reflects the toggle.
	var stored bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT upgrade_on_member_mention FROM squad WHERE id = $1`, util.MustParseUUID(squad.ID)).Scan(&stored); err != nil {
		t.Fatalf("load toggle: %v", err)
	}
	if stored {
		t.Fatalf("expected stored upgrade_on_member_mention=false")
	}
}
