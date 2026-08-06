package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// sr3Fixture builds a self-contained squad led by a fresh agent so the agent
// is the UNIQUE leader of exactly one non-archived squad (SR3 upgrade
// candidate). The issue is NOT squad-assigned (a plain issue) so the only
// triggers come from the comment's mentions.
type sr3Fixture struct {
	Issue    db.Issue
	SquadID  string
	LeaderID string
	MemberID string // second agent, added as a member of the squad
	OtherID  string // third agent, NOT a squad member
}

func newSR3Fixture(t *testing.T) sr3Fixture {
	t.Helper()
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "SR3 Leader", nil)
	memberID := createHandlerTestAgent(t, "SR3 Member", nil)
	otherID := createHandlerTestAgent(t, "SR3 Other", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "SR3 Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	// Leader is auto-added on create through the API, but this fixture inserts
	// the squad row directly — add leader + member rows explicitly.
	for _, m := range []struct{ id, role string }{{leaderID, "leader"}, {memberID, "dev"}} {
		if _, err := testHandler.Queries.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID:    util.MustParseUUID(squadID),
			MemberType: "agent",
			MemberID:   util.MustParseUUID(m.id),
			Role:       m.role,
		}); err != nil {
			t.Fatalf("add squad member %s: %v", m.id, err)
		}
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "sr3 trigger").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return sr3Fixture{
		Issue:     issue,
		SquadID:   squadID,
		LeaderID:  leaderID,
		MemberID:  memberID,
		OtherID:   otherID,
	}
}

// mentionTriggers computes the mention-derived triggers for a comment.
func mentionTriggers(t *testing.T, issue db.Issue, content string) []commentAgentTrigger {
	t.Helper()
	triggers, _ := testHandler.computeCommentAgentTriggers(context.Background(), issue, content, nil, "member", testUserID, commentTriggerComputeOptions{})
	return triggers
}

func findTrigger(triggers []commentAgentTrigger, agentID string) *commentAgentTrigger {
	for i := range triggers {
		if util.UUIDToString(triggers[i].Agent.ID) == agentID {
			return &triggers[i]
		}
	}
	return nil
}

// TestSR3_AgentMentionUniqueSquadLeaderUpgrades locks the core SR3 rule: a
// pure @agent mention of the unique leader of one non-archived squad is
// upgraded to a squad-level leader trigger (mention_squad_leader + squad
// mounted), so the daemon injects the Squad Operating Protocol + roster.
func TestSR3_AgentMentionUniqueSquadLeaderUpgrades(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	triggers := mentionTriggers(t, fx.Issue, "[@Leader](mention://agent/"+fx.LeaderID+")")
	tr := findTrigger(triggers, fx.LeaderID)
	if tr == nil {
		t.Fatalf("expected a trigger for leader %s, got %+v", fx.LeaderID, triggers)
	}
	if tr.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected SR3 upgrade to mention_squad_leader, got %q", tr.Source)
	}
	if tr.Squad == nil || util.UUIDToString(tr.Squad.ID) != fx.SquadID {
		t.Fatalf("expected squad %s mounted on upgraded trigger, got %+v", fx.SquadID, tr.Squad)
	}
}

// TestSR3_NonLeaderAgentMentionStaysPersonal: an agent who leads no squad is
// triggered as a plain personal mention.
func TestSR3_NonLeaderAgentMentionStaysPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	triggers := mentionTriggers(t, fx.Issue, "[@Other](mention://agent/"+fx.OtherID+")")
	tr := findTrigger(triggers, fx.OtherID)
	if tr == nil {
		t.Fatalf("expected a trigger for %s, got %+v", fx.OtherID, triggers)
	}
	if tr.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("expected mention_agent for non-leader, got %q", tr.Source)
	}
	if tr.Squad != nil {
		t.Fatalf("expected no squad on personal trigger, got %+v", tr.Squad)
	}
}

// TestSR3_MultiSquadLeaderMentionStaysPersonal: an agent leading two squads
// is NOT auto-upgraded — pure @agent stays personal (no guessing).
func TestSR3_MultiSquadLeaderMentionStaysPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	// Second squad led by the same agent → the agent now leads two squads.
	var secondSquadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "SR3 Second Squad", fx.LeaderID, testUserID).Scan(&secondSquadID); err != nil {
		t.Fatalf("create second squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, secondSquadID)
	})

	triggers := mentionTriggers(t, fx.Issue, "[@Leader](mention://agent/"+fx.LeaderID+")")
	tr := findTrigger(triggers, fx.LeaderID)
	if tr == nil {
		t.Fatalf("expected a trigger for leader %s, got %+v", fx.LeaderID, triggers)
	}
	if tr.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("multi-squad leader must stay personal mention_agent, got %q", tr.Source)
	}
	if tr.Squad != nil {
		t.Fatalf("expected no squad on multi-leader personal trigger, got %+v", tr.Squad)
	}
}

// TestSR3_EscapeHatchMemberMentionKeepsLeaderPersonal: @-ing the unique squad
// leader together with another member of that squad keeps the leader mention
// personal (the documented way to hand the leader a personal task).
func TestSR3_EscapeHatchMemberMentionKeepsLeaderPersonal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	content := "[@Leader](mention://agent/" + fx.LeaderID + ") [@Member](mention://agent/" + fx.MemberID + ")"
	triggers := mentionTriggers(t, fx.Issue, content)

	leader := findTrigger(triggers, fx.LeaderID)
	if leader == nil {
		t.Fatalf("expected a trigger for leader, got %+v", triggers)
	}
	if leader.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("leader mention must stay personal when a same-squad member is also @'d, got %q", leader.Source)
	}
	member := findTrigger(triggers, fx.MemberID)
	if member == nil {
		t.Fatalf("expected a trigger for member, got %+v", triggers)
	}
	if member.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("member mention must stay personal, got %q", member.Source)
	}
}

// TestSR3_NonMemberMentionDoesNotSuppressUpgrade: @-ing the leader together
// with an agent who is NOT a member of the squad still upgrades the leader.
func TestSR3_NonMemberMentionDoesNotSuppressUpgrade(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	content := "[@Leader](mention://agent/" + fx.LeaderID + ") [@Other](mention://agent/" + fx.OtherID + ")"
	triggers := mentionTriggers(t, fx.Issue, content)

	leader := findTrigger(triggers, fx.LeaderID)
	if leader == nil {
		t.Fatalf("expected a trigger for leader, got %+v", triggers)
	}
	if leader.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("leader must still upgrade when a non-member agent is also @'d, got %q", leader.Source)
	}
	other := findTrigger(triggers, fx.OtherID)
	if other == nil || other.Source != commentTriggerSourceMentionAgent {
		t.Fatalf("non-member mention should stay personal, got %+v", other)
	}
}

// TestSR3_ExplicitSquadMentionMergesToLeaderRole: an explicit @squad mention
// of the leader's squad plus the @agent mention of the leader both resolve to
// one squad-leader trigger (leader-role-wins merge, deduped by agent).
func TestSR3_ExplicitSquadMentionMergesToLeaderRole(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	content := "[@Leader](mention://agent/" + fx.LeaderID + ") [@SR3 Squad](mention://squad/" + fx.SquadID + ")"
	triggers := mentionTriggers(t, fx.Issue, content)

	leader := findTrigger(triggers, fx.LeaderID)
	if leader == nil {
		t.Fatalf("expected a trigger for leader, got %+v", triggers)
	}
	if leader.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected squad-leader source after merge, got %q", leader.Source)
	}
	if leader.Squad == nil || util.UUIDToString(leader.Squad.ID) != fx.SquadID {
		t.Fatalf("expected squad %s mounted, got %+v", fx.SquadID, leader.Squad)
	}
}

// TestSR2_SquadMentionRoutesSquadLevel is the SR2 regression guard: a plain
// mention://squad/<id> produces a squad-leader trigger with the squad mounted
// (existing routing, unchanged by SR3).
func TestSR2_SquadMentionRoutesSquadLevel(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSR3Fixture(t)

	triggers := mentionTriggers(t, fx.Issue, "[@SR3 Squad](mention://squad/"+fx.SquadID+")")
	tr := findTrigger(triggers, fx.LeaderID)
	if tr == nil {
		t.Fatalf("expected a trigger for squad leader %s, got %+v", fx.LeaderID, triggers)
	}
	if tr.Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("expected mention_squad_leader for @squad, got %q", tr.Source)
	}
	if tr.Squad == nil || util.UUIDToString(tr.Squad.ID) != fx.SquadID {
		t.Fatalf("expected squad %s mounted, got %+v", fx.SquadID, tr.Squad)
	}
}

// TestBuildSquadRoster_ChildSquadMentionTargets locks SR1: the parent
// leader's briefing lists each non-archived child squad as a copyable @squad
// mention with its member count.
func TestBuildSquadRoster_ChildSquadMentionTargets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	parent := seedSquadForBriefing(t, leaderID, "Roster Parent SR1", "")

	child := seedSquadForBriefing(t, leaderID, "Roster Child SR1", "")
	childMember := createHandlerTestAgent(t, "SR1 Child Bot", []byte("[]"))
	addAgentMember(t, child.ID, childMember, "implementer")

	if _, err := testHandler.Queries.SetSquadParent(ctx, db.SetSquadParentParams{
		ID:            child.ID,
		ParentSquadID: parent.ID,
	}); err != nil {
		t.Fatalf("nest child: %v", err)
	}

	out := buildSquadLeaderBriefing(ctx, testHandler.Queries, parent, true)

	childMention := "[@" + "Roster Child SR1" + "](mention://squad/" + util.UUIDToString(child.ID) + ")"
	if !strings.Contains(out, childMention) {
		t.Errorf("expected child squad mention %q in roster\n--- roster ---\n%s", childMention, out)
	}
	if !strings.Contains(out, "Squads:") {
		t.Errorf("expected a Squads: block listing @squad targets\n--- roster ---\n%s", out)
	}
	// Member count line: "- Roster Child SR1 — 1 members — `[@Roster Child SR1](mention://squad/...)"
	// (seedSquadForBriefing does not auto-add the leader, so the child squad
	// holds exactly the one added member.)
	if !strings.Contains(out, "Roster Child SR1 — 1 members") {
		t.Errorf("expected child squad row with member count\n--- roster ---\n%s", out)
	}
	// The child member row (SR1 keeps member rows) must still be present.
	if !strings.Contains(out, "SR1 Child Bot (Roster Child SR1)") {
		t.Errorf("expected child member row to remain\n--- roster ---\n%s", out)
	}
	// Mentions round-trip through util.ParseMentions.
	mentions := util.ParseMentions(out)
	roundTripped := false
	for _, m := range mentions {
		if m.Type == "squad" && m.ID == util.UUIDToString(child.ID) {
			roundTripped = true
		}
	}
	if !roundTripped {
		t.Errorf("child squad mention must round-trip through util.ParseMentions\n--- roster ---\n%s", out)
	}
}
