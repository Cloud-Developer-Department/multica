"use client";

import { useQuery } from "@tanstack/react-query";
import { BarChart3 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { artifactStatsOptions } from "@multica/core/artifacts/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

const TYPE_LABELS: Record<string, string> = {
  requirements: "需求文档",
  architecture: "架构设计",
  development: "代码/变更",
  testing: "测试报告",
  code_review: "代码审查报告",
  security: "安全审计报告",
  documentation: "文档",
  deployment: "部署文档",
  other: "其他",
};

function StatCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="rounded-md border p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {sub ? <div className="mt-1 text-xs text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

export function ArtifactStatsPage() {
  const wsId = useWorkspaceId();
  const { data: stats, isLoading } = useQuery(artifactStatsOptions(wsId));

  if (isLoading || !stats) {
    return (
      <div className="grid grid-cols-2 gap-3 p-5 md:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full rounded-md" />
        ))}
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
      <div className="flex items-center gap-2">
        <BarChart3 className="size-5 text-muted-foreground" />
        <h1 className="text-lg font-semibold">审核统计</h1>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard label="提交总数" value={stats.total_submitted} />
        <StatCard label="已通过" value={stats.approved} />
        <StatCard label="已打回" value={stats.rejected} />
        <StatCard label="待审核" value={stats.pending} />
        <StatCard label="通过率" value={`${Math.round(stats.approval_rate * 100)}%`} />
        <StatCard
          label="平均审核耗时"
          value={stats.avg_review_duration_ms > 0 ? `${Math.round(stats.avg_review_duration_ms / 1000)}s` : "—"}
        />
        <StatCard label="统计区间" value="近30天" sub={`${stats.range.from.slice(0, 10)} ~ ${stats.range.to.slice(0, 10)}`} />
      </div>

      <section className="rounded-md border">
        <header className="border-b px-4 py-2.5 text-sm font-medium">按类型统计</header>
        <div className="divide-y">
          {stats.by_type.map((t) => (
            <div key={t.type} className="flex items-center justify-between px-4 py-2.5 text-sm">
              <span className="font-medium">{TYPE_LABELS[t.type] ?? t.type}</span>
              <div className="flex items-center gap-6">
                <span className="tabular-nums text-muted-foreground">{t.submitted} 提交</span>
                <span className="tabular-nums text-muted-foreground">
                  通过率 {Math.round(t.approval_rate * 100)}%
                </span>
                <span className="tabular-nums text-muted-foreground">
                  平均耗时{" "}
                  {t.avg_review_duration_ms > 0 ? `${Math.round(t.avg_review_duration_ms / 1000)}s` : "—"}
                </span>
              </div>
            </div>
          ))}
          {stats.by_type.length === 0 && (
            <div className="px-4 py-6 text-center text-xs text-muted-foreground">暂无统计数据</div>
          )}
        </div>
      </section>
    </div>
  );
}
