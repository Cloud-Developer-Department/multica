// Package workflow implements the pure-logic state machine that governs
// Workflow node transitions and stage barriers for the AI software R&D
// pipeline (CLO-146).
//
// It has no database or HTTP dependencies by design: the legal-transition
// table and the stage-barrier calculation are plain data-driven functions so
// they can be unit-tested independently (see state_machine_test.go) and used
// from both the service layer and the UpdateIssue sync hook.
package workflow

import (
	"fmt"
	"sort"
)

// NodeStatus reuses the platform issue status set (Q1). 'backlog' is used for
// "not yet activated" nodes waiting for their stage to open.
const (
	StatusBacklog    = "backlog"
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusInReview   = "in_review"
	StatusDone       = "done"
	StatusBlocked    = "blocked"
	StatusCancelled  = "cancelled"
)

// ValidNodeStatuses is the node status set, shared with the DB CHECK
// constraint on workflow_node.status.
var ValidNodeStatuses = []string{
	StatusBacklog,
	StatusTodo,
	StatusInProgress,
	StatusInReview,
	StatusDone,
	StatusBlocked,
	StatusCancelled,
}

// WorkflowStatus mirrors the issue status set for the workflow instance
// itself (Q1), with 'in_review' meaning "a node is awaiting human review".
const (
	WorkflowStatusTodo       = "todo"
	WorkflowStatusInProgress = "in_progress"
	WorkflowStatusInReview   = "in_review"
	WorkflowStatusDone       = "done"
	WorkflowStatusBlocked    = "blocked"
	WorkflowStatusCancelled  = "cancelled"
)

// defaultTransitions is the node-level legal transition table described in
// architecture 3.2. Every workflow's definition may override it per-instance
// (definition.transitions), falling back to this table for missing keys.
var defaultTransitions = map[string][]string{
	StatusBacklog:    {StatusTodo},
	StatusTodo:       {StatusInProgress, StatusBlocked, StatusCancelled},
	StatusInProgress: {StatusInReview, StatusDone, StatusBlocked, StatusCancelled},
	StatusInReview:   {StatusDone, StatusTodo, StatusBlocked},
	StatusBlocked:    {StatusTodo, StatusInProgress, StatusCancelled},
	StatusDone:       {},
	StatusCancelled:  {},
}

// Transitions returns the legal destination statuses from `from` for a
// workflow whose definition carries the given transition map. A nil or empty
// definition transitions map falls back to the default table. Unknown source
// statuses are treated as having no legal transitions.
func Transitions(definitionTransitions map[string][]string, from string) []string {
	table := defaultTransitions
	if definitionTransitions != nil {
		table = definitionTransitions
	}
	dsts, ok := table[from]
	if !ok {
		return nil
	}
	out := make([]string, len(dsts))
	copy(out, dsts)
	return out
}

// CanTransition reports whether `from -> to` is legal under the workflow's
// transition rules. The architecture allows 'in_review -> done' only through
// an artifact review approval and 'in_review -> todo' only through a review
// rejection; those enforcement points live in the ArtifactService, while the
// transition table itself permits both so the pure package stays reusable.
func CanTransition(definitionTransitions map[string][]string, from, to string) bool {
	for _, dst := range Transitions(definitionTransitions, from) {
		if dst == to {
			return true
		}
	}
	return false
}

// SyncDoneAllowed reports whether a node may mirror a 'done' status arriving
// from its mapped issue update (Q4 write-back). Review-free nodes finish
// directly; a review-required node must already carry an approved artifact,
// otherwise an agent could flip its child issue straight to done and bypass
// the human review loop (AC-REV). The approved-artifact existence check is
// performed by the caller (SyncNodeFromIssue); this function keeps the
// decision pure so it can be unit-tested here.
func SyncDoneAllowed(reviewRequired, hasApprovedArtifact bool) bool {
	if !reviewRequired {
		return true
	}
	return hasApprovedArtifact
}

// TransitionReason returns a human-readable explanation for an illegal node
// transition, used for the 'invalid_transition' error detail (AC-W2).
func TransitionReason(from, to string) string {
	return fmt.Sprintf("节点状态不允许从 %s 迁移到 %s", from, to)
}

// Stage nodes statuses. These are the statuses a node may carry while it is
// part of an open (not yet completed) stage.
func isTerminal(status string) bool {
	return status == StatusDone || status == StatusCancelled
}

// StageStatus computes the aggregated status of a stage given the statuses of
// its nodes:
//   - all nodes terminal (done/cancelled) -> "done" (barrier closed)
//   - any node blocked                       -> "blocked"
//   - any node in_review                     -> "in_review"
//   - any node in_progress or todo           -> "in_progress"
//   - otherwise (all backlog, none active)   -> "todo"
func StageStatus(nodeStatuses []string) string {
	total := 0
	terminal := 0
	anyBlocked := false
	anyReview := false
	anyActive := false
	for _, s := range nodeStatuses {
		total++
		if isTerminal(s) {
			terminal++
		}
		switch s {
		case StatusBlocked:
			anyBlocked = true
		case StatusInReview:
			anyReview = true
		case StatusInProgress, StatusTodo:
			anyActive = true
		}
	}
	if total > 0 && terminal == total {
		return "done"
	}
	if anyBlocked {
		return "blocked"
	}
	if anyReview {
		return "in_review"
	}
	if anyActive {
		return "in_progress"
	}
	return "todo"
}

// NodeBySeq is a helper descriptor for stage-barrier evaluation.
type NodeBySeq struct {
	Seq    int32
	Stage  int32
	Status string
}

// StageBarrierClosed reports whether the given stage is closed — every node
// in that stage is terminal (done or cancelled). The caller passes all nodes
// of the workflow; only nodes with Stage == stage participate.
func StageBarrierClosed(nodes []NodeBySeq, stage int32) bool {
	any := false
	for _, n := range nodes {
		if n.Stage != stage {
			continue
		}
		any = true
		if !isTerminal(n.Status) {
			return false
		}
	}
	return any
}

// NextStageToOpen returns the smallest stage > currentStage that still has
// non-terminal nodes (a stage to activate after currentStage's barrier
// closes), or 0 when no later stage remains. A stage with only terminal nodes
// is skipped so advancement jumps past already-completed stages.
func NextStageToOpen(nodes []NodeBySeq, currentStage int32) int32 {
	open := map[int32]bool{}
	for _, n := range nodes {
		if n.Stage <= currentStage {
			continue
		}
		if !isTerminal(n.Status) {
			open[n.Stage] = true
		}
	}
	if len(open) == 0 {
		return 0
	}
	stages := make([]int32, 0, len(open))
	for s := range open {
		stages = append(stages, s)
	}
	sort.Slice(stages, func(i, j int) bool { return stages[i] < stages[j] })
	return stages[0]
}

// WorkflowInstanceStatus derives the workflow-instance status from its node
// statuses and the current stage (see architecture 3.1):
//   - no nodes at all               -> "todo"
//   - all nodes terminal            -> "done"
//   - any node blocked              -> "blocked"
//   - any node in_review            -> "in_review"
//   - any active node               -> "in_progress"
//   - otherwise                     -> "todo"
func WorkflowInstanceStatus(nodeStatuses []string) string {
	total := 0
	terminal := 0
	anyBlocked := false
	anyReview := false
	anyActive := false
	for _, s := range nodeStatuses {
		total++
		if isTerminal(s) {
			terminal++
		}
		switch s {
		case StatusBlocked:
			anyBlocked = true
		case StatusInReview:
			anyReview = true
		case StatusInProgress, StatusTodo:
			anyActive = true
		}
	}
	if total == 0 {
		return WorkflowStatusTodo
	}
	if total > 0 && terminal == total {
		return WorkflowStatusDone
	}
	if anyBlocked {
		return WorkflowStatusBlocked
	}
	if anyReview {
		return WorkflowStatusInReview
	}
	if anyActive {
		return WorkflowStatusInProgress
	}
	return WorkflowStatusTodo
}

// IsValidNodeStatus reports whether status is in the node status set.
func IsValidNodeStatus(status string) bool {
	for _, s := range ValidNodeStatuses {
		if s == status {
			return true
		}
	}
	return false
}
