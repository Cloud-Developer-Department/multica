"use client";

import type { FeedbackType } from "@multica/core/feedback";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

/** Localized display labels for the four feedback types. */
export function useFeedbackTypeLabels(): Record<FeedbackType, string> {
  const { t } = useT("feedback");
  return {
    bug: t(($) => $.types.bug),
    feature: t(($) => $.types.feature),
    improvement: t(($) => $.types.improvement),
    other: t(($) => $.types.other),
  };
}

/** Muted, non-primary badge so types stay visually quiet on cards/lists. */
export function FeedbackTypeBadge({
  type,
  className,
}: {
  type: FeedbackType;
  className?: string;
}) {
  const labels = useFeedbackTypeLabels();
  return (
    <Badge
      variant="outline"
      className={cn("text-caption font-medium", className)}
    >
      {labels[type] ?? type}
    </Badge>
  );
}
