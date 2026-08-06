export type ArtifactType =
  | "requirements"
  | "architecture"
  | "development"
  | "testing"
  | "code_review"
  | "security"
  | "documentation"
  | "deployment"
  | "other";

export type ArtifactStatus =
  | "draft"
  | "submitted"
  | "approved"
  | "rejected"
  | "superseded";

export type ArtifactContentType = "markdown" | "json" | "text" | "file";

export interface Artifact {
  id: string;
  workspace_id: string;
  workflow_id: string;
  node_id: string;
  issue_id: string | null;
  type: ArtifactType;
  title: string;
  content: string | null;
  content_type: ArtifactContentType;
  file_attachment_id: string | null;
  version: number;
  status: ArtifactStatus;
  author_type: "member" | "agent";
  author_id: string;
  created_at: string;
  updated_at: string;
  latest_version?: { version: number; status: ArtifactStatus } | null;
  review_required?: boolean;
}

export interface ArtifactListResponse {
  items: Artifact[];
  total: number;
}

export interface ArtifactVersion {
  id: string;
  version: number;
  status: ArtifactStatus;
  title: string;
  content_type: ArtifactContentType;
  created_at: string;
}

export interface ArtifactVersionsResponse {
  node_id: string;
  type: ArtifactType;
  items: ArtifactVersion[];
}

export interface DiffLine {
  line: number;
  op: "+" | "-" | "=";
  text: string;
}

export interface ArtifactDiffResponse {
  artifact_id: string;
  from_version: number;
  to_version: number;
  content_type: ArtifactContentType;
  diff: DiffLine[];
  summary: { added: number; removed: number; changed: number };
}

export type ReviewAction = "approved" | "rejected";

export interface ReviewArtifactRequest {
  action: ReviewAction;
  comment?: string;
}

export interface ReviewArtifactResponse {
  artifact_id: string;
  action: ReviewAction;
  comment: string;
  artifact_status: ArtifactStatus;
  node: { id: string; status: string };
  workflow_status: string;
  advanced: boolean;
  next_stage: number;
  reviewed_at: string;
}

export interface ArtifactReview {
  id: string;
  artifact_id: string;
  action: ReviewAction;
  comment: string | null;
  reviewer_type: "member";
  reviewer_id: string;
  created_at: string;
}

export interface ArtifactReviewsResponse {
  items: ArtifactReview[];
}

export interface ReviewQueueItem {
  artifact_id: string;
  workflow_id: string;
  workflow_name?: string;
  node_id: string;
  node_name?: string;
  stage?: number;
  type: ArtifactType;
  title: string;
  version: number;
  content_type: ArtifactContentType;
  file_attachment_id: string | null;
  author_type: "member" | "agent";
  author_id: string;
  created_at: string;
}

export interface ReviewQueueResponse {
  items: ReviewQueueItem[];
  total: number;
}

export interface ArtifactStats {
  workspace_id: string;
  range: { from: string; to: string };
  total_submitted: number;
  approved: number;
  rejected: number;
  pending: number;
  approval_rate: number;
  avg_review_duration_ms: number;
  by_type: {
    type: ArtifactType;
    submitted: number;
    approval_rate: number;
    avg_review_duration_ms: number;
  }[];
}

export interface CreateArtifactRequest {
  workspace_id: string;
  workflow_id: string;
  node_id: string;
  issue_id?: string;
  type: ArtifactType;
  title: string;
  content?: string;
  content_type?: ArtifactContentType;
  file_attachment_id?: string;
}
