"use client";

// The six data-module visualisations for the monitoring dashboard. Four are
// Recharts (donut, stacked bar, area, dual line); the two Top-N lists are CSS
// progress bars (same leaderboard language as the Usage page — a horizontal
// bar per ranked entity, no SVG overhead). All colours follow the design-spec
// §2.1 status palette (see utils.ts) / §5 trend colours.

import * as React from "react";
import {
  PieChart,
  Pie,
  Cell,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  AreaChart,
  Area,
  LineChart,
  Line,
  Legend,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  ChartLegendContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import { useT } from "../../i18n";
import type {
  MonitoringCommentPoint,
  MonitoringCompletionPoint,
} from "@multica/core/types";
import {
  STATUS_DISPLAY_ORDER,
  PROJECT_STACK_ORDER,
  STATUS_CHART_COLOR,
  type ProjectStackRow,
  type StatusSlice,
} from "./utils";
import { AppLink } from "../../navigation";

// ---------------------------------------------------------------------------
// Issue status distribution — donut
// ---------------------------------------------------------------------------

export function issueDonutConfig(
  statusLabel: (status: string) => string,
): ChartConfig {
  const config: ChartConfig = {};
  for (const status of STATUS_DISPLAY_ORDER) {
    config[status] = {
      label: statusLabel(status),
      color: STATUS_CHART_COLOR[status] ?? "#94A3B8",
    };
  }
  return config;
}

export function IssueDistributionDonut({
  slices,
  total,
  config,
}: {
  slices: StatusSlice[];
  total: number;
  config: ChartConfig;
}) {
  const { t } = useT("monitoring");
  return (
    <div className="relative h-[260px] w-full">
      <ChartContainer config={config} className="h-full w-full">
        <PieChart>
          <Pie
            data={slices}
            dataKey="count"
            nameKey="status"
            innerRadius="62%"
            outerRadius="100%"
            paddingAngle={2}
            strokeWidth={0}
          >
            {slices.map((slice) => (
              <Cell key={slice.status} fill={slice.color} />
            ))}
          </Pie>
          <ChartTooltip
            content={
              <ChartTooltipContent
                formatter={(value, _name, _item, _index) => {
                  const count = typeof value === "number" ? value : 0;
                  return `${count} · ${total > 0 ? Math.round((count / total) * 100) : 0}%`;
                }}
              />
            }
          />
        </PieChart>
      </ChartContainer>
      {/* Centre readout — "total issues" (design-spec §5.1). */}
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-xs text-muted-foreground">{t(($) => $.donut.total_label)}</span>
        <span className="text-2xl font-bold tabular-nums">{total.toLocaleString()}</span>
      </div>
    </div>
  );
}

export function DonutLegend({
  slices,
  total,
  statusLabel,
}: {
  slices: StatusSlice[];
  total: number;
  statusLabel: (status: string) => string;
}) {
  return (
    <ul className="mt-2 flex flex-wrap items-center justify-center gap-x-4 gap-y-1.5">
      {slices.map((slice) => (
        <li key={slice.status} className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="h-2 w-2 shrink-0 rounded-full"
            style={{ backgroundColor: slice.color }}
          />
          <span className="text-xs">{statusLabel(slice.status)}</span>
          <span className="text-xs tabular-nums text-muted-foreground">
            {slice.count}
          </span>
          <span className="text-xs tabular-nums text-muted-foreground">
            {total > 0 ? `${Math.round((slice.count / total) * 100)}%` : "0%"}
          </span>
        </li>
      ))}
    </ul>
  );
}

// ---------------------------------------------------------------------------
// Project progress — stacked bar
// ---------------------------------------------------------------------------

export function ProjectProgressStackedBar({
  rows,
  config,
}: {
  rows: ProjectStackRow[];
  config: ChartConfig;
}) {
  // Recharts Bar reads its dataKey from the row's top level, but the counts
  // live in `row.counts` — flatten them so each status stacks as its own
  // series (regression caught in stage-4 review: bars rendered at 0 height).
  const data = rows.map((row) => ({ name: row.name, ...row.counts }));
  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="name"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} width={42} />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Legend content={<ChartLegendContent className="flex-wrap" />} />
        {/* Bottom-up stack order per design-spec §5.2: done at the base →
            in_progress → in_review → todo → blocked → cancelled on top. */}
        {PROJECT_STACK_ORDER.map((status, index) => {
          const show = rows.some((r) => (r.counts[status] ?? 0) > 0);
          if (!show) return null;
          const isLast = index === PROJECT_STACK_ORDER.length - 1;
          return (
            <Bar
              key={status}
              dataKey={status}
              stackId="project"
              fill={`var(--color-${status})`}
              radius={isLast ? [3, 3, 0, 0] : [0, 0, 0, 0]}
            />
          );
        })}
      </BarChart>
    </ChartContainer>
  );
}

// ---------------------------------------------------------------------------
// Comments heat — area chart
// ---------------------------------------------------------------------------

export function CommentsAreaChart({
  data,
  config,
}: {
  data: MonitoringCommentPoint[];
  config: ChartConfig;
}) {
  const gradientId = React.useId().replace(/:/g, "");
  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
      <AreaChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--brand)" stopOpacity={0.25} />
            <stop offset="100%" stopColor="var(--brand)" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} width={42} />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Area
          type="monotone"
          dataKey="count"
          stroke="var(--brand)"
          strokeWidth={2}
          fill={`url(#${gradientId})`}
        />
      </AreaChart>
    </ChartContainer>
  );
}

// ---------------------------------------------------------------------------
// Completion / delay rates — dual line
// ---------------------------------------------------------------------------

export function CompletionTrendChart({
  data,
  config,
}: {
  data: MonitoringCompletionPoint[];
  config: ChartConfig;
}) {
  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
      <LineChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          width={42}
          domain={[0, 100]}
          tickFormatter={(v: number) => `${v}%`}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, _name) =>
                typeof value === "number" ? `${Math.round(value)}%` : String(value)
              }
            />
          }
        />
        <Legend content={<ChartLegendContent />} />
        <Line
          type="monotone"
          dataKey="completion"
          stroke="var(--success)"
          strokeWidth={2}
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="delay"
          stroke="var(--destructive)"
          strokeWidth={2}
          dot={false}
          connectNulls
        />
      </LineChart>
    </ChartContainer>
  );
}

// ---------------------------------------------------------------------------
// Top-N horizontal bars — agent workload / team activity
// ---------------------------------------------------------------------------

export interface TopNRow {
  id: string;
  name: string;
  value: number;
}

/** CSS horizontal-bar leaderboard (design-spec §5.3). `hrefFor` returns a
 *  drill-down target or null when the entity has no detail page ("能跳则跳、
 *  缺页不跳" — then the row stays plain and shows a tooltip only). */
export function TopNBarList({
  rows,
  maxValue,
  color,
  hrefFor,
  label,
}: {
  rows: TopNRow[];
  maxValue: number;
  color: string;
  hrefFor?: (id: string) => string | null;
  label: string;
}) {
  return (
    <ul aria-label={label} className="divide-y">
      {rows.map((row) => {
        const pct = maxValue > 0 ? (row.value / maxValue) * 100 : 0;
        const href = hrefFor ? hrefFor(row.id) : null;
        const content = (
          <>
            <span className="flex min-w-0 items-center gap-2 py-1.5">
              <span className="truncate text-xs">{row.name}</span>
            </span>
            <div className="col-start-2">
              <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full transition-[width] duration-300 ease-out"
                  style={{ width: `${pct}%`, backgroundColor: color }}
                />
              </div>
            </div>
            <span className="text-right text-xs tabular-nums text-muted-foreground">
              {row.value}
            </span>
          </>
        );
        return (
          <li
            key={row.id}
            className="grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_2.5rem] items-center gap-3 py-1.5"
          >
            {href ? (
              <AppLink
                href={href}
                newTabTitle={row.name}
                className="contents"
              >
                {content}
              </AppLink>
            ) : (
              content
            )}
          </li>
        );
      })}
    </ul>
  );
}
