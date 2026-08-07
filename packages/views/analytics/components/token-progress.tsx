"use client";

// Token-aware progress bar. The shadcn `Progress` wrapper always renders its
// indicator in `bg-primary`, which can't express the chart/success colours the
// analytics design calls for (`bg-brand` / `bg-chart-2` / `bg-success` /
// `bg-chart-4`). This is the same div-based bar the dashboard leaderboard uses
// (`dashboard-page.tsx`), promoted here so every analytics module shares one
// implementation. No hard-coded colors — the indicator class comes from the
// caller and must be a token class.

import { cn } from "@multica/ui/lib/utils";

export function TokenProgress({
  value,
  indicatorClassName = "bg-chart-1",
  className,
}: {
  /** 0–100 */
  value: number;
  indicatorClassName?: string;
  className?: string;
}) {
  const clamped = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
  return (
    <div
      role="progressbar"
      aria-valuenow={Math.round(clamped)}
      aria-valuemin={0}
      aria-valuemax={100}
      className={cn("h-1.5 w-full overflow-hidden rounded-full bg-muted", className)}
    >
      <div
        className={cn("h-full rounded-full transition-[width] duration-300 ease-out", indicatorClassName)}
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
}
