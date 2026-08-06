"use client";

// Runtime status list (design-spec §4.8 / PRD §4.6): full-width table with
// pagination. Health is derived from the same `deriveRuntimeHealth` the
// Runtimes page uses, so a "online" tag here matches the runtimes surface.
//
// Column reality check: the AgentRuntime wire shape has no CPU / memory
// fields, so the 资源占用 column renders "—" (the design's "如有" fallback) —
// see the PRD §13 R1 risk note. The type column shows provider + mode instead
// of a fictional "runtime / daemon / agent" enum.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import {
  runtimeListOptions,
  deriveRuntimeHealth,
  runtimeDisplayName,
} from "@multica/core/runtimes";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import { HealthBadge, RuntimeModeIcon } from "../../runtimes/components/shared";
import { useT } from "../../i18n";
import { MISSING } from "./utils";

const PAGE_SIZE = 10;

const RUNTIME_EMPTY: AgentRuntime[] = [];

export function RuntimeStatusListCard({ wsId }: { wsId: string }) {
  const { t } = useT("monitoring");
  const wsPaths = useWorkspacePaths();
  const query = useQuery(runtimeListOptions(wsId));
  const [page, setPage] = useState(0);

  const runtimes = query.data ?? RUNTIME_EMPTY;
  const now = useMemo(() => Date.now(), []);
  const pageCount = Math.max(1, Math.ceil(runtimes.length / PAGE_SIZE));
  const clampedPage = Math.min(page, pageCount - 1);
  const visible = runtimes.slice(
    clampedPage * PAGE_SIZE,
    clampedPage * PAGE_SIZE + PAGE_SIZE,
  );

  if (query.isLoading) {
    return <RuntimeListSkeleton />;
  }

  if (query.isError) {
    return null;
  }

  if (runtimes.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed py-10 text-center">
        <span className="text-xs text-muted-foreground">
          {t(($) => $.runtime.empty_title)}
        </span>
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-sm font-semibold">{t(($) => $.runtime.title)}</h4>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.runtime.summary, {
            online: runtimes.filter((r) => deriveRuntimeHealth(r, now) === "online").length,
            total: runtimes.length,
          })}
        </span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] text-left text-xs">
          <thead>
            <tr className="border-b bg-muted/40 text-muted-foreground">
              <th className="px-4 py-2 font-medium">{t(($) => $.runtime.col_name)}</th>
              <th className="px-4 py-2 font-medium">{t(($) => $.runtime.col_type)}</th>
              <th className="px-4 py-2 font-medium">{t(($) => $.runtime.col_status)}</th>
              <th className="px-4 py-2 font-medium">{t(($) => $.runtime.col_heartbeat)}</th>
              <th className="px-4 py-2 font-medium">{t(($) => $.runtime.col_resources)}</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {visible.map((runtime) => (
              <RuntimeRow
                key={runtime.id}
                runtime={runtime}
                now={now}
                href={wsPaths.runtimeDetail(runtime.id)}
              />
            ))}
          </tbody>
        </table>
      </div>

      {pageCount > 1 && (
        <div className="flex items-center justify-center gap-1 border-t px-4 py-2.5">
          <button
            type="button"
            disabled={clampedPage === 0}
            onClick={() => setPage(clampedPage - 1)}
            className="rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
          >
            {t(($) => $.runtime.prev_page)}
          </button>
          {Array.from({ length: pageCount }, (_, i) => (
            <button
              key={i}
              type="button"
              onClick={() => setPage(i)}
              aria-current={i === clampedPage ? "page" : undefined}
              className={`rounded-md px-2 py-1 text-xs tabular-nums ${
                i === clampedPage
                  ? "bg-brand text-white"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground"
              }`}
            >
              {i + 1}
            </button>
          ))}
          <button
            type="button"
            disabled={clampedPage >= pageCount - 1}
            onClick={() => setPage(clampedPage + 1)}
            className="rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
          >
            {t(($) => $.runtime.next_page)}
          </button>
        </div>
      )}
    </div>
  );
}

function RuntimeRow({
  runtime,
  now,
  href,
}: {
  runtime: AgentRuntime;
  now: number;
  href: string;
}) {
  const health = deriveRuntimeHealth(runtime, now);
  const lastSeen = runtime.last_seen_at
    ? new Date(runtime.last_seen_at).getTime()
    : null;

  return (
    <tr className="hover:bg-muted/30">
      <td className="px-4 py-2.5">
        <AppLink
          href={href}
          newTabTitle={runtimeDisplayName(runtime)}
          className="block max-w-[280px] truncate text-sm font-medium hover:underline"
        >
          {runtimeDisplayName(runtime)}
        </AppLink>
      </td>
      <td className="px-4 py-2.5">
        <Badge variant="secondary" className="bg-muted text-muted-foreground">
          <RuntimeModeIcon mode={runtime.runtime_mode} />
          <span className="capitalize">{runtime.provider}</span>
        </Badge>
      </td>
      <td className="px-4 py-2.5">
        <HealthBadge health={health} />
      </td>
      <td className="px-4 py-2.5">
        <HeartbeatCell lastSeen={lastSeen} now={now} health={health} />
      </td>
      <td className="px-4 py-2.5 text-muted-foreground">{MISSING}</td>
    </tr>
  );
}

function HeartbeatCell({
  lastSeen,
  now,
  health,
}: {
  lastSeen: number | null;
  now: number;
  health: ReturnType<typeof deriveRuntimeHealth>;
}) {
  const { t } = useT("monitoring");
  if (lastSeen == null) return <span className="text-muted-foreground">{MISSING}</span>;

  const time = new Date(lastSeen);
  const pad = (n: number) => String(n).padStart(2, "0");
  const hhmmss = `${pad(time.getHours())}:${pad(time.getMinutes())}:${pad(time.getSeconds())}`;

  const isOnline = health === "online";
  const minutesAgo = Math.max(0, Math.floor((now - lastSeen) / 60_000));

  return (
    <span className="flex items-center gap-1.5">
      <Clock className="h-3 w-3 text-muted-foreground" />
      <span className="tabular-nums">{hhmmss}</span>
      {!isOnline && (
        <span className="text-destructive">
          {t(($) => $.runtime.minutes_ago, { minutes: minutesAgo })}
        </span>
      )}
    </span>
  );
}

function RuntimeListSkeleton() {
  return (
    <div className="rounded-lg border bg-card p-4">
      <Skeleton className="h-4 w-40" />
      <div className="mt-4 space-y-3">
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    </div>
  );
}
