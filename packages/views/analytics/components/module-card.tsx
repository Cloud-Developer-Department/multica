"use client";

// Module card shell for the analytics platform: title header + per-module
// loading / error / empty handling (E8 — modules fail independently, never
// block the page). Every analytics module renders through this so the four
// states are visually consistent and any module's retry only refires its own
// request.

import { TriangleAlert } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Empty, EmptyContent, EmptyDescription, EmptyMedia, EmptyTitle } from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
export interface AnalyticsModuleCardProps {
  title: string;
  description?: string;
  /** Header right side — segmented control / select / badges. */
  action?: React.ReactNode;
  /** Optional right-aligned meta line under the title. */
  headerNote?: React.ReactNode;
  loading: boolean;
  error: boolean;
  /** True when the module has no data this window (renders empty state). */
  empty: boolean;
  emptyIcon?: React.ReactNode;
  emptyTitle?: string;
  emptyDescription?: string;
  /** When `guide` is provided the module renders the guide state instead of
   *  empty/loading (used for source_status.ready=false modules). */
  guide?: React.ReactNode;
  onRetry?: () => void;
  children?: React.ReactNode;
  className?: string;
}

export function AnalyticsModuleCard({
  title,
  description,
  action,
  headerNote,
  loading,
  error,
  empty,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  guide,
  onRetry,
  children,
  className,
}: AnalyticsModuleCardProps) {
  const { t } = useT("analytics");

  const body = (() => {
    if (guide) return guide;
    if (error) {
      return (
        <div className="flex flex-col items-center justify-center gap-3 py-8 text-center">
          <TriangleAlert className="size-5 text-destructive" />
          <p className="text-sm font-medium">{t(($) => $.common.load_failed)}</p>
          {onRetry ? (
            <Button variant="outline" size="sm" onClick={onRetry}>
              {t(($) => $.common.retry)}
            </Button>
          ) : null}
        </div>
      );
    }
    if (loading) {
      return (
        <div className="flex min-h-40 items-center justify-center py-8">
          <Spinner className="size-4" />
        </div>
      );
    }
    if (empty) {
      return (
        <Empty className="py-8">
          <EmptyContent className="gap-2">
            <EmptyMedia>{emptyIcon}</EmptyMedia>
            {emptyTitle ? <EmptyTitle>{emptyTitle}</EmptyTitle> : null}
            {emptyDescription ? <EmptyDescription>{emptyDescription}</EmptyDescription> : null}
          </EmptyContent>
        </Empty>
      );
    }
    return children;
  })();

  return (
    <Card className={cn("min-w-0", className)}>
      <CardHeader>
        <div className="min-w-0">
          <CardTitle>{title}</CardTitle>
          {description ? <CardDescription>{description}</CardDescription> : null}
        </div>
        {(action || headerNote) && (
          <CardAction>
            {headerNote}
            {action}
          </CardAction>
        )}
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
