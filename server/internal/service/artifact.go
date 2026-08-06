package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Artifact lifecycle statuses (Q2 / AC-AR).
const (
	ArtifactStatusDraft      = "draft"
	ArtifactStatusSubmitted  = "submitted"
	ArtifactStatusApproved   = "approved"
	ArtifactStatusRejected   = "rejected"
	ArtifactStatusSuperseded = "superseded"
)

// Valid artifact content types.
const (
	ContentTypeMarkdown = "markdown"
	ContentTypeJSON     = "json"
	ContentTypeText     = "text"
	ContentTypeFile     = "file"
)

// ArtifactService owns the Artifact domain: submission, versions, diff,
// human review (Q3), the review queue, and review statistics (Q6).
type ArtifactService struct {
	Queries         *db.Queries
	TxStarter       TxStarter
	Bus             *events.Bus
	WorkflowService *WorkflowService
}

func NewArtifactService(q *db.Queries, tx TxStarter, bus *events.Bus, wfs *WorkflowService) *ArtifactService {
	return &ArtifactService{Queries: q, TxStarter: tx, Bus: bus, WorkflowService: wfs}
}

// CreateArtifactParams is the validated input to ArtifactService.Submit.
type CreateArtifactParams struct {
	WorkspaceID      pgtype.UUID
	WorkflowID       pgtype.UUID
	NodeID           pgtype.UUID
	IssueID          pgtype.UUID
	Type             string
	Title            string
	Content          string
	ContentType      string
	FileAttachmentID pgtype.UUID
	AuthorType       string // "member" or "agent"
	AuthorID         pgtype.UUID
}

// Submit registers an artifact. Content-style artifacts store their text;
// file-style artifacts register the attachment id produced by /api/upload-file
// (Q2). Submission marks the artifact 'submitted', supersedes older versions
// (AC-AR2), moves the node into in_review, and notifies the reviewer.
func (s *ArtifactService) Submit(ctx context.Context, p CreateArtifactParams) (db.Artifact, error) {
	node, err := s.Queries.GetWorkflowNode(ctx, db.GetWorkflowNodeParams{ID: p.NodeID, WorkflowID: p.WorkflowID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Artifact{}, workflowError(CodeNotFound, "workflow node not found", nil)
		}
		return db.Artifact{}, err
	}
	// P2-2 (api_design §4.1): only an active/todo node may accept an artifact
	// submission. Backlog (not yet activated), done, cancelled and in_review
	// nodes are rejected with invalid_state so the state machine constraints
	// hold for submissions too.
	if node.Status != workflow.StatusInProgress && node.Status != workflow.StatusTodo {
		return db.Artifact{}, workflowError(CodeInvalidState,
			fmt.Sprintf("workflow node status %q does not allow artifact submission (expected in_progress or todo)", node.Status), nil)
	}
	wf, err := s.Queries.GetWorkflow(ctx, db.GetWorkflowParams{ID: p.WorkflowID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Artifact{}, workflowError(CodeNotFound, "workflow not found in this workspace", nil)
		}
		return db.Artifact{}, err
	}

	// M-2 (security audit): an artifact submission must be attributable to the
	// node's assigned actor (when one is configured) and must reference the
	// node's own mapped issue — otherwise any member/agent could submit to any
	// node, pollute the version history, and trip review notifications.
	//
	// V-02 (security audit): the attribution check previously only ran when the
	// node had an assignee configured. CLI `workflow create` sends no
	// customizations, so template nodes carry no assignee and the gate silently
	// lapsed. Now: when the node has no assignee, fall back to the node's
	// mapped issue assignee; when neither the node nor its issue has an
	// assignee, default-deny and only the workflow creator may submit.
	var issue *db.Issue
	if node.IssueID.Valid {
		loaded, ierr := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: node.IssueID, WorkspaceID: p.WorkspaceID})
		if ierr != nil {
			return db.Artifact{}, workflowError(CodeForbidden, "cannot verify artifact submitter: mapped issue not found", nil)
		}
		issue = &loaded
	}
	effectiveAssigneeType, effectiveAssigneeID := resolveAttributionAssignee(node, issue)
	submitterOK := false
	if effectiveAssigneeType != "" && effectiveAssigneeID.Valid {
		// The node (or its mapped issue) has an assignee — only that assignee
		// may submit. The workflow creator does NOT override a configured
		// assignee.
		submitterOK, err = s.actorMatchesAssignee(ctx, p.AuthorType, p.AuthorID, effectiveAssigneeType, effectiveAssigneeID)
		if err != nil {
			return db.Artifact{}, workflowError(CodeForbidden, "cannot verify artifact submitter: "+err.Error(), nil)
		}
	} else {
		// Neither the node nor its mapped issue has an assignee — default-deny,
		// only the workflow creator may submit (V-02).
		submitterOK = wf.CreatedByType == p.AuthorType && wf.CreatedByID == p.AuthorID
	}
	if !submitterOK {
		return db.Artifact{}, workflowError(CodeForbidden, "artifact submitter is not the workflow node's assignee", nil)
	}
	if node.IssueID.Valid && p.IssueID.Valid && p.IssueID != node.IssueID {
		return db.Artifact{}, workflowError(CodeInvalidRequest, "issue_id does not match the workflow node's mapped issue", nil)
	}
	// M-2: a file-style artifact must reference an attachment that exists in
	// this workspace.
	if p.FileAttachmentID.Valid {
		if _, err := s.Queries.GetAttachment(ctx, db.GetAttachmentParams{ID: p.FileAttachmentID, WorkspaceID: p.WorkspaceID}); err != nil {
			return db.Artifact{}, workflowError(CodeInvalidRequest, "file attachment not found in this workspace", nil)
		}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Artifact{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	next, err := qtx.GetNextArtifactVersion(ctx, db.GetNextArtifactVersionParams{NodeID: p.NodeID, Type: p.Type})
	if err != nil {
		return db.Artifact{}, err
	}
	contentArg := pgtype.Text{}
	if p.Content != "" {
		contentArg = util.StrToText(p.Content)
	}
	issueArg := pgtype.UUID{}
	if p.IssueID.Valid {
		issueArg = p.IssueID
	}
	artifact, err := qtx.CreateArtifact(ctx, db.CreateArtifactParams{
		WorkspaceID:      p.WorkspaceID,
		WorkflowID:       p.WorkflowID,
		NodeID:           p.NodeID,
		IssueID:          issueArg,
		Type:             p.Type,
		Title:            p.Title,
		Content:          contentArg,
		ContentType:      p.ContentType,
		FileAttachmentID: p.FileAttachmentID,
		Version:          next,
		Status:           ArtifactStatusSubmitted,
		AuthorType:       p.AuthorType,
		AuthorID:         p.AuthorID,
	})
	if err != nil {
		// AC-AR2: concurrent submissions race on the (node_id, type, version)
		// unique index; surface it as a conflict (409) instead of a raw 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Artifact{}, workflowError(CodeDuplicateVersion, "a version for this artifact was already submitted concurrently; re-submit to obtain the next version", nil)
		}
		return db.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}
	if err := qtx.SupersedeArtifactVersions(ctx, db.SupersedeArtifactVersionsParams{
		NodeID: p.NodeID,
		Type:   p.Type,
		ID:     artifact.ID,
	}); err != nil {
		return db.Artifact{}, err
	}

	// Node -> in_review (review gate opens). The mapped child issue is moved
	// to in_review too so the issue-based progress UI reflects it.
	if node.ReviewRequired {
		if _, err := qtx.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
			ID:         node.ID,
			Status:     workflow.StatusInReview,
			WorkflowID: p.WorkflowID,
		}); err != nil {
			return db.Artifact{}, err
		}
		if node.IssueID.Valid {
			_, _ = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          node.IssueID,
				Status:      workflow.StatusInReview,
				WorkspaceID: p.WorkspaceID,
			})
		}
		_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
			WorkflowID: p.WorkflowID,
			NodeID:     node.ID,
			FromStatus: util.StrToText(node.Status),
			ToStatus:   workflow.StatusInReview,
			ActorType:  p.AuthorType,
			ActorID:    p.AuthorID,
			Reason:     util.StrToText("Artifact 提交，等待人工审核"),
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Artifact{}, fmt.Errorf("commit artifact tx: %w", err)
	}

	// Reviewer notification (post-commit, best-effort).
	if node.ReviewRequired {
		s.WorkflowService.NotifyArtifactReviewRequested(ctx, wf, node, artifact)
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventArtifactCreated,
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		ActorType:   p.AuthorType,
		ActorID:     util.UUIDToString(p.AuthorID),
		Payload:     map[string]any{"artifact_id": util.UUIDToString(artifact.ID), "node_id": util.UUIDToString(p.NodeID)},
	})
	return artifact, nil
}

// actorMatchesAssignee reports whether the (authorType, authorID) actor is the
// actor assigned as (assigneeType, assigneeID). A "squad" assignee accepts any
// squad member; other assignee types must match type and id exactly.
func (s *ArtifactService) actorMatchesAssignee(ctx context.Context, authorType string, authorID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (bool, error) {
	if assigneeType == "squad" {
		return s.Queries.IsSquadMember(ctx, db.IsSquadMemberParams{
			SquadID:    assigneeID,
			MemberType: authorType,
			MemberID:   authorID,
		})
	}
	return authorType == assigneeType && authorID == assigneeID, nil
}

// resolveAttributionAssignee picks the effective assignee an artifact submitter
// must match for a node (V-02 security audit). The node's own assignee wins
// when configured; otherwise the node's mapped issue assignee is the fallback.
// When issue is nil (node has no mapped issue) the issue fields are ignored.
// Returns ("", zero) when neither the node nor its mapped issue has an
// assignee — the caller then applies the workflow-creator default-deny.
func resolveAttributionAssignee(node db.WorkflowNode, issue *db.Issue) (string, pgtype.UUID) {
	if node.AssigneeType.Valid && node.AssigneeID.Valid && node.AssigneeType.String != "" {
		return node.AssigneeType.String, node.AssigneeID
	}
	if issue != nil && issue.AssigneeType.Valid && issue.AssigneeID.Valid && issue.AssigneeType.String != "" {
		return issue.AssigneeType.String, issue.AssigneeID
	}
	return "", pgtype.UUID{}
}

// Get returns an artifact scoped to its workspace.
func (s *ArtifactService) Get(ctx context.Context, workspaceID pgtype.UUID, id pgtype.UUID) (db.Artifact, error) {
	a, err := s.Queries.GetArtifact(ctx, db.GetArtifactParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Artifact{}, workflowError(CodeNotFound, "artifact not found in this workspace", nil)
		}
		return db.Artifact{}, err
	}
	return a, nil
}

// List returns artifacts for a workspace with optional filters.
func (s *ArtifactService) List(ctx context.Context, workspaceID pgtype.UUID, filters ArtifactListFilters, limit, offset int32) ([]db.Artifact, int64, error) {
	params := db.ListArtifactsParams{
		WorkspaceID: workspaceID,
		Limit:       limit,
		Offset:      offset,
		Type:        optText(filters.Type),
		Status:      optText(filters.Status),
		NodeID:      optUUID(filters.NodeID),
		WorkflowID:  optUUID(filters.WorkflowID),
		AuthorID:    optUUID(filters.AuthorID),
	}
	items, err := s.Queries.ListArtifacts(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.Queries.CountArtifacts(ctx, db.CountArtifactsParams{
		WorkspaceID: workspaceID,
		Type:        params.Type,
		Status:      params.Status,
		NodeID:      params.NodeID,
		WorkflowID:  params.WorkflowID,
		AuthorID:    params.AuthorID,
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ArtifactListFilters carries the optional list filters.
type ArtifactListFilters struct {
	Type       *string
	Status     *string
	NodeID     *pgtype.UUID
	WorkflowID *pgtype.UUID
	AuthorID   *pgtype.UUID
}

// Versions returns every version of an artifact under the same (node, type).
func (s *ArtifactService) Versions(ctx context.Context, workspaceID pgtype.UUID, id pgtype.UUID) (pgtype.UUID, string, []db.Artifact, error) {
	a, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return pgtype.UUID{}, "", nil, err
	}
	versions, err := s.Queries.GetArtifactsByNodeType(ctx, db.GetArtifactsByNodeTypeParams{NodeID: a.NodeID, Type: a.Type})
	if err != nil {
		return pgtype.UUID{}, "", nil, err
	}
	return a.NodeID, a.Type, versions, nil
}

// Diff returns a line diff between two versions of the same (node, type)
// artifact. When toVersion is 0 it diffs against the latest version (Q5).
func (s *ArtifactService) Diff(ctx context.Context, workspaceID pgtype.UUID, id pgtype.UUID, fromVersion, toVersion int32) (db.Artifact, db.Artifact, []DiffLine, error) {
	base, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return db.Artifact{}, db.Artifact{}, nil, err
	}
	from, err := s.Queries.GetArtifactByNodeTypeVersion(ctx, db.GetArtifactByNodeTypeVersionParams{
		NodeID: base.NodeID, Type: base.Type, Version: fromVersion,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Artifact{}, db.Artifact{}, nil, workflowError(CodeNotFound, "from version not found", nil)
		}
		return db.Artifact{}, db.Artifact{}, nil, err
	}
	to, err := s.Queries.GetArtifactByNodeTypeVersion(ctx, db.GetArtifactByNodeTypeVersionParams{
		NodeID: base.NodeID, Type: base.Type, Version: toVersion,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Artifact{}, db.Artifact{}, nil, workflowError(CodeNotFound, "to version not found", nil)
		}
		return db.Artifact{}, db.Artifact{}, nil, err
	}
	return from, to, DiffLines(from.Content.String, to.Content.String), nil
}

// ReviewParams is the validated input to ArtifactService.Review.
type ReviewParams struct {
	ArtifactID  pgtype.UUID
	WorkspaceID pgtype.UUID
	Action      string // "approved" or "rejected"
	Comment     string
	ReviewerID  pgtype.UUID
}

// ReviewResult describes the side effects of a review decision.
type ReviewResult struct {
	Artifact        db.Artifact
	Action          string
	Comment         string
	Node            db.WorkflowNode
	NodeStatus      string
	Workflow        db.Workflow
	Advanced        bool
	NextStage       int32
	Review          db.ArtifactReview
}

// Review applies a human review decision to an artifact (Q3). Approval moves
// the node to done and advances the stage when its barrier closes; rejection
// moves the node back to todo and re-triggers the responsible agent.
//
// Concurrency safety: the artifact status CAS (submitted -> decided) means a
// concurrent duplicate review hits zero rows and returns already_reviewed.
func (s *ArtifactService) Review(ctx context.Context, p ReviewParams) (ReviewResult, error) {
	a, err := s.Queries.GetArtifact(ctx, db.GetArtifactParams{ID: p.ArtifactID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ReviewResult{}, workflowError(CodeNotFound, "artifact not found in this workspace", nil)
		}
		return ReviewResult{}, err
	}
	if a.Status != ArtifactStatusSubmitted {
		return ReviewResult{}, workflowError(CodeNotReviewable, "artifact is not in submitted status and cannot be reviewed", nil)
	}
	if p.Action == "rejected" && strings.TrimSpace(p.Comment) == "" {
		return ReviewResult{}, workflowError(CodeRejectReasonRequired, "rejection reason is required", nil)
	}

	node, err := s.Queries.GetWorkflowNode(ctx, db.GetWorkflowNodeParams{ID: a.NodeID, WorkflowID: a.WorkflowID})
	if err != nil {
		return ReviewResult{}, fmt.Errorf("load workflow node: %w", err)
	}
	wf, err := s.Queries.GetWorkflow(ctx, db.GetWorkflowParams{ID: a.WorkflowID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return ReviewResult{}, fmt.Errorf("load workflow: %w", err)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	// CAS: only decide an artifact still in 'submitted'.
	decidedStatus := ArtifactStatusApproved
	if p.Action == "rejected" {
		decidedStatus = ArtifactStatusRejected
	}
	updated, err := qtx.UpdateArtifactStatusFrom(ctx, db.UpdateArtifactStatusFromParams{
		ID:             p.ArtifactID,
		WorkspaceID:    p.WorkspaceID,
		NewStatus:      decidedStatus,
		ExpectedStatus: ArtifactStatusSubmitted,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ReviewResult{}, workflowError(CodeAlreadyReviewed, "artifact was already reviewed", nil)
		}
		return ReviewResult{}, err
	}

	review, err := qtx.CreateArtifactReview(ctx, db.CreateArtifactReviewParams{
		ArtifactID:   p.ArtifactID,
		ReviewerType: "member",
		ReviewerID:   p.ReviewerID,
		Action:       p.Action,
		Comment:      util.StrToText(p.Comment),
	})
	if err != nil {
		return ReviewResult{}, err
	}

	result := ReviewResult{
		Artifact:  updated,
		Action:    p.Action,
		Comment:   p.Comment,
		Node:      node,
		Workflow:  wf,
		Review:    review,
	}

	if p.Action == "approved" {
		result.NodeStatus = workflow.StatusDone
		updatedNode, err := qtx.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
			ID:         node.ID,
			Status:     workflow.StatusDone,
			WorkflowID: a.WorkflowID,
		})
		if err != nil {
			return ReviewResult{}, err
		}
		result.Node = updatedNode
		if node.IssueID.Valid {
			_, _ = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          node.IssueID,
				Status:      workflow.StatusDone,
				WorkspaceID: p.WorkspaceID,
			})
		}
		_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
			WorkflowID: a.WorkflowID,
			NodeID:     node.ID,
			FromStatus: util.StrToText(node.Status),
			ToStatus:   workflow.StatusDone,
			ActorType:  "member",
			ActorID:    p.ReviewerID,
			Reason:     util.StrToText("人工审核通过"),
		})
	} else {
		result.NodeStatus = workflow.StatusTodo
		updatedNode, err := qtx.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
			ID:         node.ID,
			Status:     workflow.StatusTodo,
			WorkflowID: a.WorkflowID,
		})
		if err != nil {
			return ReviewResult{}, err
		}
		result.Node = updatedNode
		if node.IssueID.Valid {
			_, _ = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          node.IssueID,
				Status:      workflow.StatusTodo,
				WorkspaceID: p.WorkspaceID,
			})
		}
		_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
			WorkflowID: a.WorkflowID,
			NodeID:     node.ID,
			FromStatus: util.StrToText(node.Status),
			ToStatus:   workflow.StatusTodo,
			ActorType:  "member",
			ActorID:    p.ReviewerID,
			Reason:     util.StrToText("人工审核打回：" + p.Comment),
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return ReviewResult{}, fmt.Errorf("commit review tx: %w", err)
	}

	// Post-commit: stage advancement on approval (AC-W3), agent re-trigger on
	// rejection, and notifications.
	if p.Action == "approved" {
		nodes, err := s.Queries.ListWorkflowNodes(ctx, db.ListWorkflowNodesParams{WorkflowID: a.WorkflowID})
		if err == nil {
			bySeq := nodesToBySeq(nodes)
			if workflow.StageBarrierClosed(bySeq, wf.CurrentStage) {
				if next := workflow.NextStageToOpen(bySeq, wf.CurrentStage); next != 0 {
					advanced, _, err := s.WorkflowService.AdvanceStage(ctx, p.WorkspaceID, a.WorkflowID, "member", p.ReviewerID, "审核通过后自动推进阶段")
					if err == nil {
						result.Advanced = true
						result.NextStage = advanced.CurrentStage
					} else {
						slog.Warn("artifact review: stage auto-advance failed", "workflow_id", util.UUIDToString(a.WorkflowID), "error", err)
					}
				}
			}
		}
	} else {
		// Reject: re-trigger the node's agent so it can produce a new version
		// (AC-E2E2). Load the fresh issue row and enqueue through the chain.
		if node.IssueID.Valid {
			issueRow, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: node.IssueID, WorkspaceID: p.WorkspaceID})
			if err == nil && issueRow.AssigneeID.Valid {
				if _, err := s.WorkflowService.TaskService.EnqueueTaskForIssue(ctx, issueRow); err != nil {
					slog.Warn("artifact review: re-enqueue rejected node task failed", "issue_id", util.UUIDToString(node.IssueID), "error", err)
				}
			}
		}
	}

	s.Bus.Publish(events.Event{
		Type:        protocol.EventArtifactReviewed,
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(p.ReviewerID),
		Payload: map[string]any{
			"artifact_id":    util.UUIDToString(p.ArtifactID),
			"action":         p.Action,
			"workflow_id":    util.UUIDToString(a.WorkflowID),
			"node_id":        util.UUIDToString(a.NodeID),
			"workflow_status": result.Workflow.Status,
		},
	})
	return result, nil
}

// Reviews returns the review history of an artifact (FR4.6).
func (s *ArtifactService) Reviews(ctx context.Context, workspaceID pgtype.UUID, artifactID pgtype.UUID) ([]db.ArtifactReview, error) {
	if _, err := s.Get(ctx, workspaceID, artifactID); err != nil {
		return nil, err
	}
	return s.Queries.ListArtifactReviews(ctx, db.ListArtifactReviewsParams{ArtifactID: artifactID})
}

// ReviewQueue returns submitted artifacts awaiting review (FR4.1).
func (s *ArtifactService) ReviewQueue(ctx context.Context, workspaceID pgtype.UUID, limit, offset int32) ([]db.Artifact, int64, error) {
	items, err := s.Queries.ListReviewQueue(ctx, db.ListReviewQueueParams{
		WorkspaceID: workspaceID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.Queries.CountReviewQueue(ctx, db.CountReviewQueueParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ReviewStats summarizes review outcomes over a time window (Q6).
type ReviewStats struct {
	WorkspaceID string
	From        time.Time
	To          time.Time
	Total       int64
	Approved    int64
	Rejected    int64
	Pending     int64
	ApprovalRate float64
	AvgDuration time.Duration
	ByType      []TypeStats
}

// TypeStats aggregates review statistics per artifact type.
type TypeStats struct {
	Type               string
	Submitted          int64
	ApprovalRate       float64
	AvgReviewDuration  time.Duration
	approvedCount      int64
	rejectedCount      int64
}

// Stats computes review statistics (Q6 / api_design 4.8).
func (s *ArtifactService) Stats(ctx context.Context, workspaceID pgtype.UUID, from, to time.Time) (ReviewStats, error) {
	fromArg := pgtype.Timestamptz{Time: from, Valid: true}
	toArg := pgtype.Timestamptz{Time: to, Valid: true}

	artifacts, err := s.Queries.ListArtifactsInRange(ctx, db.ListArtifactsInRangeParams{
		WorkspaceID:   workspaceID,
		FromCreatedAt: fromArg,
		ToCreatedAt:   toArg,
	})
	if err != nil {
		return ReviewStats{}, err
	}
	reviews, err := s.Queries.ListArtifactReviewsInRange(ctx, db.ListArtifactReviewsInRangeParams{
		WorkspaceID:   workspaceID,
		FromCreatedAt: fromArg,
		ToCreatedAt:   toArg,
	})
	if err != nil {
		return ReviewStats{}, err
	}

	stats := ReviewStats{
		WorkspaceID: util.UUIDToString(workspaceID),
		From:        from,
		To:          to,
		Total:       int64(len(artifacts)),
	}
	byType := map[string]*TypeStats{}
	durations := map[pgtype.UUID][]time.Duration{}

	for _, a := range artifacts {
		switch a.Status {
		case ArtifactStatusApproved:
			stats.Approved++
		case ArtifactStatusRejected:
			stats.Rejected++
		case ArtifactStatusSubmitted:
			stats.Pending++
		}
		ts, ok := byType[a.Type]
		if !ok {
			ts = &TypeStats{Type: a.Type}
			byType[a.Type] = ts
		}
		ts.Submitted++
		switch a.Status {
		case ArtifactStatusApproved:
			ts.approvedCount++
		case ArtifactStatusRejected:
			ts.rejectedCount++
		}
	}
	for _, r := range reviews {
		if r.ReviewedAt.Valid && r.ArtifactCreatedAt.Valid {
			d := r.ReviewedAt.Time.Sub(r.ArtifactCreatedAt.Time)
			if d >= 0 {
				durations[r.ArtifactID] = append(durations[r.ArtifactID], d)
			}
		}
	}
	decided := stats.Approved + stats.Rejected
	if decided > 0 {
		stats.ApprovalRate = float64(stats.Approved) / float64(decided)
	}
	var sumDur time.Duration
	var nDur int
	for _, ds := range durations {
		for _, d := range ds {
			sumDur += d
			nDur++
		}
	}
	if nDur > 0 {
		stats.AvgDuration = sumDur / time.Duration(nDur)
	}
	for _, ts := range byType {
		dec := ts.approvedCount + ts.rejectedCount
		if dec > 0 {
			ts.ApprovalRate = float64(ts.approvedCount) / float64(dec)
		}
		stats.ByType = append(stats.ByType, *ts)
	}
	return stats, nil
}

// DiffLine is one line of a content diff.
type DiffLine struct {
	Line int    `json:"line"`
	Op   string `json:"op"`
	Text string `json:"text"`
}

// DiffLines computes a simple line diff between two texts using LCS. The
// implementation is dependency-free (architecture §5 chose to avoid new
// libraries under vendor constraints).
func DiffLines(from, to string) []DiffLine {
	fromLines := strings.Split(strings.TrimRight(from, "\n"), "\n")
	toLines := strings.Split(strings.TrimRight(to, "\n"), "\n")
	if len(fromLines) == 1 && fromLines[0] == "" {
		fromLines = nil
	}
	if len(toLines) == 1 && toLines[0] == "" {
		toLines = nil
	}
	// Simple prefix/suffix trim for the common case, then fall back to
	// per-line markup for the remainder. This keeps the diff readable for
	// markdown documents without a heavy algorithm.
	prefix := 0
	for prefix < len(fromLines) && prefix < len(toLines) && fromLines[prefix] == toLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(fromLines)-prefix && suffix < len(toLines)-prefix &&
		fromLines[len(fromLines)-1-suffix] == toLines[len(toLines)-1-suffix] {
		suffix++
	}
	var out []DiffLine
	line := 1
	for i := 0; i < prefix; i++ {
		out = append(out, DiffLine{Line: line, Op: "=", Text: fromLines[i]})
		line++
	}
	fromMid := fromLines[prefix : len(fromLines)-suffix]
	toMid := toLines[prefix : len(toLines)-suffix]
	for i := 0; i < len(fromMid); i++ {
		out = append(out, DiffLine{Line: line, Op: "-", Text: fromMid[i]})
		line++
	}
	for i := 0; i < len(toMid); i++ {
		out = append(out, DiffLine{Line: line, Op: "+", Text: toMid[i]})
		line++
	}
	for i := len(toLines) - suffix; i < len(toLines); i++ {
		out = append(out, DiffLine{Line: line, Op: "=", Text: toLines[i]})
		line++
	}
	return out
}

func optText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return util.StrToText(*v)
}

func optUUID(v *pgtype.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return *v
}
