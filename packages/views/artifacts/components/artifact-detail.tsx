"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  artifactDetailOptions,
  artifactReviewsOptions,
  artifactVersionsOptions,
} from "@multica/core/artifacts/queries";
import type { ArtifactReview } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";

const STATUS_LABELS: Record<string, string> = {
  draft: "草稿",
  submitted: "待审核",
  approved: "已通过",
  rejected: "已打回",
  superseded: "已取代",
};

export function ArtifactDetailPage({ artifactId }: { artifactId: string }) {
  const wsId = useWorkspaceId();
  const { data: a, isLoading } = useQuery(artifactDetailOptions(wsId, artifactId));
  const { data: versionsRes } = useQuery(artifactVersionsOptions(wsId, artifactId));
  const { data: reviewsRes } = useQuery(artifactReviewsOptions(wsId, artifactId));

  if (isLoading || !a) {
    return (
      <div className="space-y-3 p-5">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <FileText className="size-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">{a.title}</h1>
            <Badge variant="secondary">v{a.version}</Badge>
            <Badge
              variant={a.status === "approved" ? "default" : a.status === "rejected" ? "destructive" : "secondary"}
            >
              {STATUS_LABELS[a.status] ?? a.status}
            </Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            类型：{a.type} · 提交：{a.author_type === "agent" ? "Agent" : "成员"} ·{" "}
            {new Date(a.created_at).toLocaleString()}
          </p>
        </div>
      </div>

      <Tabs defaultValue="content">
        <TabsList>
          <TabsTrigger value="content">内容</TabsTrigger>
          <TabsTrigger value="versions">版本历史</TabsTrigger>
          <TabsTrigger value="reviews">审核记录</TabsTrigger>
        </TabsList>
        <TabsContent value="content" className="space-y-3">
          {a.content ? (
            <pre className="whitespace-pre-wrap rounded-md border p-4 text-sm">{a.content}</pre>
          ) : (
            <div className="rounded-md border p-6 text-center text-sm text-muted-foreground">
              文件式 Artifact，请在审核页面下载查看。
            </div>
          )}
        </TabsContent>
        <TabsContent value="versions">
          <div className="space-y-1">
            {(versionsRes?.items ?? []).map((v) => (
              <div key={v.id} className="flex items-center justify-between rounded-md border px-4 py-2 text-sm">
                <span className="font-medium">v{v.version}</span>
                <span className="text-muted-foreground">{v.title}</span>
                <Badge variant="outline">{STATUS_LABELS[v.status] ?? v.status}</Badge>
                <span className="text-xs tabular-nums text-muted-foreground">
                  {new Date(v.created_at).toLocaleString()}
                </span>
              </div>
            ))}
            {(versionsRes?.items ?? []).length === 0 && (
              <div className="py-6 text-center text-xs text-muted-foreground">暂无其他版本</div>
            )}
          </div>
        </TabsContent>
        <TabsContent value="reviews">
          <div className="space-y-2">
            {(reviewsRes?.items ?? []).map((r: ArtifactReview) => (
              <div key={r.id} className="rounded-md border px-4 py-3 text-sm">
                <div className="flex items-center gap-2">
                  <Badge variant={r.action === "approved" ? "default" : "destructive"}>
                    {r.action === "approved" ? "通过" : "打回"}
                  </Badge>
                  <span className="text-xs text-muted-foreground">
                    {new Date(r.created_at).toLocaleString()}
                  </span>
                </div>
                {r.comment ? <p className="mt-1 text-sm text-muted-foreground">{r.comment}</p> : null}
              </div>
            ))}
            {(reviewsRes?.items ?? []).length === 0 && (
              <div className="py-6 text-center text-xs text-muted-foreground">暂无审核记录</div>
            )}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
