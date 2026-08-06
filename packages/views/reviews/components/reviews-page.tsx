"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { reviewQueueOptions } from "@multica/core/artifacts/queries";
import type { ReviewQueueItem } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";

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

export function ReviewsPage() {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: res, isLoading } = useQuery(reviewQueueOptions(wsId));

  const items = res?.items ?? [];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={CheckCircle2}
        title="人工审核"
        count={res?.total ?? 0}
        description="由需求 issue 创建者审核 AI 产物"
      />

      {!isLoading && items.length === 0 ? (
        <CollectionPageState
          icon={CheckCircle2}
          title="没有待审核的 Artifact"
          description="Agent 提交产物后，待审核列表将在此展示。"
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto px-5 pt-4">
          {isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-16 w-full rounded-md" />
              ))}
            </div>
          ) : (
            <div className="space-y-2">
              {items.map((item: ReviewQueueItem) => (
                <AppLink
                  key={item.artifact_id}
                  href={wsPaths.reviewDetail(item.artifact_id)}
                  className="flex items-center justify-between rounded-md border bg-card px-4 py-3 transition-colors hover:border-primary/50"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{item.title}</span>
                      <Badge variant="outline">v{item.version}</Badge>
                    </div>
                    <div className="mt-1 flex items-center gap-3 text-xs text-muted-foreground">
                      <span>{item.workflow_name ?? "—"}</span>
                      <span>·</span>
                      <span>{item.node_name ?? "—"}</span>
                      <span>·</span>
                      <span>Stage {item.stage ?? "—"}</span>
                      <span>·</span>
                      <span>{TYPE_LABELS[item.type] ?? item.type}</span>
                    </div>
                  </div>
                  <div className="shrink-0 text-xs tabular-nums text-muted-foreground">
                    {new Date(item.created_at).toLocaleString()}
                  </div>
                </AppLink>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
