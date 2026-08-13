"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import {
  FEEDBACK_TYPES,
  useCreateCenterFeedback,
  type FeedbackType,
} from "@multica/core/feedback";
import { useT } from "../i18n";
import { useFeedbackTypeLabels } from "./feedback-types";

const MAX_TITLE_LEN = 200;
const MAX_DESCRIPTION_LEN = 10000;

/**
 * Feedback center submit dialog — type + title + description. The `creator_id`
 * and `workspace_id` are taken from the server-side session/workspace context
 * on POST /api/feedbacks, never from the client.
 */
export function FeedbackSubmitDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("feedback");
  const typeLabels = useFeedbackTypeLabels();
  const queryClient = useQueryClient();
  const mutation = useCreateCenterFeedback();

  const [type, setType] = useState<FeedbackType>("feature");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");

  const canSubmit =
    type.length > 0 &&
    title.trim().length > 0 &&
    title.trim().length <= MAX_TITLE_LEN &&
    description.trim().length > 0 &&
    description.trim().length <= MAX_DESCRIPTION_LEN &&
    !mutation.isPending;

  const reset = () => {
    setType("feature");
    setTitle("");
    setDescription("");
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    const trimmedTitle = title.trim();
    const trimmedDescription = description.trim();
    if (!trimmedTitle) {
      toast.error(t(($) => $.submit_dialog.title_required));
      return;
    }
    if (!trimmedDescription) {
      toast.error(t(($) => $.submit_dialog.description_required));
      return;
    }
    try {
      await mutation.mutateAsync({
        type,
        title: trimmedTitle,
        description: trimmedDescription,
      });
      reset();
      onOpenChange(false);
      toast.success(t(($) => $.submit_dialog.toast_sent));
      queryClient.invalidateQueries({ queryKey: ["feedback"] });
    } catch {
      toast.error(t(($) => $.page.submit_failed));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !mutation.isPending) onOpenChange(false); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.submit_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.page.subtitle)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-body font-medium">
              {t(($) => $.submit_dialog.type_label)}
            </label>
            <div className="flex flex-wrap gap-1.5">
              {FEEDBACK_TYPES.map((value) => (
                <Button
                  key={value}
                  type="button"
                  size="sm"
                  variant={type === value ? "default" : "outline"}
                  className={cn(
                    "h-7 px-2.5 text-caption",
                    type !== value && "text-muted-foreground",
                  )}
                  aria-pressed={type === value}
                  onClick={() => setType(value)}
                >
                  {typeLabels[value]}
                </Button>
              ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="feedback-title" className="text-body font-medium">
              {t(($) => $.submit_dialog.title_label)}
            </label>
            <Input
              id="feedback-title"
              value={title}
              maxLength={MAX_TITLE_LEN}
              placeholder={t(($) => $.submit_dialog.title_placeholder)}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="feedback-description" className="text-body font-medium">
              {t(($) => $.submit_dialog.description_label)}
            </label>
            <Textarea
              id="feedback-description"
              value={description}
              rows={6}
              maxLength={MAX_DESCRIPTION_LEN}
              placeholder={t(($) => $.submit_dialog.description_placeholder)}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={mutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.submit_dialog.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            onClick={handleSubmit}
          >
            {mutation.isPending
              ? t(($) => $.submit_dialog.submitting)
              : t(($) => $.submit_dialog.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
