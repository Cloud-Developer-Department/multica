"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, XCircle } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  artifactDetailOptions,
  artifactDiffOptions,
  useReviewArtifact,
} from "@multica/core/artifacts/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";

const STATUS_LABELS: Record<string, string> = {
  draft: "草稿",
  submitted: "待审核",
  approved: "已通过",
  rejected: "已打回",
  superseded: "已取代",
};

export function ReviewDetailPage({ artifactId }: { artifactId: string }) {
  const wsId = useWorkspaceId();
  const [comment, setComment] = useState("");
  const { data: a, isLoading } = useQuery(artifactDetailOptions(wsId, artifactId));
  const { data: diffRes } = useQuery(artifactDiffOptions(wsId, artifactId));
  const review = useReviewArtifact(wsId, artifactId);

  if (isLoading || !a) {
    return (
      <div className="space-y-3 p-5">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const submit = (action: "approved" | "rejected") => {
    if (action === "rejected" && !comment.trim()) {
      toast.error("打回必须填写原因");
      return;
    }
    review.mutate(
      { action, comment: comment.trim() || undefined },
      {
        onSuccess: () => toast.success(action === "approved" ? "审核已通过" : "已打回"),
        onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
      },
    );
  };

  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-lg font-semibold">{a.title}</h1>
            <Badge variant="outline">v{a.version}</Badge>
            <Badge variant="secondary">{STATUS_LABELS[a.status] ?? a.status}</Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            类型：{a.type} · 提交：{a.author_type === "agent" ? "Agent" : "成员"} ·{" "}
            {new Date(a.created_at).toLocaleString()}
          </p>
        </div>
      </div>

      <div className="rounded-md border">
        <header className="border-b px-4 py-2.5 text-sm font-medium">产物内容</header>
        {a.content ? (
          <pre className="whitespace-pre-wrap p-4 text-sm">{a.content}</pre>
        ) : (
          <div className="p-6 text-center text-sm text-muted-foreground">
            文件式 Artifact，请通过附件下载查看。
          </div>
        )}
      </div>

      {diffRes && diffRes.diff.length > 0 && (
        <div className="rounded-md border">
          <header className="border-b px-4 py-2.5 text-sm font-medium">
            版本对比 v{diffRes.from_version} → v{diffRes.to_version}
          </header>
          <div className="max-h-80 overflow-y-auto font-mono text-xs">
            {diffRes.diff.map((d, i) => (
              <div
                key={i}
                className={`flex gap-3 px-4 py-0.5 ${
                  d.op === "+" ? "bg-emerald-500/10 text-emerald-700" : d.op === "-" ? "bg-red-500/10 text-red-600" : ""
                }`}
              >
                <span className="w-8 shrink-0 tabular-nums text-muted-foreground">{d.line}</span>
                <span className="w-4 shrink-0">{d.op}</span>
                <span className="min-w-0 flex-1 whitespace-pre-wrap">{d.text}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="rounded-md border p-4">
        <label className="mb-2 block text-sm font-medium">审核意见（打回必填）</label>
        <Textarea
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          placeholder="填写审核意见…"
          rows={3}
        />
        <div className="mt-3 flex items-center gap-2">
          <Button
            disabled={a.status !== "submitted" || review.isPending}
            onClick={() => submit("approved")}
          >
            <CheckCircle2 className="mr-1 size-4" />
            通过
          </Button>
          <Button
            variant="destructive"
            disabled={a.status !== "submitted" || review.isPending}
            onClick={() => submit("rejected")}
          >
            <XCircle className="mr-1 size-4" />
            打回
          </Button>
          {a.status !== "submitted" && (
            <span className="ml-2 text-xs text-muted-foreground">
              该 Artifact 已审核（{STATUS_LABELS[a.status] ?? a.status}），不可重复操作。
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
