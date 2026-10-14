"use client";

import { MessageCircle, ThumbsUp } from "lucide-react";
import type { FeedbackSummary } from "@multica/core/feedback";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import { FeedbackTypeBadge } from "./feedback-types";
import { cn } from "@multica/ui/lib/utils";

/** Truncated description preview for list cards. */
const SUMMARY_LENGTH = 140;

function truncate(text: string, max: number): string {
  const trimmed = text.trim();
  if (trimmed.length <= max) return trimmed;
  return `${trimmed.slice(0, max).trimEnd()}…`;
}

/**
 * Feedback list card. Shows votes, title, description preview, type, creator,
 * creation time and comment count. The whole card navigates to the detail page.
 */
export function FeedbackCard({
  feedback,
}: {
  feedback: FeedbackSummary;
}) {
  const wsPaths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const { t } = useT("feedback");

  return (
    <AppLink
      href={wsPaths.feedbackDetail(feedback.id)}
      className={cn(
        "group/card flex flex-col gap-1.5 rounded-md border bg-card p-3 transition-colors",
        "hover:border-primary/50 hover:bg-accent/30",
      )}
    >
      <div className="flex items-start gap-2">
        <span className="mt-0.5 inline-flex shrink-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-caption tabular-nums text-muted-foreground">
          <ThumbsUp className="size-3" />
          {feedback.vote_count}
        </span>
        <h3 className="min-w-0 flex-1 truncate text-body font-medium">
          {feedback.title}
        </h3>
        <FeedbackTypeBadge type={feedback.type} className="shrink-0" />
      </div>

      {feedback.description ? (
        <p className="line-clamp-2 text-caption text-muted-foreground">
          {truncate(feedback.description, SUMMARY_LENGTH)}
        </p>
      ) : null}

      <div className="mt-1 flex items-center gap-1.5 text-caption text-muted-foreground">
        <span className="min-w-0 truncate">{feedback.creator_name || feedback.creator_id}</span>
        <span aria-hidden="true">·</span>
        <span className="shrink-0">{timeAgo(feedback.created_at)}</span>
        <span aria-hidden="true">·</span>
        <span className="inline-flex shrink-0 items-center gap-1">
          <MessageCircle className="size-3" />
          {t(($) => $.page.comment_count, { count: feedback.comment_count })}
        </span>
      </div>
    </AppLink>
  );
}
