import type { IssueStatus } from "./issue";

export type WorkflowStatus = IssueStatus;

export type WorkflowNodeType =
  | "requirements"
  | "architecture"
  | "development"
  | "testing"
  | "code_review"
  | "security"
  | "documentation"
  | "deployment"
  | "other";

export type WorkflowNodeStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type ArtifactReviewStatus = "none" | "pending" | "approved" | "rejected";

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  stage: number;
  seq: number;
  type: WorkflowNodeType;
  name: string;
  status: WorkflowNodeStatus;
  issue_id: string;
  assignee_type: "member" | "agent" | "squad" | null;
  assignee_id: string | null;
  review_required: boolean;
  artifact_review_status: ArtifactReviewStatus;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowStage {
  stage: number;
  name: string;
  status: WorkflowStatus;
  nodes: WorkflowNode[];
}

export interface WorkflowProgress {
  total_nodes: number;
  done_nodes: number;
  blocked_nodes: number;
  in_review_nodes: number;
}

export interface Workflow {
  id: string;
  workspace_id: string;
  source_issue_id: string;
  name: string;
  description: string | null;
  status: WorkflowStatus;
  current_stage: number;
  created_by_type: "member" | "agent";
  created_by_id: string;
  created_at: string;
  updated_at: string;
  stages?: WorkflowStage[];
  progress?: WorkflowProgress;
}

export interface WorkflowListResponse {
  items: Workflow[];
  total: number;
}

export interface WorkflowTransition {
  id: string;
  workflow_id: string;
  node_id: string | null;
  from_status: string | null;
  to_status: string;
  actor_type: "member" | "agent" | "system";
  actor_id: string | null;
  reason: string | null;
  created_at: string;
}

export interface WorkflowTransitionsResponse {
  items: WorkflowTransition[];
  total: number;
}

export interface CreateWorkflowRequest {
  workspace_id: string;
  source_issue_id: string;
  name: string;
  description?: string;
  template_key?: string;
  customizations?: {
    nodes?: {
      type: WorkflowNodeType;
      assignee_type?: "member" | "agent" | "squad";
      assignee_id?: string;
      review_required?: boolean;
    }[];
  };
}

export interface AdvanceWorkflowResponse {
  id: string;
  current_stage: number;
  status: WorkflowStatus;
  advanced: boolean;
}
