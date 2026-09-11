"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  agentListOptions,
  squadListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  MARKETPLACE_CATEGORIES,
  type MarketplaceKind,
  type MarketplacePublishMetadata,
} from "@multica/core/types";
import type { Agent, Squad } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { useT } from "../../i18n";

/**
 * Publish-listing dialog (PRD §5.4). Three steps: choose a resource
 * (agent/squad) → fill marketplace metadata → confirm. The backend runs the
 * server-side export + validation on publish, so the dialog only needs the
 * resource identity — no template assembly happens client-side.
 */
export function PublishListingDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("marketplace");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const qc = useQueryClient();

  const [kind, setKind] = useState<MarketplaceKind>("agent");
  const [resourceId, setResourceId] = useState("");
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [category, setCategory] = useState("");
  const [tags, setTags] = useState("");
  const [version, setVersion] = useState("");
  const [error, setError] = useState<string | null>(null);

  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: open && kind === "agent" && !!wsId,
  });
  const { data: squads = [] } = useQuery({
    ...squadListOptions(wsId),
    enabled: open && kind === "squad" && !!wsId,
  });

  const resources: Array<{ id: string; name: string }> = useMemo(() => {
    if (kind === "agent") {
      return (agents as Agent[]).map((a) => ({ id: a.id, name: a.name }));
    }
    return (squads as Squad[]).map((s) => ({ id: s.id, name: s.name }));
  }, [kind, agents, squads]);

  const publish = useMutation({
    mutationFn: () => {
      const metadata: MarketplacePublishMetadata = { title: title.trim() };
      if (summary.trim()) metadata.summary = summary.trim();
      if (category) metadata.category = category;
      const parsedTags = tags
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (parsedTags.length > 0) metadata.tags = parsedTags;
      if (version.trim()) metadata.version = version.trim();
      return api.publishMarketplaceListing({
        kind,
        resource_id: resourceId,
        metadata,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.marketplace(wsId) });
      onOpenChange(false);
      toast.success(t(($) => $.publish.success));
    },
    onError: (err) => {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      toast.error(msg);
    },
  });

  const handleSubmit = () => {
    setError(null);
    if (!resourceId) {
      const msg = t(($) => $.publish.resource_required);
      setError(msg);
      return;
    }
    if (!title.trim()) {
      const msg = t(($) => $.publish.title_required);
      setError(msg);
      return;
    }
    publish.mutate();
  };

  const canSubmit =
    resourceId !== "" && title.trim() !== "" && !publish.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.publish.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.page.tagline)}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>{t(($) => $.publish.resource_kind)}</Label>
              <NativeSelect
                className="mt-1.5"
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value as MarketplaceKind);
                  setResourceId("");
                }}
              >
                <NativeSelectOption value="agent">
                  {t(($) => $.publish.resource_agent)}
                </NativeSelectOption>
                <NativeSelectOption value="squad">
                  {t(($) => $.publish.resource_squad)}
                </NativeSelectOption>
              </NativeSelect>
            </div>
            <div>
              <Label>{t(($) => $.publish.resource_label)}</Label>
              <NativeSelect
                className="mt-1.5"
                value={resourceId}
                onChange={(e) => setResourceId(e.target.value)}
              >
                <NativeSelectOption value="" disabled>
                  {t(($) => $.publish.resource_placeholder, {
                    kind: kind === "agent" ? "agent" : "squad",
                  })}
                </NativeSelectOption>
                {resources.map((r) => (
                  <NativeSelectOption key={r.id} value={r.id}>
                    {r.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
          </div>
          <div>
            <Label>{t(($) => $.publish.title_label)}</Label>
            <Input
              className="mt-1.5"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t(($) => $.publish.title_placeholder)}
            />
          </div>
          <div>
            <Label>{t(($) => $.publish.summary_label)}</Label>
            <Input
              className="mt-1.5"
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder={t(($) => $.publish.summary_placeholder)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>{t(($) => $.publish.category_label)}</Label>
              <NativeSelect
                className="mt-1.5"
                value={category}
                onChange={(e) => setCategory(e.target.value)}
              >
                <NativeSelectOption value="">
                  {t(($) => $.filters.all)}
                </NativeSelectOption>
                {MARKETPLACE_CATEGORIES.map((c) => (
                  <NativeSelectOption key={c} value={c}>
                    {t(($) => $.categories[c])}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <div>
              <Label>{t(($) => $.publish.version_label)}</Label>
              <Input
                className="mt-1.5"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                placeholder={t(($) => $.publish.version_placeholder)}
              />
            </div>
          </div>
          <div>
            <Label>{t(($) => $.publish.tags_label)}</Label>
            <Input
              className="mt-1.5"
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              placeholder={t(($) => $.publish.tags_placeholder)}
            />
          </div>
          {error ? (
            <p className="text-caption text-destructive">{error}</p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={publish.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.publish.back)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            onClick={handleSubmit}
          >
            {publish.isPending ? (
              <>
                <Loader2 className="mr-1 size-3.5 animate-spin" />
                {t(($) => $.publish.publishing)}
              </>
            ) : (
              t(($) => $.publish.publish)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
