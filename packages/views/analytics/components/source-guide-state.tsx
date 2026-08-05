"use client";

// "Data source not connected" guide state (API §0.7 `source_status.ready=false`).
//
// Distinct from a window empty state: the guide state means "this module
// exists but its external data source isn't wired yet" and carries an action
// entry point; an empty state means "connected, just no data this window".
// Rendered from `source_status.ready === false` (design `01` §1).

import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
export interface SourceGuideStateProps {
  /** Lucide icon node (size-8 text-muted-foreground). */
  icon: React.ReactNode;
  /** Badge label above the title — module family ("Git" / "DORA" / "LDAP"). */
  badge: string;
  /** `EmptyTitle` text. */
  title: string;
  /** `EmptyDescription` text — normally `source_status.reason`. */
  description: string;
  /** Rendered when `source_status.updated_at` exists. */
  updatedAt?: string | null;
  /** Optional configure entry point (default: rendered "前往配置"). */
  configureLabel?: string;
  /** Optional navigate target; when omitted the button is inert. */
  onConfigure?: () => void;
  className?: string;
}

export function SourceGuideState({
  icon,
  badge,
  title,
  description,
  updatedAt,
  configureLabel,
  onConfigure,
  className,
}: SourceGuideStateProps) {
  const { t } = useT("analytics");
  return (
    <Empty className={cn("gap-3 py-8", className)}>
      <EmptyHeader>
        <EmptyMedia>{icon}</EmptyMedia>
        <Badge variant="secondary">{badge}</Badge>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent className="gap-3">
        <Button variant="outline" size="sm" onClick={onConfigure}>
          {configureLabel ?? t(($) => $.common.configure)}
        </Button>
        {updatedAt ? (
          <span className="text-[10px] text-muted-foreground">
            {t(($) => $.common.recent_sync, { date: updatedAt.slice(0, 10) })}
          </span>
        ) : null}
      </EmptyContent>
    </Empty>
  );
}
