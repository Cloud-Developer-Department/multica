"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowRight, GitBranch, History, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  useAdvanceWorkflow,
  workflowDetailOptions,
  workflowTransitionsOptions,
} from "@multica/core/workflows/queries";
import type { WorkflowNode, WorkflowStage } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";

const NODE_STATUS_LABELS: Record<string, string> = {
  backlog: "待激活",
  todo: "待执行",
  in_progress: "执行中",
  in_review: "待审核",
  done: "已完成",
  blocked: "阻塞",
  cancelled: "已取消",
};

function NodeBadge({ status }: { status: string }) {
  const tone =
    status === "done"
      ? "default"
      : status === "blocked" || status === "cancelled"
        ? "destructive"
        : status === "in_review"
          ? "secondary"
          : "outline";
  return (
    <Badge variant={tone === "default" ? undefined : tone} className={tone === "default" ? "bg-emerald-600 text-white" : ""}>
      {NODE_STATUS_LABELS[status] ?? status}
    </Badge>
  );
}

export function WorkflowDetailPage({ workflowId }: { workflowId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const advance = useAdvanceWorkflow(wsId, workflowId);

  const { data: wf, isLoading } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: transitionsRes } = useQuery(workflowTransitionsOptions(wsId, workflowId));

  if (isLoading || !wf) {
    return (
      <div className="space-y-3 p-5">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  const stageName = (stage: number) =>
    wf.stages?.find((s: WorkflowStage) => s.stage === stage)?.name ?? `阶段 ${stage}`;

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <GitBranch className="size-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">{wf.name}</h1>
            <Badge variant="secondary">{wf.status}</Badge>
          </div>
          {wf.description ? (
            <p className="mt-1 text-sm text-muted-foreground">{wf.description}</p>
          ) : null}
          <p className="mt-1 text-xs text-muted-foreground">
            当前阶段：Stage {wf.current_stage}（{stageName(wf.current_stage)}）
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              advance.mutate("阶段人工推进", {
                onSuccess: () => toast.success("阶段已推进"),
                onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
              });
            }}
          >
            <ArrowRight className="mr-1 size-3.5" />
            推进阶段
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => window.location.reload()}
          >
            <RefreshCw className="mr-1 size-3.5" />
            刷新
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-4 gap-3 rounded-md border p-4">
        <div className="text-center">
          <div className="text-2xl font-semibold">{wf.progress?.done_nodes ?? 0}</div>
          <div className="text-xs text-muted-foreground">已完成节点</div>
        </div>
        <div className="text-center">
          <div className="text-2xl font-semibold">{wf.progress?.in_review_nodes ?? 0}</div>
          <div className="text-xs text-muted-foreground">待审核节点</div>
        </div>
        <div className="text-center">
          <div className="text-2xl font-semibold">{wf.progress?.blocked_nodes ?? 0}</div>
          <div className="text-xs text-muted-foreground">阻塞节点</div>
        </div>
        <div className="text-center">
          <div className="text-2xl font-semibold">{wf.progress?.total_nodes ?? 0}</div>
          <div className="text-xs text-muted-foreground">总节点数</div>
        </div>
      </div>

      <div className="space-y-3">
        {wf.stages?.map((stage: WorkflowStage) => (
          <section key={stage.stage} className="rounded-md border">
            <header className="flex items-center justify-between border-b px-4 py-2.5">
              <h2 className="text-sm font-medium">
                Stage {stage.stage} · {stage.name}
              </h2>
              <Badge variant="outline">{stage.status}</Badge>
            </header>
            <div className="divide-y">
              {(stage.nodes ?? []).map((n: WorkflowNode) => (
                <div key={n.id} className="flex items-center justify-between px-4 py-2.5 text-sm">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="min-w-0 truncate font-medium">{n.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">#{n.seq}</span>
                  </div>
                  <div className="flex shrink-0 items-center gap-3">
                    {n.review_required && <Badge variant="secondary">需审核</Badge>}
                    <NodeBadge status={n.status} />
                    <AppLink
                      href={wsPaths.issueDetail(n.issue_id)}
                      className="text-xs text-muted-foreground underline-offset-2 hover:text-primary hover:underline"
                    >
                      子任务
                    </AppLink>
                  </div>
                </div>
              ))}
              {(stage.nodes ?? []).length === 0 && (
                <div className="px-4 py-3 text-xs text-muted-foreground">该阶段暂无节点</div>
              )}
            </div>
          </section>
        ))}
      </div>

      <section className="rounded-md border">
        <header className="flex items-center gap-2 border-b px-4 py-2.5">
          <History className="size-4 text-muted-foreground" />
          <h2 className="text-sm font-medium">流转审计</h2>
        </header>
        <div className="divide-y">
          {(transitionsRes?.items ?? []).slice(0, 20).map((t) => (
            <div key={t.id} className="flex items-center justify-between px-4 py-2 text-xs">
              <span className="min-w-0 truncate text-muted-foreground">
                {t.from_status || "—"} → {t.to_status}
              </span>
              <span className="ml-3 min-w-0 flex-1 truncate text-muted-foreground">
                {t.reason || ""}
              </span>
              <span className="ml-3 shrink-0 tabular-nums text-muted-foreground">
                {t.actor_type === "system" ? "系统" : t.actor_type}
              </span>
              <span className="ml-3 shrink-0 tabular-nums text-muted-foreground">
                {new Date(t.created_at).toLocaleString()}
              </span>
            </div>
          ))}
          {(transitionsRes?.items ?? []).length === 0 && (
            <div className="px-4 py-3 text-xs text-muted-foreground">暂无流转记录</div>
          )}
        </div>
      </section>
    </div>
  );
}
