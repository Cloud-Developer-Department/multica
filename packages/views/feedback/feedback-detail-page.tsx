"use client";

import { ArrowLeft, MessageSquare } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { feedbackDetailOptions } from "@multica/core/feedback";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import { CollectionPageState } from "../layout/collection-page";
import { FeedbackTypeBadge } from "./feedback-types";
import { FeedbackVoteButton } from "./feedback-vote-button";
import { FeedbackComments } from "./feedback-comments";

export function FeedbackDetailPage({ feedbackId }: { feedbackId: string }) {
  const { t } = useT("feedback");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const user = useAuthStore((s) => s.user);

  const { data: feedback, isLoading, isError, error, refetch } = useQuery({
    ...feedbackDetailOptions(wsId, feedbackId),
    enabled: !!wsId && !!feedbackId,
  });

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col gap-4 px-5 py-4">
        <Skeleton className="h-4 w-32" />
        <div className="flex items-center gap-2">
          <Skeleton className="size-6 rounded-full" />
          <Skeleton className="h-5 w-56" />
        </div>
        <Skeleton className="h-4 w-40" />
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      </div>
    );
  }

  if (isError) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <CollectionPageState
        icon={MessageSquare}
        title={notFound ? t(($) => $.page.not_found) : t(($) => $.page.load_failed)}
        description={notFound ? t(($) => $.page.not_found_description) : undefined}
        tone={notFound ? "muted" : "destructive"}
        actions={
          <Button size="sm" variant="outline" onClick={() => refetch()}>
            {t(($) => $.page.reload)}
          </Button>
        }
      />
    );
  }

  if (!feedback) return null;

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-1 min-h-0 flex-col gap-4 px-5 py-4">
      <AppLink
        href={wsPaths.feedback()}
        className="inline-flex w-fit items-center gap-1 text-caption text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="size-3.5" />
        {t(($) => $.page.back)}
      </AppLink>

      <div className="flex flex-wrap items-start gap-3">
        <FeedbackVoteButton
          feedbackId={feedback.id}
          voteCount={feedback.vote_count}
          myVote={feedback.my_vote}
          disabled={!user}
          className="shrink-0"
        />
        <div className="min-w-0 flex-1">
          <h1 className="break-words text-title font-semibold">{feedback.title}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <FeedbackTypeBadge type={feedback.type} />
            <span className="min-w-0 truncate">{feedback.creator_name || feedback.creator_id}</span>
            <span aria-hidden="true">·</span>
            <span className="shrink-0">{timeAgo(feedback.created_at)}</span>
          </div>
        </div>
      </div>

      {feedback.description ? (
        <div className="whitespace-pre-wrap break-words rounded-md border bg-card p-4 text-body text-muted-foreground">
          {feedback.description}
        </div>
      ) : null}

      <div className="h-px bg-border" />

      <FeedbackComments feedbackId={feedback.id} />
    </div>
  );
}
