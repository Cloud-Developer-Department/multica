import type {
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
} from "./issue";

/**
 * Issue templates (CLO-159) — workspace-scoped presets that pre-fill an
 * issue's fields on creation. The CRUD surface mirrors label / property:
 * any workspace member can read, writes are owner/admin-gated at the route
 * level (same group as labels). Preset templates (`is_preset = true`) are
 * seeded per workspace and protected from deletion, but owners may edit
 * their content.
 *
 * `label_ids` is a JSONB array of UUID strings referencing issue-scoped
 * labels in the same workspace. The backend validates each on write so a
 * template never references a foreign or deleted label.
 */
export interface IssueTemplate {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  /** Prefix or full title preset applied to the new issue's title. */
  title_template: string;
  /** Markdown body preset applied to the new issue's description. */
  body_template: string;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  project_id: string | null;
  /** Ordered stage (>= 1) grouping the new sub-issue under its parent. */
  stage: number | null;
  /** Issue-scoped label IDs to attach on creation. */
  label_ids: string[];
  /** Lucide icon glyph name (e.g. "sparkles", "bug"). Display-only. */
  icon: string;
  /** Free-form grouping (e.g. "engineering", "planning"). Display-only. */
  category: string;
  /** Seeded built-in template; protected from deletion. */
  is_preset: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface CreateIssueTemplateRequest {
  name: string;
  description?: string;
  title_template?: string;
  body_template?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  project_id?: string | null;
  stage?: number | null;
  label_ids?: string[];
  icon?: string;
  category?: string;
}

export interface UpdateIssueTemplateRequest {
  name?: string;
  description?: string;
  title_template?: string;
  body_template?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  project_id?: string | null;
  stage?: number | null;
  label_ids?: string[];
  icon?: string;
  category?: string;
}

export interface ListIssueTemplatesResponse {
  issue_templates: IssueTemplate[];
  total: number;
}
