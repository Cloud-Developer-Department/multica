"use client";

// Chart components for the engineering-analytics platform. All follow the
// existing recharts style (ChartContainer / ChartTooltip from packages/ui,
// `--color-chart-*` tokens, `text-[10px]/[11px]` in-chart labels). No
// hard-coded colors — every series colour resolves through a token.

import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  PolarAngleAxis,
  PolarGrid,
  PolarRadiusAxis,
  Radar,
  RadarChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import { useT } from "../i18n";

// ---------------------------------------------------------------------------
// Dual line (adoption trend, DORA lead time, failure rate/MTTR)
// ---------------------------------------------------------------------------

export interface DualLinePoint {
  label: string;
  /** 0–1 ratio for the "percent" mode, raw seconds for "duration" mode. */
  a: number | null;
  b: number | null;
  /** Sample count for tooltip (DORA lead time). */
  samples?: number | null;
}

export function DualLineChart({
  data,
  config,
  aKey = "a",
  bKey = "b",
  formatValue,
  connectNulls = false,
  className = "aspect-[3/1]",
}: {
  data: DualLinePoint[];
  config: ChartConfig;
  aKey?: string;
  bKey?: string;
  formatValue?: (value: number, key: string) => string;
  connectNulls?: boolean;
  className?: string;
}) {
  const { t } = useT("analytics");
  return (
    <ChartContainer config={config} className={className}>
      <LineChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} width={48} />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) => {
                if (typeof value !== "number") return String(value);
                const key = String(name);
                if (formatValue) return formatValue(value, key);
                return value.toFixed(1);
              }}
              // Append the sample count when the point carries one (DORA).
              labelFormatter={(label, payload) => {
                const samples = payload?.[0]?.payload?.samples;
                if (samples == null) return label;
                return `${label} · ${t(($) => $.lead_time.samples, { count: samples })}`;
              }}
            />
          }
        />
        <Line type="monotone" dataKey={aKey} stroke="var(--color-chart-1)" strokeWidth={2} dot={false} connectNulls={connectNulls} />
        <Line type="monotone" dataKey={bKey} stroke="var(--color-chart-4)" strokeWidth={2} dot={false} connectNulls={connectNulls} />
      </LineChart>
    </ChartContainer>
  );
}

// ---------------------------------------------------------------------------
// Deploy frequency — weekly bar chart
// ---------------------------------------------------------------------------

export interface DeployBarPoint {
  label: string;
  deployments: number;
  failed: number;
}

export function DeployFrequencyChart({ data }: { data: DeployBarPoint[] }) {
  const { t } = useT("analytics");
  const config: ChartConfig = {
    deployments: { label: t(($) => $.deploy_freq.deployments), color: "var(--color-chart-2)" },
  };
  return (
    <ChartContainer config={config} className="aspect-[3/1]">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} width={42} allowDecimals={false} />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) => {
                if (name === "failed") {
                  return `${t(($) => $.deploy_freq.failed)}: ${value}`;
                }
                return value;
              }}
            />
          }
        />
        <Bar dataKey="deployments" fill="var(--color-chart-2)" radius={4} />
      </BarChart>
    </ChartContainer>
  );
}

// ---------------------------------------------------------------------------
// Commit-quality radar (coverage / vulnerabilities / duplication)
// ---------------------------------------------------------------------------

export interface QualityRadarData {
  /** 0–100. */
  coverage: number | null;
  /** Raw count, reversed (fewer is better). */
  vulnerabilities: number | null;
  /** 0–100, reversed (lower is better). */
  duplication: number | null;
}

export function QualityRadarChart({ data }: { data: QualityRadarData }) {
  const { t } = useT("analytics");
  const coverage = data.coverage ?? 0;
  const vuln = data.vulnerabilities ?? 0;
  const duplication = data.duplication ?? 0;

  // Reverse-axis normalisation: vulnerability count and duplication rate are
  // "lower is better", so we invert them against a reference maximum (50 vulns
  // / 50%) so the radar still reads "bigger = better" for all three axes. The
  // tooltip / axis labels always show the raw value.
  const vulnInverted = Math.max(0, Math.min(100, 100 - Math.min(vuln, 50) * 2));
  const dupInverted = Math.max(0, Math.min(100, 100 - duplication));

  const chartData = [
    { axis: t(($) => $.quality.coverage), value: coverage },
    { axis: t(($) => $.quality.vulnerabilities), value: vulnInverted },
    { axis: t(($) => $.quality.duplication), value: dupInverted },
  ];

  const config: ChartConfig = {
    value: { label: "value", color: "var(--color-chart-3)" },
  };

  return (
    <div className="flex flex-col items-center gap-2">
      <div className="w-full max-w-[280px]">
        <ChartContainer config={config} className="aspect-square w-full">
          <RadarChart data={chartData} margin={{ top: 8, right: 8, bottom: 8, left: 8 }}>
            <PolarGrid />
            <PolarAngleAxis dataKey="axis" tick={{ fontSize: 10, fill: "var(--color-muted-foreground)" }} />
            <PolarRadiusAxis tick={false} axisLine={false} domain={[0, 100]} />
            <Radar dataKey="value" fill="var(--color-chart-3)" fillOpacity={0.3} stroke="var(--color-chart-3)" strokeWidth={2} />
            <Tooltip
              content={
                <ChartTooltipContent
                  formatter={(value, name) => {
                    if (String(name) !== "value") return String(value);
                    return typeof value === "number"
                      ? `${coverage}%`
                      : String(value);
                  }}
                />
              }
            />
          </RadarChart>
        </ChartContainer>
      </div>
      <div className="flex w-full flex-col gap-2 text-xs">
        <RadarValueRow label={t(($) => $.quality.coverage)} value={`${coverage.toFixed(0)}%`} better={coverage >= 60} />
        <RadarValueRow label={t(($) => $.quality.vulnerabilities)} value={String(vuln)} />
        <RadarValueRow label={t(($) => $.quality.duplication)} value={`${duplication.toFixed(0)}%`} better={duplication <= 15} />
        <p className="text-[10px] text-muted-foreground">{t(($) => $.quality.better)}</p>
      </div>
    </div>
  );
}

function RadarValueRow({
  label,
  value,
  better,
}: {
  label: string;
  value: string;
  better?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-2 border-b border-dashed pb-1.5 last:border-0">
      <span className="text-muted-foreground">{label}</span>
      <span className={`font-medium tabular-nums ${better ? "text-success" : ""}`}>{value}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Day×hour activity heatmap (A2)
// ---------------------------------------------------------------------------

export interface HeatmapCell {
  date: string;
  hour: number;
  value: number | null;
}

export function AnalyticsHeatmap({
  days,
  maxValue,
  metricLabel,
  onCellClick,
}: {
  days: { date: string; hours: { hour: number; value: number | null }[] }[];
  maxValue: number;
  metricLabel: (date: string, hour: number, value: number | null) => string;
  onCellClick?: (date: string, hour: number) => void;
}) {
  const { t } = useT("analytics");

  const cells: HeatmapCell[] = [];
  for (const day of days) {
    for (const h of day.hours) {
      cells.push({ date: day.date, hour: h.hour, value: h.value });
    }
  }

  const colorFor = (value: number | null) => {
    if (value == null) return "var(--color-muted)";
    const pct = maxValue > 0 ? value / maxValue : 0;
    const opacities = ["20%", "45%", "70%", "100%"];
    const idx = pct <= 0 ? 0 : Math.min(3, Math.ceil(pct * 4) - 1);
    return `color-mix(in oklch, var(--color-chart-1) ${opacities[idx] ?? "20%"}, transparent)`;
  };

  const hourLabel = (hour: number) => String(hour).padStart(2, "0");

  return (
    <div className="overflow-x-auto">
      <div className="inline-flex flex-col gap-1">
        <div className="grid grid-cols-[2.5rem_1fr] items-center gap-1">
          <span />
          <div className="grid grid-flow-col gap-[2px]">
            {days.map((d) => (
              <span key={d.date} className="text-center text-[10px] text-muted-foreground">
                {d.date.slice(5)}
              </span>
            ))}
          </div>
        </div>
        {Array.from({ length: 24 }, (_, hour) => (
          <div key={hour} className="grid grid-cols-[2.5rem_1fr] items-center gap-1">
            <span className="text-right text-[10px] text-muted-foreground">{hourLabel(hour)}</span>
            <div className="grid grid-flow-col gap-[2px]">
              {days.map((day) => {
                const cell = day.hours.find((h) => h.hour === hour);
                return (
                  <button
                    key={`${day.date}-${hour}`}
                    type="button"
                    className="h-[10px] w-[10px] rounded-[2px] transition-colors"
                    style={{ backgroundColor: colorFor(cell?.value ?? null) }}
                    title={metricLabel(day.date, hour, cell?.value ?? null)}
                    onClick={
                      cell?.value != null && onCellClick
                        ? () => onCellClick(day.date, hour)
                        : undefined
                    }
                  />
                );
              })}
            </div>
          </div>
        ))}
        <div className="mt-1 flex items-center justify-end gap-1 text-[10px] text-muted-foreground">
          <span>{t(($) => $.heatmap.less)}</span>
          {["20%", "45%", "70%", "100%"].map((o) => (
            <span
              key={o}
              className="h-[10px] w-[10px] rounded-[2px]"
              style={{ backgroundColor: `color-mix(in oklch, var(--color-chart-1) ${o}, transparent)` }}
            />
          ))}
          <span>{t(($) => $.heatmap.more)}</span>
        </div>
      </div>
    </div>
  );
}
