export const FEEDBACK_KINDS = ["bug", "feature", "general", "praise"] as const;

export type FeedbackKind = (typeof FEEDBACK_KINDS)[number];

/** Feedback center types (the four first-release categories). */
export const FEEDBACK_TYPES = ["bug", "feature", "improvement", "other"] as const;

export type FeedbackType = (typeof FEEDBACK_TYPES)[number];

export type FeedbackSort = "latest" | "hot" | "comments";

/** List item shown on the feedback center page (includes aggregate counts). */
export interface FeedbackSummary {
  id: string;
  workspace_id: string;
  creator_id: string;
  creator_name: string;
  creator_avatar_url: string | null;
  title: string;
  description: string;
  type: FeedbackType;
  vote_count: number;
  comment_count: number;
  my_vote: boolean;
  created_at: string;
  updated_at: string;
}

/** Full feedback detail (same shape as the summary today). */
export interface Feedback extends FeedbackSummary {}

/** A comment on a feedback item. */
export interface FeedbackComment {
  id: string;
  feedback_id: string;
  user_id: string;
  user_name: string;
  user_avatar_url: string | null;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface ListFeedbacksParams {
  type?: FeedbackType;
  keyword?: string;
  sort?: FeedbackSort;
  page?: number;
  page_size?: number;
}

export interface ListFeedbacksResponse {
  items: FeedbackSummary[];
  total: number;
  page: number;
  page_size: number;
  has_more: boolean;
}

export interface CreateCenterFeedbackRequest {
  type: FeedbackType;
  title: string;
  description: string;
}

export interface CreateFeedbackCommentRequest {
  content: string;
}

export interface FeedbackErrorContext {
  name: string;
  message: string;
  stack?: string;
}

export interface DesktopRouteErrorFeedbackContext {
  kind: "desktop_route_error";
  trigger: string;
  error: FeedbackErrorContext;
}

export type FeedbackContext = DesktopRouteErrorFeedbackContext;

export function isFeedbackContext(value: unknown): value is FeedbackContext {
  if (!value || typeof value !== "object") return false;
  const context = value as Record<string, unknown>;
  if (
    context.kind !== "desktop_route_error" ||
    typeof context.trigger !== "string" ||
    !context.error ||
    typeof context.error !== "object"
  ) {
    return false;
  }
  const error = context.error as Record<string, unknown>;
  return (
    typeof error.name === "string" &&
    typeof error.message === "string" &&
    (error.stack === undefined || typeof error.stack === "string")
  );
}

export interface CreateFeedbackResponse {
  id: string;
  created_at: string;
}
