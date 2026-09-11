"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Store, Search, Plus } from "lucide-react";
import { api } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  MARKETPLACE_CATEGORIES,
  type MarketplaceKind,
  type MarketplaceSort,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useRowLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { useT } from "../../i18n";
import { PublishListingDialog } from "./publish-listing-dialog";

/**
 * Marketplace list page (PRD §5.2): search + type/category filters + sort over
 * the workspace's published listings, with a publish entry point.
 */
export function MarketplaceListPage() {
  const { t } = useT("marketplace");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const p = useWorkspacePaths();
  const rowLink = useRowLink();

  const [kind, setKind] = useState<MarketplaceKind | "">("");
  const [category, setCategory] = useState("");
  const [sort, setSort] = useState<MarketplaceSort>("latest");
  const [search, setSearch] = useState("");
  const [publishOpen, setPublishOpen] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ["marketplace", wsId, { kind, category, sort, search }],
    queryFn: () =>
      api.listMarketplaceListings({
        kind: kind || undefined,
        category: category || undefined,
        q: search || undefined,
        sort,
        page: 1,
        page_size: 50,
      }),
    enabled: !!wsId,
  });

  const items = data?.items ?? [];
  const total = data?.total ?? 0;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Store}
        title={t(($) => $.page.title)}
        count={total}
        description={t(($) => $.page.tagline)}
        actions={
          <CollectionPageHeaderAction
            icon={Plus}
            label={t(($) => $.page.publish_button)}
            onClick={() => setPublishOpen(true)}
          />
        }
      />

      {/* Toolbar: search + filters + sort */}
      <div className="flex h-12 shrink-0 items-center gap-2 px-5">
        <div className="relative min-w-0 flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t(($) => $.page.search_placeholder)}
          />
        </div>
        <NativeSelect
          className="w-auto"
          value={kind}
          onChange={(e) => setKind(e.target.value as MarketplaceKind | "")}
        >
          <NativeSelectOption value="">{t(($) => $.filters.all)}</NativeSelectOption>
          <NativeSelectOption value="agent">{t(($) => $.filters.agents)}</NativeSelectOption>
          <NativeSelectOption value="squad">{t(($) => $.filters.squads)}</NativeSelectOption>
        </NativeSelect>
        <NativeSelect
          className="w-auto"
          value={category}
          onChange={(e) => setCategory(e.target.value)}
        >
          <NativeSelectOption value="">{t(($) => $.filters.category)}</NativeSelectOption>
          {MARKETPLACE_CATEGORIES.map((c) => (
            <NativeSelectOption key={c} value={c}>
              {t(($) => $.categories[c])}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect
          className="w-auto"
          value={sort}
          onChange={(e) => setSort(e.target.value as MarketplaceSort)}
        >
          <NativeSelectOption value="latest">{t(($) => $.filters.sort_latest)}</NativeSelectOption>
          <NativeSelectOption value="downloads">{t(($) => $.filters.sort_downloads)}</NativeSelectOption>
          <NativeSelectOption value="name">{t(($) => $.filters.sort_name)}</NativeSelectOption>
        </NativeSelect>
      </div>

      <div className="min-h-0 flex-1 overflow-auto px-5 pb-6">
        {isLoading ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-40 rounded-lg" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <CollectionPageState
            icon={Store}
            title={t(($) => $.page.no_matches)}
            actions={
              search || category || kind ? undefined : (
                <Button size="sm" onClick={() => setPublishOpen(true)}>
                  <Plus aria-hidden="true" className="size-3.5" />
                  {t(($) => $.page.publish_button)}
                </Button>
              )
            }
          />
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {items.map((item) => (
              <div
                key={item.id}
                {...rowLink(p.marketplaceDetail(item.id), item.title)}
                className="group flex cursor-pointer flex-col gap-2 rounded-lg border bg-card p-4 transition-colors hover:border-border hover:bg-accent/40"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="rounded-full bg-muted px-2 py-0.5 text-caption font-medium text-muted-foreground">
                    {item.kind === "agent"
                      ? t(($) => $.filters.agents)
                      : t(($) => $.filters.squads)}
                  </span>
                  <span className="text-caption tabular-nums text-muted-foreground">
                    {t(($) => $.card.downloads, { count: item.downloads })}
                  </span>
                </div>
                <div className="min-w-0">
                  <h3 className="truncate text-body font-medium">
                    {item.title}
                  </h3>
                  {item.summary ? (
                    <p className="mt-1 line-clamp-2 text-caption text-muted-foreground">
                      {item.summary}
                    </p>
                  ) : null}
                </div>
                <div className="mt-auto flex items-center justify-between gap-2 pt-1">
                  <span className="truncate text-caption text-muted-foreground">
                    {item.author_display_name}
                  </span>
                  <span className="shrink-0 font-mono text-caption text-muted-foreground">
                    {t(($) => $.card.version, { version: item.version })}
                  </span>
                </div>
                {item.tags.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {item.tags.slice(0, 4).map((tag) => (
                      <span
                        key={tag}
                        className="rounded bg-muted px-1.5 py-0.5 text-caption text-muted-foreground"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </div>

      <PublishListingDialog open={publishOpen} onOpenChange={setPublishOpen} />
    </div>
  );
}
