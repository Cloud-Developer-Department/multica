"use client";

import { useQuery } from "@tanstack/react-query";
import { GitBranch, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workflowListOptions } from "@multica/core/workflows/queries";
import type { Workflow, WorkflowStatus } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import { AppLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";

const STATUS_LABELS: Record<WorkflowStatus, string> = {
  todo: "未开始",
  in_progress: "进行中",
  in_review: "待审核",
  done: "已完成",
  blocked: "阻塞",
  cancelled: "已取消",
  backlog: "待激活",
};

function WorkflowStatusBadge({ status }: { status: WorkflowStatus }) {
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
      {STATUS_LABELS[status] ?? status}
    </Badge>
  );
}

export function WorkflowsPage() {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [search, setSearch] = useState("");
  const { data: res, isLoading } = useQuery(workflowListOptions(wsId));

  const items = useMemo(() => {
    if (!res?.items) return [];
    const q = search.trim().toLowerCase();
    if (!q) return res.items;
    return res.items.filter((w) => w.name.toLowerCase().includes(q));
  }, [res, search]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={GitBranch}
        title="研发流程"
        count={res?.items.length ?? 0}
        actions={
          <CollectionPageHeaderAction
            icon={Plus}
            label="新建流程"
            onClick={() => toast.info("新建流程：请先通过项目经理 Agent 创建 Workflow")}
          />
        }
      />

      {!isLoading && res?.items.length === 0 ? (
        <CollectionPageState
          icon={GitBranch}
          title="还没有研发流程"
          description="由项目经理 Agent 基于需求 issue 创建 Workflow 后，流程将在此展示。"
        />
      ) : (
        <>
          <div className="flex h-12 shrink-0 items-center justify-between gap-2 px-5">
            <div className="relative">
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索流程名称"
                className="h-8 w-56 pl-3 text-sm"
              />
            </div>
            <span className="text-xs tabular-nums text-muted-foreground">
              {items.length} / {res?.items.length ?? 0}
            </span>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-5 pt-4">
            {isLoading ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-40 w-full rounded-md" />
                ))}
              </div>
            ) : items.length === 0 ? (
              <div className="py-24 text-center text-sm text-muted-foreground">无匹配流程</div>
            ) : (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {items.map((w) => (
                  <WorkflowCard key={w.id} workflow={w} href={wsPaths.workflowDetail(w.id)} />
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function WorkflowCard({ workflow, href }: { workflow: Workflow; href: string }) {
  const total = workflow.progress?.total_nodes ?? 0;
  const done = workflow.progress?.done_nodes ?? 0;
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <AppLink href={href} className="group flex flex-col rounded-md border bg-card p-4 transition-colors hover:border-primary/50">
      <div className="flex items-start justify-between gap-2">
        <h3 className="truncate text-sm font-medium">{workflow.name}</h3>
        <WorkflowStatusBadge status={workflow.status} />
      </div>
      {workflow.description ? (
        <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{workflow.description}</p>
      ) : null}
      <div className="mt-4 space-y-1">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>当前阶段</span>
          <span>Stage {workflow.current_stage}</span>
        </div>
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>节点进度</span>
          <span>
            {done}/{total} ({pct}%)
          </span>
        </div>
      </div>
    </AppLink>
  );
}
