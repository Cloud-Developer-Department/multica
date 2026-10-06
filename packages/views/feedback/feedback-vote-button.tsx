"use client";

import { ThumbsUp } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  useAddFeedbackVote,
  useRemoveFeedbackVote,
} from "@multica/core/feedback";
import { useT } from "../i18n";

/**
 * Optimistically toggles a user's vote on a feedback item. The mutation is
 * idempotent on the server (UNIQUE(feedback_id, user_id)), so a fast double
 * click cannot create a duplicate vote. Success invalidates the cached list +
 * detail so counts and the `my_vote` flag reconcile with the server; failure
 * rolls the optimistic update back and surfaces a toast.
 */
export function FeedbackVoteButton({
  feedbackId,
  voteCount,
  myVote,
  disabled,
  className,
}: {
  feedbackId: string;
  voteCount: number;
  myVote: boolean;
  disabled?: boolean;
  className?: string;
}) {
  const { t } = useT("feedback");
  const queryClient = useQueryClient();
  const addVote = useAddFeedbackVote();
  const removeVote = useRemoveFeedbackVote();
  const isPending = addVote.isPending || removeVote.isPending;

  const patchFeedback = (id: string, delta: number, voted: boolean) => {
    queryClient.setQueriesData(
      { queryKey: ["feedback"] },
      (old: unknown) => {
        if (!old || typeof old !== "object") return old;
        const any = old as { items?: unknown[]; id?: string; vote_count?: number; my_vote?: boolean };
        // List page payload { items, total, ... } — patch the matching row.
        if (Array.isArray(any.items)) {
          return {
            ...any,
            items: any.items.map((it) =>
              it && typeof it === "object" && (it as { id?: string }).id === id
                ? {
                    ...it,
                    vote_count: Math.max(0, ((it as { vote_count?: number }).vote_count ?? 0) + delta),
                    my_vote: voted,
                  }
                : it,
            ),
          };
        }
        // Detail payload — patch in place.
        if (any.id === id) {
          return {
            ...any,
            vote_count: Math.max(0, (any.vote_count ?? 0) + delta),
            my_vote: voted,
          };
        }
        return old;
      },
    );
  };

  const toggle = () => {
    const delta = myVote ? -1 : 1;
    const voted = !myVote;
    // Optimistic flip before the request so the button feels instant.
    patchFeedback(feedbackId, delta, voted);
    const mutation = myVote ? removeVote : addVote;
    mutation.mutate(feedbackId, {
      onError: () => {
        // Roll back to the pre-click state.
        patchFeedback(feedbackId, -delta, myVote);
        toast.error(t(($) => $.vote.toggle_failed));
      },
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: ["feedback"] });
      },
    });
  };

  return (
    <Button
      type="button"
      size="sm"
      variant={myVote ? "default" : "outline"}
      className={className}
      disabled={disabled || isPending}
      aria-pressed={myVote}
      onClick={toggle}
    >
      <ThumbsUp className="size-3.5" />
      <span className="tabular-nums">{voteCount}</span>
      <span className="hidden sm:inline">
        {myVote ? t(($) => $.vote.supported) : t(($) => $.vote.support)}
      </span>
    </Button>
  );
}
