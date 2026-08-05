"use client";

import { useQuery } from "@tanstack/react-query";
import { FileText, Search } from "lucide-react";
import { useMemo, useState } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { artifactListOptions } from "@multica/core/artifacts/queries";
import type { Artifact, ArtifactStatus, ArtifactType } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";

const STATUS_LABELS: Record<ArtifactStatus, string> = {
  draft: "草稿",
  submitted: "待审核",
  approved: "已通过",
  rejected: "已打回",
  superseded: "已取代",
};

const TYPE_LABELS: Record<ArtifactType, string> = {
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

const ARTIFACT_TYPES = Object.keys(TYPE_LABELS) as ArtifactType[];
const ARTIFACT_STATUSES = Object.keys(STATUS_LABELS) as ArtifactStatus[];

export function ArtifactsPage() {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<ArtifactStatus | "all">("all");
  const [type, setType] = useState<ArtifactType | "all">("all");

  const { data: res, isLoading } = useQuery(
    artifactListOptions(wsId, status !== "all" ? { status } : undefined),
  );

  const items = useMemo(() => {
    if (!res?.items) return [];
    const q = search.trim().toLowerCase();
    return res.items.filter((a) => {
      if (type !== "all" && a.type !== type) return false;
      if (q && !a.title.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [res, search, type]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader icon={FileText} title="Artifact 产物" count={res?.items.length ?? 0} />

      {!isLoading && res?.items.length === 0 ? (
        <CollectionPageState
          icon={FileText}
          title="还没有 Artifact"
          description="Agent 完成节点任务并提交产物后，将在此展示。"
        />
      ) : (
        <>
          <div className="flex h-12 shrink-0 items-center gap-2 px-5">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索标题"
                className="h-8 w-56 pl-8 text-sm"
              />
            </div>
            <select
              value={type}
              onChange={(e) => setType(e.target.value as ArtifactType | "all")}
              className="h-8 w-36 rounded-md border border-input bg-transparent px-2 text-xs"
              aria-label="类型"
            >
              <option value="all">全部类型</option>
              {ARTIFACT_TYPES.map((t) => (
                <option key={t} value={t}>
                  {TYPE_LABELS[t]}
                </option>
              ))}
            </select>
            <select
              value={status}
              onChange={(e) => setStatus(e.target.value as ArtifactStatus | "all")}
              className="h-8 w-32 rounded-md border border-input bg-transparent px-2 text-xs"
              aria-label="状态"
            >
              <option value="all">全部状态</option>
              {ARTIFACT_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {STATUS_LABELS[s]}
                </option>
              ))}
            </select>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-5 pt-4">
            {isLoading ? (
              <div className="space-y-2">
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className="h-14 w-full rounded-md" />
                ))}
              </div>
            ) : items.length === 0 ? (
              <div className="py-24 text-center text-sm text-muted-foreground">无匹配产物</div>
            ) : (
              <div className="overflow-hidden rounded-md border">
                <table className="w-full text-sm">
                  <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                    <tr>
                      <th className="px-4 py-2 font-medium">标题</th>
                      <th className="px-4 py-2 font-medium">类型</th>
                      <th className="px-4 py-2 font-medium">版本</th>
                      <th className="px-4 py-2 font-medium">状态</th>
                      <th className="px-4 py-2 font-medium">提交人</th>
                      <th className="px-4 py-2 font-medium">时间</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {items.map((a: Artifact) => (
                      <tr key={a.id} className="hover:bg-accent/30">
                        <td className="px-4 py-2.5">
                          <AppLink href={wsPaths.artifactDetail(a.id)} className="font-medium hover:underline">
                            {a.title}
                          </AppLink>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">{TYPE_LABELS[a.type] ?? a.type}</td>
                        <td className="px-4 py-2.5 tabular-nums text-muted-foreground">v{a.version}</td>
                        <td className="px-4 py-2.5">
                          <Badge variant={a.status === "approved" ? "default" : a.status === "rejected" ? "destructive" : "secondary"}>
                            {STATUS_LABELS[a.status] ?? a.status}
                          </Badge>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">
                          {a.author_type === "agent" ? "Agent" : "成员"}
                        </td>
                        <td className="px-4 py-2.5 text-xs tabular-nums text-muted-foreground">
                          {new Date(a.created_at).toLocaleString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
