"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquareText, Send } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Avatar, AvatarImage, AvatarFallback } from "@multica/ui/components/ui/avatar";
import { feedbackCommentsOptions, feedbackKeys, useCreateFeedbackComment } from "@multica/core/feedback";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT, useTimeAgo } from "../i18n";
import { CollectionPageState } from "../layout/collection-page";

const MAX_COMMENT_LEN = 5000;

function CommentAvatar({ name, avatarUrl }: { name: string; avatarUrl: string | null }) {
  const initials = (name || "?").split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase();
  const url = resolvePublicFileUrl(avatarUrl);
  return (
    <Avatar size="sm">
      {url ? (
        <AvatarImage src={url} alt="" />
      ) : null}
      <AvatarFallback>{initials}</AvatarFallback>
    </Avatar>
  );
}

export function FeedbackComments({ feedbackId }: { feedbackId: string }) {
  const { t } = useT("feedback");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const user = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");

  const { data: comments = [], isLoading, isError, refetch } = useQuery({
    ...feedbackCommentsOptions(wsId, feedbackId),
    enabled: !!wsId,
  });
  const createComment = useCreateFeedbackComment();

  const canSend = draft.trim().length > 0 && draft.trim().length <= MAX_COMMENT_LEN && !createComment.isPending;

  const handleSend = async () => {
    const content = draft.trim();
    if (!content || !canSend) return;
    try {
      await createComment.mutateAsync({ feedbackId, content });
      setDraft("");
      await queryClient.invalidateQueries({ queryKey: feedbackKeys.comments(wsId, feedbackId) });
      // Comment count on the detail/list rows changes too.
      await queryClient.invalidateQueries({ queryKey: ["feedback", wsId, "detail", feedbackId] });
      await queryClient.invalidateQueries({ queryKey: ["feedback", wsId, "list"] });
    } catch {
      toast.error(t(($) => $.page.comment_failed));
    }
  };

  return (
    <section className="space-y-3">
      <h2 className="text-body font-medium">{t(($) => $.comments.title)}</h2>

      {isError ? (
        <CollectionPageState
          icon={MessageSquareText}
          title={t(($) => $.page.load_failed)}
          tone="destructive"
          actions={
            <Button size="sm" variant="outline" onClick={() => refetch()}>
              {t(($) => $.page.reload)}
            </Button>
          }
        />
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="flex gap-2">
              <Skeleton className="size-6 rounded-full" />
              <div className="flex-1 space-y-1">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-3 w-full" />
              </div>
            </div>
          ))}
        </div>
      ) : comments.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.comments.empty)}</p>
      ) : (
        <ul className="space-y-3">
          {comments.map((comment) => (
            <li key={comment.id} className="flex gap-2">
              <CommentAvatar name={comment.user_name} avatarUrl={comment.user_avatar_url} />
              <div className="min-w-0 flex-1 rounded-md bg-muted/50 px-2.5 py-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-caption font-medium">
                    {comment.user_name || comment.user_id}
                  </span>
                  <span aria-hidden="true" className="text-faint-foreground">·</span>
                  <span className="text-caption text-muted-foreground">
                    {timeAgo(comment.created_at)}
                  </span>
                </div>
                <p className="mt-0.5 break-words whitespace-pre-wrap text-body text-muted-foreground">
                  {comment.content}
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}

      {user ? (
        <div className="flex items-end gap-2">
          <Textarea
            value={draft}
            rows={2}
            maxLength={MAX_COMMENT_LEN}
            placeholder={t(($) => $.comments.placeholder)}
            className="min-h-12 flex-1 resize-none"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                void handleSend();
              }
            }}
          />
          <Button
            type="button"
            size="sm"
            disabled={!canSend}
            onClick={handleSend}
            aria-label={t(($) => $.comments.send)}
          >
            <Send className="size-3.5" />
            <span className="hidden sm:inline">{t(($) => $.comments.send)}</span>
          </Button>
        </div>
      ) : null}
    </section>
  );
}
