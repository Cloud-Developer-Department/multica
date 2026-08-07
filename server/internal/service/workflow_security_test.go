package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestNodeReviewRequired_CriticalTypesForced pins V-01 (security audit): the
// critical node types (requirements / architecture / code_review / testing)
// must always require human review, no matter what a caller requests in
// customizations. This is the server-side guarantee that an agent can never
// turn off the CLO-175 human review gate on those nodes.
func TestNodeReviewRequired_CriticalTypesForced(t *testing.T) {
	critical := []string{
		NodeTypeRequirements,
		NodeTypeArchitecture,
		NodeTypeCodeReview,
		NodeTypeTesting,
	}
	for _, nodeType := range critical {
		if got := nodeReviewRequired(nodeType, false); !got {
			t.Errorf("nodeReviewRequired(%q, false) = false; want true (critical node must force review)", nodeType)
		}
		if got := nodeReviewRequired(nodeType, true); !got {
			t.Errorf("nodeReviewRequired(%q, true) = false; want true", nodeType)
		}
	}
}

// TestNodeReviewRequired_NonCriticalHonorsRequest pins that non-critical node
// types keep honoring the caller's requested review flag.
func TestNodeReviewRequired_NonCriticalHonorsRequest(t *testing.T) {
	for _, nodeType := range []string{
		NodeTypeDevelopment,
		NodeTypeSecurity,
		NodeTypeDocumentation,
		NodeTypeDeployment,
		NodeTypeOther,
	} {
		if got := nodeReviewRequired(nodeType, false); got {
			t.Errorf("nodeReviewRequired(%q, false) = true; want false", nodeType)
		}
		if got := nodeReviewRequired(nodeType, true); !got {
			t.Errorf("nodeReviewRequired(%q, true) = false; want true", nodeType)
		}
	}
}

func testAttributionText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func testAttributionUUID(u string) pgtype.UUID { return mustParseUUIDOrEmpty(u) }

// TestResolveAttributionAssignee_NodeWins pins V-02 (security audit): when the
// workflow node has an assignee configured, the node assignee is the effective
// attribution target even if the mapped issue carries a different assignee.
func TestResolveAttributionAssignee_NodeWins(t *testing.T) {
	node := db.WorkflowNode{AssigneeType: testAttributionText("agent"), AssigneeID: testAttributionUUID("11111111-1111-1111-1111-111111111111")}
	issue := db.Issue{AssigneeType: testAttributionText("agent"), AssigneeID: testAttributionUUID("22222222-2222-2222-2222-222222222222")}
	typ, id := resolveAttributionAssignee(node, &issue)
	if typ != "agent" || id != testAttributionUUID("11111111-1111-1111-1111-111111111111") {
		t.Fatalf("node assignee should win, got type=%q id=%s", typ, id)
	}
}

// TestResolveAttributionAssignee_IssueFallback pins V-02: when the node has no
// assignee (CLI `workflow create` without customizations), the node's mapped
// issue assignee becomes the effective attribution target.
func TestResolveAttributionAssignee_IssueFallback(t *testing.T) {
	node := db.WorkflowNode{AssigneeType: pgtype.Text{}, AssigneeID: pgtype.UUID{}}
	issue := db.Issue{AssigneeType: testAttributionText("agent"), AssigneeID: testAttributionUUID("33333333-3333-3333-3333-333333333333")}
	typ, id := resolveAttributionAssignee(node, &issue)
	if typ != "agent" || id != testAttributionUUID("33333333-3333-3333-3333-333333333333") {
		t.Fatalf("issue assignee should be the fallback, got type=%q id=%s", typ, id)
	}
}

// TestResolveAttributionAssignee_NonePins V-02: when neither the node nor its
// mapped issue has an assignee, the effective target is empty so the caller
// applies the workflow-creator default-deny.
func TestResolveAttributionAssignee_None(t *testing.T) {
	node := db.WorkflowNode{AssigneeType: pgtype.Text{}, AssigneeID: pgtype.UUID{}}
	typ, id := resolveAttributionAssignee(node, nil)
	if typ != "" || id.Valid {
		t.Fatalf("expected empty assignee, got type=%q id=%v", typ, id)
	}
}
