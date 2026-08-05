package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WorkflowError carries a machine-readable code so handlers can translate it
// into the API error contract ({error, code, detail}). See api_design.md §2.
type WorkflowError struct {
	Code   string
	Msg    string
	Detail any
}

func (e *WorkflowError) Error() string { return e.Msg }

// Workflow codes (api_design.md §2).
const (
	CodeInvalidRequest       = "invalid_request"
	CodeInvalidTransition    = "invalid_transition"
	CodeRejectReasonRequired = "reject_reason_required"
	CodeNotFound             = "not_found"
	CodeDuplicateVersion     = "duplicate_version"
	CodeAlreadyReviewed      = "already_reviewed"
	CodeStageConflict        = "stage_conflict"
	CodeNotReviewable        = "not_reviewable"
	CodeInvalidState         = "invalid_state"
	CodeForbidden            = "forbidden"
)

func workflowError(code, msg string, detail any) *WorkflowError {
	return &WorkflowError{Code: code, Msg: msg, Detail: detail}
}

// TemplateNode is a single node config inside a workflow definition.
type TemplateNode struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	AssigneeType   string `json:"assignee_type"`
	AssigneeID     string `json:"assignee_id"`
	ReviewRequired bool   `json:"review_required"`
}

// TemplateStage is one stage inside a workflow definition.
type TemplateStage struct {
	Stage int            `json:"stage"`
	Name  string         `json:"name"`
	Nodes []TemplateNode `json:"nodes"`
}

// TemplateDefinition is the JSONB snapshot stored on the workflow row.
type TemplateDefinition struct {
	TemplateKey string                       `json:"template_key"`
	Stages      []TemplateStage              `json:"stages"`
	Transitions map[string][]string          `json:"transitions,omitempty"`
}

// SoftwareRDTemplateKey is the built-in AI software R&D pipeline template.
const SoftwareRDTemplateKey = "software_rd"

// Node type names shared between the template and artifact type enum.
const (
	NodeTypeRequirements   = "requirements"
	NodeTypeArchitecture   = "architecture"
	NodeTypeDevelopment    = "development"
	NodeTypeTesting        = "testing"
	NodeTypeCodeReview     = "code_review"
	NodeTypeSecurity       = "security"
	NodeTypeDocumentation  = "documentation"
	NodeTypeDeployment     = "deployment"
	NodeTypeOther          = "other"
)

// criticalReviewNodeTypes are node types whose gate must always require human
// review. V-01 (security audit): even an owner/admin customizing a workflow
// must not be able to set review_required=false on these nodes — they are the
// core gates of the CLO-175 human review chain.
var criticalReviewNodeTypes = map[string]bool{
	NodeTypeRequirements: true,
	NodeTypeArchitecture: true,
	NodeTypeCodeReview:   true,
	NodeTypeTesting:      true,
}

// nodeReviewRequired enforces V-01: a critical node type always requires
// human review regardless of the caller-supplied customization.
func nodeReviewRequired(nodeType string, requested bool) bool {
	if criticalReviewNodeTypes[nodeType] {
		return true
	}
	return requested
}

// BuiltInTemplate returns the software_rd template definition (5 stages,
// AC-W1). Node assignees are filled at create time from the request's
// customizations, so the template carries no assignee defaults.
func BuiltInTemplate() TemplateDefinition {
	return TemplateDefinition{
		TemplateKey: SoftwareRDTemplateKey,
		Stages: []TemplateStage{
			{Stage: 1, Name: "需求分析", Nodes: []TemplateNode{{Type: NodeTypeRequirements, Name: "需求分析", ReviewRequired: true}}},
			{Stage: 2, Name: "架构设计", Nodes: []TemplateNode{{Type: NodeTypeArchitecture, Name: "架构设计", ReviewRequired: true}}},
			{Stage: 3, Name: "开发实现", Nodes: []TemplateNode{
				{Type: NodeTypeDevelopment, Name: "开发实现", ReviewRequired: false},
				{Type: NodeTypeCodeReview, Name: "代码审查", ReviewRequired: true},
			}},
			{Stage: 4, Name: "测试验证", Nodes: []TemplateNode{{Type: NodeTypeTesting, Name: "测试验证", ReviewRequired: true}}},
			{Stage: 5, Name: "部署交付", Nodes: []TemplateNode{
				{Type: NodeTypeSecurity, Name: "安全审计", ReviewRequired: false},
				{Type: NodeTypeDeployment, Name: "部署交付", ReviewRequired: false},
			}},
		},
	}
}

// WorkflowService owns the Workflow domain: instance lifecycle, node status
// sync (Q4 write-back), stage advancement, and the transition audit trail.
// It deliberately does not depend on http.Request — handlers parse transport
// and pass typed params (same convention as IssueService).
type WorkflowService struct {
	Queries      *db.Queries
	TxStarter    TxStarter
	Bus          *events.Bus
	IssueService *IssueService
	TaskService  *TaskService
}

func NewWorkflowService(q *db.Queries, tx TxStarter, bus *events.Bus, is *IssueService, ts *TaskService) *WorkflowService {
	return &WorkflowService{Queries: q, TxStarter: tx, Bus: bus, IssueService: is, TaskService: ts}
}

// CreateWorkflowParams is the validated input to WorkflowService.Create.
type CreateWorkflowParams struct {
	WorkspaceID   pgtype.UUID
	SourceIssueID pgtype.UUID
	Name          string
	Description   string
	// TemplateKey defaults to SoftwareRDTemplateKey when empty.
	TemplateKey string
	// CustomNodes overrides template node config by type.
	CustomNodes []TemplateNode
	CreatorType string // "member" or "agent"
	CreatorID   pgtype.UUID
}

// CreateWorkflowResult carries the created instance plus the created child
// issues so the handler can build the response.
type CreateWorkflowResult struct {
	Workflow db.Workflow
	Nodes    []db.WorkflowNode
}

// Create instantiates a workflow from the built-in template, creates a child
// issue per node (Q4: reuse the existing issue → agent_task_queue → daemon
// trigger chain), and activates the first stage (backlog→todo) so the first
// agents start running.
func (s *WorkflowService) Create(ctx context.Context, p CreateWorkflowParams) (CreateWorkflowResult, error) {
	if _, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          p.SourceIssueID,
		WorkspaceID: p.WorkspaceID,
	}); err != nil {
		return CreateWorkflowResult{}, workflowError(CodeNotFound, "source issue not found in this workspace", nil)
	}

	templateKey := p.TemplateKey
	if templateKey == "" {
		templateKey = SoftwareRDTemplateKey
	}
	// Only the built-in template exists this iteration (extensibility: the
	// definition column stores whatever template produced the instance).
	if templateKey != SoftwareRDTemplateKey {
		return CreateWorkflowResult{}, workflowError(CodeInvalidRequest, "unknown template_key: "+templateKey, nil)
	}

	custom := map[string]TemplateNode{}
	for _, n := range p.CustomNodes {
		custom[n.Type] = n
	}

	def := BuiltInTemplate()
	defJSON, err := json.Marshal(def)
	if err != nil {
		return CreateWorkflowResult{}, fmt.Errorf("marshal workflow definition: %w", err)
	}

	// Flatten template nodes into (stage, seq) order so node rows and child
	// issues are created deterministically.
	type flatNode struct {
		template TemplateNode
		stage    int32
		seq      int32
	}
	var flat []flatNode
	for _, st := range def.Stages {
		for i, tn := range st.Nodes {
			cfg, ok := custom[tn.Type]
			if ok {
				tn.AssigneeType = cfg.AssigneeType
				tn.AssigneeID = cfg.AssigneeID
				tn.ReviewRequired = cfg.ReviewRequired
			}
			// V-01 (security audit): critical node types always require human
			// review; a caller-supplied false is not honored for them.
			tn.ReviewRequired = nodeReviewRequired(tn.Type, tn.ReviewRequired)
			flat = append(flat, flatNode{template: tn, stage: int32(st.Stage), seq: int32(i + 1)})
		}
	}

	// Create child issues FIRST (each through IssueService.Create so numbering
	// and the workspace-scoped duplicate guard are reused). Every node issue is
	// created as backlog so no agent task is enqueued before the workflow rows
	// exist; the first stage is activated (backlog→todo) after the workflow
	// transaction commits, which then triggers the stage-1 agents (AC-A1).
	issues := make([]pgtype.UUID, len(flat))
	for i, fn := range flat {
		res, err := s.IssueService.Create(ctx, IssueCreateParams{
			WorkspaceID:   p.WorkspaceID,
			Title:         fn.template.Name,
			Description:   util.StrToText(fmt.Sprintf("Workflow 节点: %s（阶段 %d）。请完成该节点任务并提交 Artifact。", fn.template.Name, fn.stage)),
			Status:        workflow.StatusBacklog,
			Priority:      "medium",
			AssigneeType:  strToTextOrEmpty(fn.template.AssigneeType),
			AssigneeID:    mustParseUUIDOrEmpty(fn.template.AssigneeID),
			CreatorType:   p.CreatorType,
			CreatorID:     p.CreatorID,
			ParentIssueID: p.SourceIssueID,
			Stage:         pgtype.Int4{Int32: fn.stage, Valid: true},
		}, IssueCreateOpts{ActorID: util.UUIDToString(p.CreatorID)})
		if err != nil {
			return CreateWorkflowResult{}, fmt.Errorf("create node issue (stage %d): %w", fn.stage, err)
		}
		issues[i] = res.Issue.ID
	}

	// Insert workflow + nodes in one transaction.
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return CreateWorkflowResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	wf, err := qtx.CreateWorkflow(ctx, db.CreateWorkflowParams{
		WorkspaceID:   p.WorkspaceID,
		SourceIssueID: p.SourceIssueID,
		Name:          p.Name,
		Description:   util.StrToText(p.Description),
		Definition:    defJSON,
		Status:        workflow.WorkflowStatusTodo,
		CurrentStage:  1,
		CreatedByType: p.CreatorType,
		CreatedByID:   p.CreatorID,
	})
	if err != nil {
		return CreateWorkflowResult{}, fmt.Errorf("create workflow: %w", err)
	}

	nodes := make([]db.WorkflowNode, 0, len(flat))
	for i, fn := range flat {
		node, err := qtx.CreateWorkflowNode(ctx, db.CreateWorkflowNodeParams{
			WorkflowID:     wf.ID,
			Seq:            fn.seq,
			Stage:          fn.stage,
			Type:           fn.template.Type,
			Name:           fn.template.Name,
			Description:    pgtype.Text{},
			Status:         workflow.StatusBacklog,
			IssueID:        issues[i],
			AssigneeType:   strToTextOrEmpty(fn.template.AssigneeType),
			AssigneeID:     mustParseUUIDOrEmpty(fn.template.AssigneeID),
			ReviewRequired: fn.template.ReviewRequired,
		})
		if err != nil {
			return CreateWorkflowResult{}, fmt.Errorf("create workflow node (stage %d seq %d): %w", fn.stage, fn.seq, err)
		}
		nodes = append(nodes, node)
	}
	_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
		WorkflowID: wf.ID,
		FromStatus: util.StrToText(""),
		ToStatus:   workflow.WorkflowStatusTodo,
		ActorType:  p.CreatorType,
		ActorID:    p.CreatorID,
		Reason:     util.StrToText("流程创建"),
	})

	if err := tx.Commit(ctx); err != nil {
		return CreateWorkflowResult{}, fmt.Errorf("commit workflow tx: %w", err)
	}

	// Post-commit: activate stage 1 (backlog→todo) so the assigned agents are
	// triggered through the existing issue chain. Uses the same helper as
	// AdvanceStage so the two activation paths cannot drift.
	activeNodes, err := s.activateStage(ctx, s.Queries, p.WorkspaceID, wf.ID, 1, p.CreatorType, p.CreatorID, "流程创建：激活第一阶段节点")
	if err != nil {
		slog.Warn("workflow create: activate stage 1 failed", "workflow_id", util.UUIDToString(wf.ID), "error", err)
	}
	if len(activeNodes) > 0 {
		// Reflect activation in the returned node list.
		for i := range nodes {
			for _, an := range activeNodes {
				if util.UUIDToString(nodes[i].ID) == util.UUIDToString(an.ID) {
					nodes[i] = an
				}
			}
		}
		// Recompute the workflow-instance status now that stage-1 nodes are
		// active (todo → in_progress).
		s.recomputeWorkflowStatus(ctx, wf)
	}

	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowCreated,
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		ActorType:   p.CreatorType,
		ActorID:     util.UUIDToString(p.CreatorID),
		Payload:     map[string]any{"workflow_id": util.UUIDToString(wf.ID)},
	})
	return CreateWorkflowResult{Workflow: wf, Nodes: nodes}, nil
}

// Get returns a workflow instance scoped to its workspace.
func (s *WorkflowService) Get(ctx context.Context, workspaceID pgtype.UUID, id pgtype.UUID) (db.Workflow, error) {
	wf, err := s.Queries.GetWorkflow(ctx, db.GetWorkflowParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Workflow{}, workflowError(CodeNotFound, "workflow not found in this workspace", nil)
		}
		return db.Workflow{}, err
	}
	return wf, nil
}

// List returns workflows for a workspace with optional status filter.
func (s *WorkflowService) List(ctx context.Context, workspaceID pgtype.UUID, status *string, limit, offset int32) ([]db.Workflow, int64, error) {
	statusArg := pgtype.Text{}
	if status != nil {
		statusArg = util.StrToText(*status)
	}
	items, err := s.Queries.ListWorkflows(ctx, db.ListWorkflowsParams{
		WorkspaceID: workspaceID,
		Limit:       limit,
		Offset:      offset,
		Status:      statusArg,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.Queries.CountWorkflows(ctx, db.CountWorkflowsParams{
		WorkspaceID: workspaceID,
		Status:      statusArg,
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Update metadata (name/description).
func (s *WorkflowService) Update(ctx context.Context, workspaceID pgtype.UUID, id pgtype.UUID, name *string, description *string) (db.Workflow, error) {
	nameArg := pgtype.Text{}
	if name != nil {
		nameArg = util.StrToText(*name)
	}
	descArg := pgtype.Text{}
	if description != nil {
		descArg = util.StrToText(*description)
	}
	wf, err := s.Queries.UpdateWorkflow(ctx, db.UpdateWorkflowParams{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        nameArg,
		Description: descArg,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Workflow{}, workflowError(CodeNotFound, "workflow not found in this workspace", nil)
		}
		return db.Workflow{}, err
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowUpdated,
		WorkspaceID: util.UUIDToString(workspaceID),
		Payload:     map[string]any{"workflow_id": util.UUIDToString(wf.ID)},
	})
	return wf, nil
}

// ListNodes returns the nodes of a workflow with optional stage/status filters.
func (s *WorkflowService) ListNodes(ctx context.Context, workspaceID pgtype.UUID, workflowID pgtype.UUID, stage *int32, status *string) ([]db.WorkflowNode, error) {
	if _, err := s.Get(ctx, workspaceID, workflowID); err != nil {
		return nil, err
	}
	stageArg := pgtype.Int4{}
	if stage != nil {
		stageArg = pgtype.Int4{Int32: *stage, Valid: true}
	}
	statusArg := pgtype.Text{}
	if status != nil {
		statusArg = util.StrToText(*status)
	}
	return s.Queries.ListWorkflowNodes(ctx, db.ListWorkflowNodesParams{
		WorkflowID: workflowID,
		Stage:      stageArg,
		Status:     statusArg,
	})
}

// ListTransitions returns the workflow transition audit trail.
func (s *WorkflowService) ListTransitions(ctx context.Context, workspaceID pgtype.UUID, workflowID pgtype.UUID, nodeID *pgtype.UUID, limit, offset int32) ([]db.WorkflowTransitionLog, int64, error) {
	if _, err := s.Get(ctx, workspaceID, workflowID); err != nil {
		return nil, 0, err
	}
	nodeArg := pgtype.UUID{}
	if nodeID != nil {
		nodeArg = *nodeID
	}
	items, err := s.Queries.ListWorkflowTransitions(ctx, db.ListWorkflowTransitionsParams{
		WorkflowID: workflowID,
		Limit:      limit,
		Offset:     offset,
		NodeID:     nodeArg,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.Queries.CountWorkflowTransitions(ctx, db.CountWorkflowTransitionsParams{
		WorkflowID: workflowID,
		NodeID:     nodeArg,
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// definitionTransitions parses the workflow's stored definition transitions
// map (nil-safe; the state machine falls back to the default table).
func definitionTransitions(wf db.Workflow) map[string][]string {
	var def TemplateDefinition
	if len(wf.Definition) > 0 {
		_ = json.Unmarshal(wf.Definition, &def)
	}
	return def.Transitions
}

// SyncNodeFromIssue is the Q4 write-back hook. Called from the UpdateIssue
// handler after a status change commits: if the issue maps to a workflow node,
// validate the transition and mirror the issue status onto the node, write the
// audit log, and notify the reviewer when the node enters in_review.
//
// Best-effort by contract (architecture R-table): a failure here logs and
// never rolls back the committed issue update.
func (s *WorkflowService) SyncNodeFromIssue(ctx context.Context, issue db.Issue, prevStatus string) {
	node, err := s.Queries.GetWorkflowNodeByIssue(ctx, db.GetWorkflowNodeByIssueParams{IssueID: issue.ID})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("workflow sync: load node by issue failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		}
		return
	}
	if node.Status == issue.Status {
		return
	}
	wf, err := s.Get(ctx, issue.WorkspaceID, node.WorkflowID)
	if err != nil {
		slog.Warn("workflow sync: load workflow failed", "workflow_id", util.UUIDToString(node.WorkflowID), "error", err)
		return
	}
	trans := definitionTransitions(wf)
	// The node's mapped issue may legitimately land in a status the node
	// machine restricts (e.g. direct done from in_progress on a review-free
	// node). Validate against the transition table; illegal moves are logged
	// but not blocked (the review gating happens on the artifact review path).
	if !workflow.CanTransition(trans, node.Status, issue.Status) {
		slog.Warn("workflow sync: illegal node transition via issue update",
			"node_id", util.UUIDToString(node.ID),
			"from", node.Status, "to", issue.Status)
	}

	// P1-1 (AC-REV): a review-required node must not reach 'done' purely via
	// an issue update — that would let the assigned agent bypass Artifact
	// submission and the human review gate. Mirror 'done' only when the node
	// already has an approved artifact; otherwise refuse the mirror, log, and
	// write an audit trail (best-effort, never rolls back the issue update).
	if issue.Status == workflow.StatusDone && node.ReviewRequired {
		approved, err := s.Queries.HasApprovedArtifact(ctx, db.HasApprovedArtifactParams{NodeID: node.ID})
		if err != nil {
			slog.Warn("workflow sync: check approved artifact failed",
				"node_id", util.UUIDToString(node.ID), "error", err)
		}
		if err != nil || !workflow.SyncDoneAllowed(node.ReviewRequired, approved) {
			if err == nil {
				_, _ = s.Queries.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
					WorkflowID: node.WorkflowID,
					NodeID:     node.ID,
					FromStatus: util.StrToText(node.Status),
					ToStatus:   workflow.StatusDone,
					ActorType:  "agent",
					ActorID:    issue.AssigneeID,
					Reason:     util.StrToText("拒绝镜像：review_required 节点未经人工审核（无 approved artifact）不能置 done"),
				})
				slog.Warn("workflow sync: refuse to mirror done on review-required node without approved artifact",
					"node_id", util.UUIDToString(node.ID),
					"issue_id", util.UUIDToString(issue.ID))
			}
			return
		}
	}

	updated, err := s.Queries.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
		ID:         node.ID,
		Status:     issue.Status,
		WorkflowID: node.WorkflowID,
	})
	if err != nil {
		slog.Warn("workflow sync: update node status failed", "node_id", util.UUIDToString(node.ID), "error", err)
		return
	}

	reason := ""
	if !workflow.CanTransition(trans, node.Status, issue.Status) {
		reason = "非法迁移（经 issue 状态更新回写）"
	}
	_, _ = s.Queries.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
		WorkflowID: node.WorkflowID,
		NodeID:     node.ID,
		FromStatus: util.StrToText(node.Status),
		ToStatus:   issue.Status,
		ActorType:  "agent",
		ActorID:    issue.AssigneeID,
		Reason:     util.StrToText(reason),
	})

	// Node entered in_review and the workflow requires a human review gate:
	// notify the source_issue creator (Q3 reviewer).
	if issue.Status == workflow.StatusInReview && node.ReviewRequired {
		s.notifyReviewer(ctx, wf, node)
	}

	s.recomputeWorkflowStatus(ctx, wf)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowNodeUpdated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(issue.AssigneeID),
		Payload: map[string]any{
			"workflow_id": util.UUIDToString(node.WorkflowID),
			"node_id":     util.UUIDToString(node.ID),
			"status":      updated.Status,
		},
	})
}

// AdvanceStage moves the workflow to the next unfinished stage. It verifies
// the current stage barrier is closed, CAS-increments current_stage, and
// activates (backlog→todo) the next stage's nodes so the assigned agents are
// triggered through the existing issue chain.
func (s *WorkflowService) AdvanceStage(ctx context.Context, workspaceID pgtype.UUID, workflowID pgtype.UUID, actorType string, actorID pgtype.UUID, reason string) (db.Workflow, []db.WorkflowNode, error) {
	wf, err := s.Get(ctx, workspaceID, workflowID)
	if err != nil {
		return db.Workflow{}, nil, err
	}
	if wf.Status == workflow.WorkflowStatusDone || wf.Status == workflow.WorkflowStatusCancelled {
		return db.Workflow{}, nil, workflowError(CodeInvalidState, "workflow is already finished and cannot advance", nil)
	}
	nodes, err := s.Queries.ListWorkflowNodes(ctx, db.ListWorkflowNodesParams{
		WorkflowID: workflowID,
	})
	if err != nil {
		return db.Workflow{}, nil, err
	}
	bySeq := nodesToBySeq(nodes)
	if !workflow.StageBarrierClosed(bySeq, wf.CurrentStage) {
		return db.Workflow{}, nil, workflowError(CodeInvalidState,
			fmt.Sprintf("stage %d is not closed yet (all nodes must be done/cancelled)", wf.CurrentStage), nil)
	}
	next := workflow.NextStageToOpen(bySeq, wf.CurrentStage)
	if next == 0 {
		return db.Workflow{}, nil, workflowError(CodeInvalidState, "all stages are complete", nil)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Workflow{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	// CAS: only advances if current_stage is still the expected value. The
	// target stage is the actual next stage to open (P2-1): NextStageToOpen may
	// skip past already-completed stages, so writing current_stage = next keeps
	// the instance's stage pointer aligned with the truly active stage.
	updated, err := qtx.AdvanceWorkflowStage(ctx, db.AdvanceWorkflowStageParams{
		ID:           workflowID,
		WorkspaceID:  workspaceID,
		CurrentStage: wf.CurrentStage,
		NextStage:    next,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Workflow{}, nil, workflowError(CodeStageConflict, "stage was advanced concurrently", nil)
		}
		return db.Workflow{}, nil, err
	}

	activated, err := s.activateStage(ctx, qtx, workspaceID, workflowID, next, actorType, actorID, reason)
	if err != nil {
		return db.Workflow{}, nil, err
	}

	_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
		WorkflowID: workflowID,
		FromStatus: util.StrToText(fmt.Sprintf("stage %d", wf.CurrentStage)),
		ToStatus:   fmt.Sprintf("stage %d", next),
		ActorType:  actorType,
		ActorID:    actorID,
		Reason:     util.StrToText(reason),
	})

	if err := tx.Commit(ctx); err != nil {
		return db.Workflow{}, nil, fmt.Errorf("commit advance tx: %w", err)
	}

	// The derived instance status may change once the next stage activates
	// (e.g. a previously done workflow re-enters in_progress on a re-run).
	// Best-effort recompute; AdvanceWorkflowStage already bumped current_stage.
	s.recomputeWorkflowStatus(ctx, updated)

	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowAdvanced,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"workflow_id":   util.UUIDToString(workflowID),
			"current_stage": updated.CurrentStage,
		},
	})
	return updated, activated, nil
}

// activateStage promotes every backlog node of `stage` to todo: it updates the
// node row, promotes the mapped child issue to todo, enqueues the agent task
// through the existing chain (AC-A1 / AC-W3), and writes the transition audit.
// `q` is either the workspace queries (post-commit activation, e.g. Create) or
// the transaction-scoped queries (AdvanceStage, inside the tx).
func (s *WorkflowService) activateStage(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, workflowID pgtype.UUID, stage int32, actorType string, actorID pgtype.UUID, reason string) ([]db.WorkflowNode, error) {
	nodes, err := q.ListWorkflowNodes(ctx, db.ListWorkflowNodesParams{
		WorkflowID: workflowID,
		Stage:      pgtype.Int4{Int32: stage, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	var activated []db.WorkflowNode
	for _, n := range nodes {
		if n.Status != workflow.StatusBacklog {
			continue
		}
		act, err := q.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
			ID:         n.ID,
			Status:     workflow.StatusTodo,
			WorkflowID: workflowID,
		})
		if err != nil {
			return nil, fmt.Errorf("activate node %s: %w", util.UUIDToString(n.ID), err)
		}
		activated = append(activated, act)

		// Promote the mapped child issue to todo and enqueue the agent task
		// through the existing chain (AC-A1 / AC-W3).
		if _, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          n.IssueID,
			Status:      workflow.StatusTodo,
			WorkspaceID: workspaceID,
		}); err != nil {
			slog.Warn("workflow activate: update node issue status failed", "issue_id", util.UUIDToString(n.IssueID), "error", err)
			continue
		}
		issueRow, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: n.IssueID, WorkspaceID: workspaceID})
		if err == nil && issueRow.AssigneeID.Valid {
			if _, err := s.TaskService.EnqueueTaskForIssue(ctx, issueRow); err != nil {
				slog.Warn("workflow activate: enqueue node issue task failed", "issue_id", util.UUIDToString(n.IssueID), "error", err)
			}
		}

		_, _ = q.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
			WorkflowID: workflowID,
			NodeID:     n.ID,
			FromStatus: util.StrToText(workflow.StatusBacklog),
			ToStatus:   workflow.StatusTodo,
			ActorType:  actorType,
			ActorID:    actorID,
			Reason:     util.StrToText(reason),
		})
	}
	return activated, nil
}

// OverrideNodeStatus is the Leader/admin manual override (FR5.5). It validates
// the transition against the workflow state machine unless it is a review path,
// requires a reason, and mirrors the status onto the mapped child issue.
func (s *WorkflowService) OverrideNodeStatus(ctx context.Context, workspaceID pgtype.UUID, workflowID pgtype.UUID, nodeID pgtype.UUID, status, reason string, actorType string, actorID pgtype.UUID) (db.WorkflowNode, error) {
	if reason == "" {
		return db.WorkflowNode{}, workflowError(CodeInvalidRequest, "reason is required for manual node status override", nil)
	}
	if !workflow.IsValidNodeStatus(status) {
		return db.WorkflowNode{}, workflowError(CodeInvalidRequest, "invalid node status", nil)
	}
	node, err := s.Queries.GetWorkflowNode(ctx, db.GetWorkflowNodeParams{ID: nodeID, WorkflowID: workflowID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.WorkflowNode{}, workflowError(CodeNotFound, "workflow node not found", nil)
		}
		return db.WorkflowNode{}, err
	}
	wf, err := s.Get(ctx, workspaceID, workflowID)
	if err != nil {
		return db.WorkflowNode{}, err
	}
	if node.Status == status {
		return db.WorkflowNode{}, workflowError(CodeInvalidTransition, "node is already in that status", nil)
	}
	trans := definitionTransitions(wf)
	if !workflow.CanTransition(trans, node.Status, status) {
		return db.WorkflowNode{}, workflowError(CodeInvalidTransition, workflow.TransitionReason(node.Status, status), map[string]string{
			"from": node.Status, "to": status,
		})
	}

	// H-2 (security audit): a review-required node must not be forced to done
	// via the override path — that would let a Leader/admin bypass the human
	// review loop, mirroring the SyncNodeFromIssue write-back guard (P1-1).
	// Reuse the same SyncDoneAllowed + HasApprovedArtifact gate; on refusal we
	// reject and write an audit trail.
	if status == workflow.StatusDone && node.ReviewRequired {
		approved, err := s.Queries.HasApprovedArtifact(ctx, db.HasApprovedArtifactParams{NodeID: node.ID})
		if err != nil {
			return db.WorkflowNode{}, fmt.Errorf("check approved artifact: %w", err)
		}
		if !workflow.SyncDoneAllowed(node.ReviewRequired, approved) {
			_, _ = s.Queries.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
				WorkflowID: workflowID,
				NodeID:     node.ID,
				FromStatus: util.StrToText(node.Status),
				ToStatus:   workflow.StatusDone,
				ActorType:  actorType,
				ActorID:    actorID,
				Reason:     util.StrToText("拒绝覆盖：review_required 节点未经人工审核（无 approved artifact）不能置 done"),
			})
			slog.Warn("workflow override: refused done on review-required node without approved artifact",
				"node_id", util.UUIDToString(node.ID),
				"workflow_id", util.UUIDToString(workflowID))
			return db.WorkflowNode{}, workflowError(CodeInvalidState, "review_required node cannot be set to done without an approved artifact", nil)
		}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkflowNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	updated, err := qtx.UpdateWorkflowNodeStatus(ctx, db.UpdateWorkflowNodeStatusParams{
		ID:         node.ID,
		Status:     status,
		WorkflowID: workflowID,
	})
	if err != nil {
		return db.WorkflowNode{}, err
	}
	_, _ = qtx.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
		WorkflowID: workflowID,
		NodeID:     node.ID,
		FromStatus: util.StrToText(node.Status),
		ToStatus:   status,
		ActorType:  actorType,
		ActorID:    actorID,
		Reason:     util.StrToText(reason),
	})
	// Mirror onto the child issue when the target status is part of the issue
	// status set.
	_, _ = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          node.IssueID,
		Status:      status,
		WorkspaceID: workspaceID,
	})
	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowNode{}, fmt.Errorf("commit override tx: %w", err)
	}

	s.recomputeWorkflowStatus(ctx, wf)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowNodeUpdated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"workflow_id": util.UUIDToString(workflowID),
			"node_id":     util.UUIDToString(node.ID),
			"status":      updated.Status,
		},
	})
	return updated, nil
}

// recomputeWorkflowStatus derives the workflow-instance status from its node
// statuses and persists it. Called after every node write-back.
func (s *WorkflowService) recomputeWorkflowStatus(ctx context.Context, wf db.Workflow) {
	nodes, err := s.Queries.ListWorkflowNodes(ctx, db.ListWorkflowNodesParams{WorkflowID: wf.ID})
	if err != nil {
		slog.Warn("workflow sync: recompute workflow status failed", "workflow_id", util.UUIDToString(wf.ID), "error", err)
		return
	}
	statuses := make([]string, len(nodes))
	for i, n := range nodes {
		statuses[i] = n.Status
	}
	derived := workflow.WorkflowInstanceStatus(statuses)
	if derived == wf.Status {
		return
	}
	updated, err := s.Queries.UpdateWorkflowStatus(ctx, db.UpdateWorkflowStatusParams{
		ID:          wf.ID,
		Status:      derived,
		WorkspaceID: wf.WorkspaceID,
	})
	if err != nil {
		slog.Warn("workflow sync: update workflow status failed", "workflow_id", util.UUIDToString(wf.ID), "error", err)
		return
	}
	_, _ = s.Queries.CreateWorkflowTransitionLog(ctx, db.CreateWorkflowTransitionLogParams{
		WorkflowID: wf.ID,
		FromStatus: util.StrToText(wf.Status),
		ToStatus:   derived,
		ActorType:  "system",
		Reason:     util.StrToText("节点状态派生"),
	})
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowUpdated,
		WorkspaceID: util.UUIDToString(wf.WorkspaceID),
		Payload:     map[string]any{"workflow_id": util.UUIDToString(wf.ID), "status": updated.Status},
	})
}

// notifyReviewer creates an inbox item for the source_issue creator (Q3) so
// they know an artifact / node is awaiting human review.
func (s *WorkflowService) notifyReviewer(ctx context.Context, wf db.Workflow, node db.WorkflowNode) {
	source, err := s.Queries.GetIssue(ctx, wf.SourceIssueID)
	if err != nil {
		slog.Warn("workflow notify: load source issue failed", "workflow_id", util.UUIDToString(wf.ID), "error", err)
		return
	}
	if source.CreatorType != "member" || !source.CreatorID.Valid {
		return
	}
	s.createReviewerInboxItem(ctx, wf, node, source.CreatorID, "流程节点进入待审核："+node.Name,
		fmt.Sprintf("Workflow「%s」的节点「%s」（阶段 %d）已进入待审核状态，请前往审核。", wf.Name, node.Name, node.Stage))
}

// NotifyArtifactReviewRequested notifies the source_issue creator that an
// artifact submission is awaiting human review (Q3). Public so ArtifactService
// can reuse the same inbox path after a submit.
func (s *WorkflowService) NotifyArtifactReviewRequested(ctx context.Context, wf db.Workflow, node db.WorkflowNode, artifact db.Artifact) {
	source, err := s.Queries.GetIssue(ctx, wf.SourceIssueID)
	if err != nil {
		slog.Warn("workflow notify: load source issue failed", "workflow_id", util.UUIDToString(wf.ID), "error", err)
		return
	}
	if source.CreatorType != "member" || !source.CreatorID.Valid {
		return
	}
	s.createReviewerInboxItem(ctx, wf, node, source.CreatorID, "有待审核的 Artifact："+artifact.Title,
		fmt.Sprintf("节点「%s」（阶段 %d）提交了「%s」v%d，请前往审核。", node.Name, node.Stage, artifact.Title, artifact.Version))
}

func (s *WorkflowService) createReviewerInboxItem(ctx context.Context, wf db.Workflow, node db.WorkflowNode, reviewerID pgtype.UUID, title, body string) {
	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   wf.WorkspaceID,
		RecipientType: "member",
		RecipientID:   reviewerID,
		Type:          "workflow_review_requested",
		Severity:      "normal",
		IssueID:       wf.SourceIssueID,
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText("system"),
		ActorID:       pgtype.UUID{},
		Details:       nil,
	})
	if err != nil {
		slog.Warn("workflow notify: create inbox item failed", "error", err)
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: util.UUIDToString(wf.WorkspaceID),
		Payload:     map[string]any{"item": item},
	})
}

// nodesToBySeq converts db nodes to the pure state-machine descriptor.
func nodesToBySeq(nodes []db.WorkflowNode) []workflow.NodeBySeq {
	out := make([]workflow.NodeBySeq, len(nodes))
	for i, n := range nodes {
		out[i] = workflow.NodeBySeq{Seq: n.Seq, Stage: n.Stage, Status: n.Status}
	}
	return out
}

func strToTextOrEmpty(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return util.StrToText(s)
}

func mustParseUUIDOrEmpty(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	u, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}
