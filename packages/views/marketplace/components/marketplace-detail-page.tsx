"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, Download, Loader2, Store } from "lucide-react";
import { api } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  MARKETPLACE_CATEGORIES,
  type ResourceTemplateDoc,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useIntentNavigate } from "../../navigation";
import { ResourceTemplateImportDialog } from "../../common/resource-template-import-dialog";
import { CollectionPageHeader } from "../../layout/collection-page";
import { useT } from "../../i18n";

/**
 * Marketplace detail page (PRD §5.3). Shows the listing card + skills/members
 * summary and offers one-click import (reuses the full validate/apply wizard
 * with the listing's template), template-file download, and archive/restore
 * for the publisher / workspace owner.
 */

/** Normalises a listing category to a known label key (unknown → other). */
function categoryLabelKey(category: string): (typeof MARKETPLACE_CATEGORIES)[number] {
  return (MARKETPLACE_CATEGORIES as readonly string[]).includes(category)
    ? (category as (typeof MARKETPLACE_CATEGORIES)[number])
    : "other";
}

export function MarketplaceDetailPage({ id }: { id: string }) {
  const { t } = useT("marketplace");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const p = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const currentUser = useAuthStore((s) => s.user);
  const qc = useQueryClient();

  const [importOpen, setImportOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ["marketplace", wsId, "listing", id],
    queryFn: () => api.getMarketplaceListing(id),
    enabled: !!wsId && !!id,
  });
  const listing = data?.listing;

  const templateDoc: ResourceTemplateDoc | undefined = listing?.template as
    | ResourceTemplateDoc
    | undefined;

  const isAuthor = !!currentUser && listing?.author_id === currentUser.id;

  const archive = useMutation({
    mutationFn: () =>
      api.archiveMarketplaceListing(id, listing?.status === "archived"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.marketplace(wsId) });
      qc.invalidateQueries({
        queryKey: ["marketplace", wsId, "listing", id],
      });
      setConfirmOpen(false);
      toast.success(
        listing?.status === "archived"
          ? t(($) => $.detail.restore_button)
          : t(($) => $.detail.archive_button),
      );
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : String(err)),
  });

  const handleDownload = async () => {
    try {
      const blob = await api.downloadMarketplaceTemplate(id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${listing?.title ?? "listing"}-v${listing?.version ?? "1.0.0"}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      // The backend counts downloads on the explicit report endpoint; the
      // file download itself also reports a count (PRD §4.6).
      api.reportMarketplaceDownload(id).catch(() => undefined);
      qc.invalidateQueries({ queryKey: workspaceKeys.marketplace(wsId) });
      toast.success(t(($) => $.detail.download_toast));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };

  const handleImportSuccess = () => {
    setImportOpen(false);
    api.reportMarketplaceDownload(id).catch(() => undefined);
    qc.invalidateQueries({ queryKey: workspaceKeys.marketplace(wsId) });
    toast.success(t(($) => $.detail.imported_toast));
  };

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (!listing) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 p-8 text-center">
        <Store className="size-8 text-muted-foreground" />
        <h2 className="text-body font-medium">{t(($) => $.detail.not_found)}</h2>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.not_found_hint)}
        </p>
        <Button
          size="sm"
          variant="outline"
          className="mt-2"
          onClick={() => intentNavigate(p.marketplace(), "foreground-tab", "marketplace")}
        >
          <ArrowLeft className="mr-1 size-3.5" />
          {t(($) => $.detail.back)}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Store}
        title={listing.title}
        actions={
          <Button
            size="sm"
            variant="outline"
            onClick={() => intentNavigate(p.marketplace(), "foreground-tab", "marketplace")}
          >
            <ArrowLeft className="mr-1 size-3.5" />
            {t(($) => $.detail.back)}
          </Button>
        }
      />
      <div className="min-h-0 flex-1 overflow-auto px-5 pb-8">
        <div className="mx-auto max-w-3xl">
          {/* Header card */}
          <div className="rounded-lg border bg-card p-5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="rounded-full bg-muted px-2 py-0.5 text-caption font-medium text-muted-foreground">
                {listing.kind === "agent"
                  ? t(($) => $.filters.agents)
                  : t(($) => $.filters.squads)}
              </span>
              <span className="rounded-full bg-muted px-2 py-0.5 text-caption font-medium text-muted-foreground">
                {t(($) => $.categories[categoryLabelKey(listing.category)])}
              </span>
              <span className="font-mono text-caption text-muted-foreground">
                {t(($) => $.card.version, { version: listing.version })}
              </span>
              <span className="text-caption tabular-nums text-muted-foreground">
                {t(($) => $.card.downloads, { count: listing.downloads })}
              </span>
            </div>
            {listing.summary ? (
              <p className="mt-3 text-body text-muted-foreground">
                {listing.summary}
              </p>
            ) : null}
            <p className="mt-2 text-caption text-muted-foreground">
              {t(($) => $.detail.author, { name: listing.author_display_name })}
            </p>
            {listing.tags.length > 0 ? (
              <div className="mt-3 flex flex-wrap gap-1">
                {listing.tags.map((tag) => (
                  <span
                    key={tag}
                    className="rounded bg-muted px-1.5 py-0.5 text-caption text-muted-foreground"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            ) : null}

            <div className="mt-5 flex flex-wrap items-center gap-2">
              <Button size="sm" onClick={() => setImportOpen(true)}>
                {t(($) => $.detail.import_button)}
              </Button>
              <Button size="sm" variant="outline" onClick={handleDownload}>
                <Download className="mr-1 size-3.5" />
                {t(($) => $.detail.download_template)}
              </Button>
              {isAuthor ? (
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto text-destructive hover:text-destructive"
                  onClick={() => setConfirmOpen(true)}
                >
                  {listing.status === "archived"
                    ? t(($) => $.detail.restore_button)
                    : t(($) => $.detail.archive_button)}
                </Button>
              ) : null}
            </div>
          </div>

          {/* Template preview */}
          {templateDoc ? (
            <div className="mt-4 rounded-lg border bg-card p-5">
              <h3 className="text-body font-medium">{t(($) => $.detail.description)}</h3>
              {templateDoc.spec?.agent ? (
                <div className="mt-3 space-y-2 text-caption text-muted-foreground">
                  {templateDoc.spec.agent.description ? (
                    <p>{templateDoc.spec.agent.description}</p>
                  ) : null}
                  {templateDoc.spec.agent.skills &&
                  templateDoc.spec.agent.skills.length > 0 ? (
                    <div>
                      <span className="font-medium text-foreground">
                        {t(($) => $.detail.skills)}
                      </span>
                      <div className="mt-1.5 flex flex-wrap gap-1">
                        {templateDoc.spec.agent.skills.map((s, i) => (
                          <span
                            key={`${s.name}-${i}`}
                            className="rounded bg-muted px-1.5 py-0.5"
                          >
                            {s.name}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : null}
              {templateDoc.spec?.squad ? (
                <div className="mt-3 space-y-2 text-caption text-muted-foreground">
                  {templateDoc.spec.squad.description ? (
                    <p>{templateDoc.spec.squad.description}</p>
                  ) : null}
                  {templateDoc.spec.squad.members &&
                  templateDoc.spec.squad.members.length > 0 ? (
                    <div>
                      <span className="font-medium text-foreground">
                        {t(($) => $.detail.members)}
                      </span>
                      <div className="mt-1.5 flex flex-wrap gap-1">
                        {templateDoc.spec.squad.members.map((m, i) => (
                          <span
                            key={`${m.ref}-${i}`}
                            className="rounded bg-muted px-1.5 py-0.5"
                          >
                            {m.role}
                            {m.agent?.name ? `: ${m.agent.name}` : `: ${m.ref}`}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      <ResourceTemplateImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        onSuccess={handleImportSuccess}
        initialDocs={templateDoc ? [templateDoc] : undefined}
      />

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {listing.status === "archived"
                ? t(($) => $.detail.restore_confirm_title)
                : t(($) => $.detail.archive_confirm_title)}
            </DialogTitle>
            <DialogDescription>
              {listing.status === "archived"
                ? t(($) => $.detail.restore_confirm_description)
                : t(($) => $.detail.archive_confirm_description)}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={archive.isPending}
              onClick={() => setConfirmOpen(false)}
            >
              {t(($) => $.publish.back)}
            </Button>
            <Button
              type="button"
              size="sm"
              variant={listing.status === "archived" ? "default" : "destructive"}
              disabled={archive.isPending}
              onClick={() => archive.mutate()}
            >
              {archive.isPending ? (
                <>
                  <Loader2 className="mr-1 size-3.5 animate-spin" />
                  {t(($) => $.publish.publishing)}
                </>
              ) : listing.status === "archived" ? (
                t(($) => $.detail.restore_confirm_ok)
              ) : (
                t(($) => $.detail.archive_confirm_ok)
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
