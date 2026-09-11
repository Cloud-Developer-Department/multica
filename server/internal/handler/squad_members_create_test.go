package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func countRows(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func containsSubstring(s, sub string) bool {
	return strings.Contains(s, sub)
}

// squadMembersOf returns the (member_type, member_id, role) rows for a squad,
// ordered deterministically for assertions.
func squadMembersOf(t *testing.T, squadID string) []struct {
	MemberType string
	MemberID   pgtype.UUID
	Role       string
} {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT member_type, member_id, role FROM squad_member WHERE squad_id = $1::uuid ORDER BY member_type, member_id
	`, squadID)
	if err != nil {
		t.Fatalf("list squad members: %v", err)
	}
	defer rows.Close()
	var out []struct {
		MemberType string
		MemberID   pgtype.UUID
		Role       string
	}
	for rows.Next() {
		var m struct {
			MemberType string
			MemberID   pgtype.UUID
			Role       string
		}
		if err := rows.Scan(&m.MemberType, &m.MemberID, &m.Role); err != nil {
			t.Fatalf("scan squad member: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func createSquadWithMembersAs(t *testing.T, userID string, body map[string]any) (*httptest.ResponseRecorder, SquadResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	r := squadScopeReq(userID, "POST", "/api/squads", body, nil)
	testHandler.CreateSquad(w, r)
	var resp SquadResponse
	if w.Code == http.StatusCreated {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode squad: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
			testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
		})
	}
	return w, resp
}

// TestCreateSquad_WithMembers_CreatesLeaderAndAllMembers verifies the F1/CLO-418
// atomic create path: members supplied in the create payload are persisted
// alongside the auto-added leader in one request.
func TestCreateSquad_WithMembers_CreatesLeaderAndAllMembers(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "sq-members-leader", nil)
	m1 := createHandlerTestAgent(t, "sq-members-m1", nil)
	m2 := createHandlerTestAgent(t, "sq-members-m2", nil)

	w, resp := createSquadWithMembersAs(t, "", map[string]any{
		"name":      "Members Squad",
		"leader_id": leaderID,
		"members": []map[string]any{
			{"member_type": "agent", "member_id": m1, "role": "member"},
			{"member_type": "agent", "member_id": m2, "role": ""},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad with members: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	rows := squadMembersOf(t, resp.ID)
	if len(rows) != 3 {
		t.Fatalf("expected 3 squad members (leader + 2), got %d: %+v", len(rows), rows)
	}
	roleByID := map[string]string{}
	for _, m := range rows {
		roleByID[uuidToString(m.MemberID)] = m.Role
	}
	if roleByID[leaderID] != "leader" {
		t.Errorf("leader member role = %q, want leader", roleByID[leaderID])
	}
	if roleByID[m1] != "member" {
		t.Errorf("m1 member role = %q, want member", roleByID[m1])
	}
	if roleByID[m2] != "member" {
		t.Errorf("m2 (blank role) = %q, want default member", roleByID[m2])
	}
}

// TestCreateSquad_InvalidMember_FailsAtomically verifies a bad member rejects
// the whole create with a structured failed_members payload and leaves no
// squad behind (atomic fail).
func TestCreateSquad_InvalidMember_FailsAtomically(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "sq-members-bad-leader", nil)
	before := countRows(t, `SELECT COUNT(*) FROM squad WHERE name = 'Bad Members Squad'`)

	w, _ := createSquadWithMembersAs(t, "", map[string]any{
		"name":      "Bad Members Squad",
		"leader_id": leaderID,
		"members": []map[string]any{
			{"member_type": "agent", "member_id": "00000000-0000-0000-0000-000000000000", "role": "member"},
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid member, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error         string `json:"error"`
		FailedMembers []struct {
			MemberType string `json:"member_type"`
			MemberID   string `json:"member_id"`
			Reason     string `json:"reason"`
		} `json:"failed_members"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if len(body.FailedMembers) != 1 {
		t.Fatalf("expected 1 failed_members entry, got %d: %s", len(body.FailedMembers), w.Body.String())
	}
	if body.FailedMembers[0].Reason == "" {
		t.Errorf("failed_members[0].reason is empty")
	}
	after := countRows(t, `SELECT COUNT(*) FROM squad WHERE name = 'Bad Members Squad'`)
	if after != before {
		t.Errorf("atomic fail violated: squad count changed %d -> %d", before, after)
	}
}

// TestCreateSquad_MemberType_RequiresWorkspaceMembership verifies a user member
// must be a workspace member; a non-member user is rejected with failed_members.
func TestCreateSquad_MemberType_RequiresWorkspaceMembership(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "sq-members-user-leader", nil)

	// testUserID is the workspace owner seeded by setupHandlerTestFixture — succeeds.
	w, resp := createSquadWithMembersAs(t, "", map[string]any{
		"name":      "User Member Squad",
		"leader_id": leaderID,
		"members": []map[string]any{
			{"member_type": "member", "member_id": testUserID, "role": "member"},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("user member create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(squadMembersOf(t, resp.ID)); got != 2 {
		t.Fatalf("expected 2 members (leader + user member), got %d", got)
	}

	// otherMemberID is not a workspace member — fails atomically.
	w2, _ := createSquadWithMembersAs(t, "", map[string]any{
		"name":      "NonMember User Squad",
		"leader_id": leaderID,
		"members": []map[string]any{
			{"member_type": "member", "member_id": otherMemberID, "role": "member"},
		},
	})
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("non-member user: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
	if !containsSubstring(w2.Body.String(), "failed_members") {
		t.Errorf("expected failed_members in error body, got: %s", w2.Body.String())
	}
}

// TestCreateSquad_LeaderInMembers_Skipped verifies listing the leader again in
// members does not create a duplicate member row.
func TestCreateSquad_LeaderInMembers_Skipped(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "sq-members-self-leader", nil)

	w, resp := createSquadWithMembersAs(t, "", map[string]any{
		"name":      "Self Member Squad",
		"leader_id": leaderID,
		"members": []map[string]any{
			{"member_type": "agent", "member_id": leaderID, "role": "member"},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(squadMembersOf(t, resp.ID)); got != 1 {
		t.Fatalf("expected 1 member (leader only, no duplicate), got %d", got)
	}
}
