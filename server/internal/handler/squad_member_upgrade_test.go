package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F3 (LIU-9 子任务A) member-mention upgrade tests.
//
// The rule (AC-2.1/2.2): @-mentioning an ORDINARY squad member (non-leader)
// who belongs to exactly one non-archived squad — with that squad's
// upgrade_on_member_mention switch on and no other member of the squad
// mentioned in the same comment — upgrades the mention to a squad-level
// trigger (source=mention_squad_leader, squad mounted) that wakes the squad
// Leader. The member's own personal task is deferred (B01 serial semantics):
// only the Leader task runs, and the Leader delegates back to the member via
// a later @mention.

// mentionTriggersAndTargets computes the mention-derived triggers AND the
// per-mention targets for a comment, so tests can assert both the leader
// trigger and the member's deferred outcome.
func mentionTriggersAndTargets(t *testing.T, issue db.Issue, content string) ([]commentAgentTrigger, []commentMentionTarget) {
	t.Helper()
	return testHandler.computeCommentAgentTriggers(context.Background(), issue, content, nil, "member", testUserID, commentTriggerComputeOptions{})
}

func findTarget(targets []commentMentionTarget, targetID string) *commentMentionTarget {
	for i := range targets {
		if targets[i].TargetID == targetID {
			return &targets[i]
		}
	}
	return nil
}

// addSquadMemberAgent adds an agent member to an existing squad row directly
// (fixtures insert squads without the API, so the leader is added explicitly).
func addSquadMemberAgent(t *testing.T, squadID, agentID, role string) {
	t.Helper()
	if _, err := testHandler.Queries.AddSquadMember(context.Background(), db.AddSquadMemberParams{
		SquadID:    util.MustParseUUID(squadID),
		MemberType: "agent",
		MemberID:   util.MustParseUUID(agentID),
		Role:       role,
	}); err != nil {
		t.Fatalf("add squad member %s: %v", agentID, err)
	}
}

// newPlainIssue seeds a plain (unassigned) issue so the only triggers come
// from the comment's mentions.
func newPlainIssue(t *testing.T) db.Issue {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "f3 trigger").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	issue, err := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return issue
}

// TestF3_MemberMentionUpgradesToSquadLeader locks the happy path: a pure
// @mention of an ordinary member of exactly one squad produces a
// mention_squad_leader trigger for that squad's leader with the squad mounted,
// and the member's own task is deferred (deferred_member) rather than
// enqueued.
func TestF3_MemberMentionUpgradesToSquadLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	triggers, targets := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	leader := findTrigger(triggers, fx.LeaderID)
	if leader == nil {
		t.Fatalf("expected a squad-leader trigger for leader %s, got %+v", fx.LeaderID, triggers)
	}
	if leader.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected F3 upgrade to mention_squad_leader, got %q", leader.Source)
	}
	if leader.Squad == nil || util.UUIDToString(leader.Squad.ID) != fx.SquadID {
		t.Fatalf("expected squad %s mounted on upgraded trigger, got %+v", fx.SquadID, leader.Squad)
	}
	// The member must NOT have a personal trigger (its task defers).
	if m := findTrigger(triggers, fx.MemberID); m != nil {
		t.Fatalf("member must not have a personal trigger after upgrade, got %+v", m)
	}
	mt := findTarget(targets, fx.MemberID)
	if mt == nil {
		t.Fatalf("expected a deferred target for member %s, got %+v", fx.MemberID, targets)
	}
	if mt.Status != DispatchDeferred || mt.ReasonCode != ReasonDeferredMember {
		t.Fatalf("expected member target deferred with deferred_member, got status=%s reason=%s", mt.Status, mt.ReasonCode)
	}
}

// TestF3_MultiSquadMemberStaysPersonal: a member of more than one non-archived
// squad is NOT upgraded (no guessing) — the mention stays personal.
func TestF3_MultiSquadMemberStaysPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	// Second squad the member belongs to.
	var secondSquadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "F3 Second Squad", fx.LeaderID, testUserID).Scan(&secondSquadID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, secondSquadID)
	})
	addSquadMemberAgent(t, secondSquadID, fx.MemberID, "dev")

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	member := findTrigger(triggers, fx.MemberID)
	if member == nil {
		t.Fatalf("expected a personal trigger for member, got %+v", triggers)
	}
	if member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("multi-squad member must stay personal mention_agent, got %q", member.Source)
	}
	if member.Squad != nil {
		t.Fatalf("expected no squad on personal trigger, got %+v", member.Squad)
	}
	if leader := findTrigger(triggers, fx.LeaderID); leader != nil {
		t.Fatalf("no leader trigger expected for multi-squad member, got %+v", leader)
	}
}

// TestF3_SwitchOffKeepsMentionPersonal: with upgrade_on_member_mention=false
// the member mention stays personal (AC-2.6 safety valve).
func TestF3_SwitchOffKeepsMentionPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE squad SET upgrade_on_member_mention = false WHERE id = $1`, fx.SquadID); err != nil {
		t.Fatalf("disable switch: %v", err)
	}

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	member := findTrigger(triggers, fx.MemberID)
	if member == nil || member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("expected personal mention_agent when switch is off, got %+v", triggers)
	}
	if leader := findTrigger(triggers, fx.LeaderID); leader != nil {
		t.Fatalf("no leader trigger expected when switch is off, got %+v", leader)
	}
}

// TestF3_ArchivedSquadDoesNotCountForUniqueness locks R2: the uniqueness
// judgment counts only non-archived squads. A member of 1 active + 1 archived
// squad still upgrades (the archived one does not break uniqueness).
func TestF3_ArchivedSquadDoesNotCountForUniqueness(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	// Member belongs to a second squad that is then archived.
	var secondSquadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "F3 Archived Squad", fx.LeaderID, testUserID).Scan(&secondSquadID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, secondSquadID)
	})
	addSquadMemberAgent(t, secondSquadID, fx.MemberID, "dev")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE squad SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, secondSquadID); err != nil {
		t.Fatalf("archive second squad: %v", err)
	}

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	leader := findTrigger(triggers, fx.LeaderID)
	if leader == nil || leader.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected upgrade despite the archived squad (R2), got %+v", triggers)
	}
	if leader.Squad == nil || util.UUIDToString(leader.Squad.ID) != fx.SquadID {
		t.Fatalf("expected squad %s mounted, got %+v", fx.SquadID, leader.Squad)
	}
}

// TestF3_MemberOfOnlyArchivedSquadStaysPersonal locks the other half of R2: a
// member whose ONLY squad is archived is not upgraded to any (archived) leader
// — the mention stays personal.
func TestF3_MemberOfOnlyArchivedSquadStaysPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE squad SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, fx.SquadID); err != nil {
		t.Fatalf("archive squad: %v", err)
	}

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	member := findTrigger(triggers, fx.MemberID)
	if member == nil || member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("expected personal mention_agent for only-archived membership, got %+v", triggers)
	}
}

// TestF3_EscapeHatchMultipleMembersSameComment: @-ing two members of the same
// squad in one comment keeps BOTH mentions personal (AC-2.1 escape hatch).
func TestF3_EscapeHatchMultipleMembersSameComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)
	otherMember := createHandlerTestAgent(t, "F3 Second Member", nil)
	addSquadMemberAgent(t, fx.SquadID, otherMember, "tester")

	content := "[@Member](mention://agent/" + fx.MemberID + ") [@Second](mention://agent/" + otherMember + ")"
	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, content)

	for _, id := range []string{fx.MemberID, otherMember} {
		tr := findTrigger(triggers, id)
		if tr == nil || tr.Source != commentTriggerSourceMentionAgent {
			t.Fatalf("expected personal mention_agent for %s when multiple members are @'d, got %+v", id, triggers)
		}
	}
	if leader := findTrigger(triggers, fx.LeaderID); leader != nil {
		t.Fatalf("no leader trigger expected when multiple members are @'d, got %+v", leader)
	}
}

// TestF3_SubSquadMemberUpgradesToSubLeader: a member who belongs only to a
// child squad upgrades to the CHILD squad's leader (ListSquadsByMember returns
// direct memberships only). Dual parent+child membership stays personal.
func TestF3_SubSquadMemberUpgradesToSubLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	subLeader := createHandlerTestAgent(t, "F3 Sub Leader", nil)
	subMember := createHandlerTestAgent(t, "F3 Sub Member", nil)
	parentLeader := createHandlerTestAgent(t, "F3 Parent Leader", nil)

	var childSquadID, parentSquadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4) RETURNING id
	`, testWorkspaceID, "F3 Child Squad", subLeader, testUserID).Scan(&childSquadID); err != nil {
		t.Fatalf("create child squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, childSquadID) })
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4) RETURNING id
	`, testWorkspaceID, "F3 Parent Squad", parentLeader, testUserID).Scan(&parentSquadID); err != nil {
		t.Fatalf("create parent squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, parentSquadID) })
	addSquadMemberAgent(t, childSquadID, subMember, "dev")
	if _, err := testHandler.Queries.SetSquadParent(ctx, db.SetSquadParentParams{
		ID:            util.MustParseUUID(childSquadID),
		ParentSquadID: util.MustParseUUID(parentSquadID),
	}); err != nil {
		t.Fatalf("nest child: %v", err)
	}

	issue := newPlainIssue(t)

	// Child-only member → upgrade to the CHILD leader.
	triggers, _ := mentionTriggersAndTargets(t, issue, "[@SubMember](mention://agent/"+subMember+")")
	subLeaderTr := findTrigger(triggers, subLeader)
	if subLeaderTr == nil || subLeaderTr.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected child-squad leader upgrade, got %+v", triggers)
	}
	if subLeaderTr.Squad == nil || util.UUIDToString(subLeaderTr.Squad.ID) != childSquadID {
		t.Fatalf("expected child squad %s mounted, got %+v", childSquadID, subLeaderTr.Squad)
	}

	// Dual membership (parent + child) → multi-squad, no upgrade.
	addSquadMemberAgent(t, parentSquadID, subMember, "dev")
	triggers, _ = mentionTriggersAndTargets(t, issue, "[@SubMember](mention://agent/"+subMember+")")
	if tr := findTrigger(triggers, subMember); tr == nil || tr.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("dual parent+child member must stay personal, got %+v", triggers)
	}
	for _, leaderID := range []string{subLeader, parentLeader} {
		if tr := findTrigger(triggers, leaderID); tr != nil {
			t.Fatalf("no leader trigger expected for dual member, got %+v", tr)
		}
	}
}

// TestF3_PriorityLeaderUpgradeWins: an agent who is the unique leader of one
// squad AND a member of another is upgraded by the SR3 leader rule (leader
// role is a strict superset), not the member rule (B06).
func TestF3_PriorityLeaderUpgradeWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	// The member leads their own squad, so SR3 now applies to them.
	var ledSquadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "F3 Member-Led Squad", fx.MemberID, testUserID).Scan(&ledSquadID); err != nil {
		t.Fatalf("create member-led squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, ledSquadID)
	})

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	tr := findTrigger(triggers, fx.MemberID)
	if tr == nil || tr.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected SR3 leader upgrade (priority over member upgrade), got %+v", triggers)
	}
	if tr.Squad == nil || util.UUIDToString(tr.Squad.ID) != ledSquadID {
		t.Fatalf("expected the LEADER-owned squad %s mounted, got %+v", ledSquadID, tr.Squad)
	}
}

// TestF3_LeaderDelegatingOwnMemberStaysPersonal: when the squad leader itself
// @s its own member, the member mention stays personal (B01 "Leader 委派时再
// @ 触发" — delegation must reach the member, not bounce back to the leader).
func TestF3_LeaderDelegatingOwnMemberStaysPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	triggers, _ := testHandler.computeCommentAgentTriggers(context.Background(), fx.Issue,
		"[@Member](mention://agent/"+fx.MemberID+")", nil, "agent", fx.LeaderID,
		commentTriggerComputeOptions{OriginatorUserID: testUserID})

	member := findTrigger(triggers, fx.MemberID)
	if member == nil || member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("leader @-ing its own member must stay personal mention_agent, got %+v", triggers)
	}
	if leader := findTrigger(triggers, fx.LeaderID); leader != nil {
		t.Fatalf("no self-directed squad upgrade expected, got %+v", leader)
	}
}

// TestF3_UnavailableLeaderFallsBackToPersonal: when the squad leader cannot
// run (archived), the upgrade is skipped and the member's personal task runs —
// the explicit @mention must not be silently dropped.
func TestF3_UnavailableLeaderFallsBackToPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, fx.LeaderID); err != nil {
		t.Fatalf("archive leader agent: %v", err)
	}

	triggers, _ := mentionTriggersAndTargets(t, fx.Issue, "[@Member](mention://agent/"+fx.MemberID+")")

	member := findTrigger(triggers, fx.MemberID)
	if member == nil || member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("expected personal fallback when leader is archived, got %+v", triggers)
	}
}
