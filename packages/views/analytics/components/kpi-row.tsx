"use client";

// KPI card row — the shared 4-up `divide-x` grid used by every tab (v1.0
// design `02` §2). Reuses `KpiCard` from the runtimes components so the
// typeface/labels stay consistent across the platform.

import { KpiCard } from "../../runtimes/components/shared";
import { cn } from "@multica/ui/lib/utils";

export interface AnalyticsKpiItem {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  accent?: "brand" | "success" | "default";
}

export function AnalyticsKpiRow({
  items,
  className,
}: {
  items: AnalyticsKpiItem[];
  className?: string;
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4",
        className,
      )}
    >
      {items.map((item) => (
        <KpiCard
          key={item.label}
          label={item.label}
          value={item.value}
          hint={item.hint}
          accent={item.accent}
        />
      ))}
    </div>
  );
}
