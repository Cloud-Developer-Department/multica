"use client";

import { useEffect, useMemo, useState } from "react";
import {
  ChevronDown,
  MessageCircle,
  Plus,
  Search,
  MessageSquare,
} from "lucide-react";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import {
  FEEDBACK_TYPES,
  feedbackKeys,
  type FeedbackSort,
  type FeedbackType,
} from "@multica/core/feedback";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../layout/collection-page";
import { InfiniteScrollSentinel } from "../issues/components/infinite-scroll-sentinel";
import { useT } from "../i18n";
import { FeedbackCard } from "./feedback-card";
import { FeedbackSubmitDialog } from "./feedback-submit-dialog";
import { useFeedbackTypeLabels } from "./feedback-types";
import { cn } from "@multica/ui/lib/utils";

const PAGE_SIZE = 20;

/** No-feedback vs no-search-result empty state. */
function isFiltered(
  type?: FeedbackType,
  keyword?: string,
): boolean {
  return Boolean(type) || (keyword?.trim().length ?? 0) > 0;
}

export function FeedbackCenterPage() {
  const { t } = useT("feedback");
  const wsId = useWorkspaceId();
  const typeLabels = useFeedbackTypeLabels();

  const [type, setType] = useState<FeedbackType | undefined>(undefined);
  const [sort, setSort] = useState<FeedbackSort>("latest");
  const [keyword, setKeyword] = useState("");
  const [submitOpen, setSubmitOpen] = useState(false);

  // Debounce the search input so keystrokes don't fire a request each time.
  const [debouncedKeyword, setDebouncedKeyword] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedKeyword(keyword.trim()), 300);
    return () => clearTimeout(timer);
  }, [keyword]);

  const filters = useMemo(
    () => ({ type, keyword: debouncedKeyword, sort }),
    [type, debouncedKeyword, sort],
  );

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: feedbackKeys.list(wsId, filters),
    initialPageParam: 1,
    queryFn: ({ pageParam }) =>
      api.listFeedbacks({
        ...filters,
        page: pageParam,
        page_size: PAGE_SIZE,
      }),
    getNextPageParam: (lastPage) => (lastPage.has_more ? lastPage.page + 1 : undefined),
    enabled: !!wsId,
  });

  const items = useMemo(
    () => data?.pages.flatMap((p) => p.items) ?? [],
    [data],
  );
  const total = data?.pages[0]?.total ?? 0;

  const filtered = isFiltered(type, debouncedKeyword);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={MessageCircle}
        title={t(($) => $.page.title)}
        count={total}
        description={t(($) => $.page.subtitle)}
        actions={
          <Button
            size="sm"
            onClick={() => setSubmitOpen(true)}
            className="h-8 w-8 gap-1 px-0 md:w-auto md:px-2.5"
          >
            <Plus className="size-3.5" />
            <span className="hidden md:inline">{t(($) => $.page.submit)}</span>
          </Button>
        }
      />

      {/* Filter + search + sort toolbar */}
      <div className="flex flex-wrap items-center gap-2 px-5 pb-2">
        <div className="flex flex-wrap items-center gap-1">
          <Button
            type="button"
            size="sm"
            variant={!type ? "default" : "ghost"}
            className={cn("h-7 px-2.5 text-caption", type && "text-muted-foreground")}
            aria-pressed={!type}
            onClick={() => setType(undefined)}
          >
            {t(($) => $.types.all)}
          </Button>
          {FEEDBACK_TYPES.map((value) => (
            <Button
              key={value}
              type="button"
              size="sm"
              variant={type === value ? "default" : "ghost"}
              className={cn("h-7 px-2.5 text-caption", type !== value && "text-muted-foreground")}
              aria-pressed={type === value}
              onClick={() => setType(type === value ? undefined : value)}
            >
              {typeLabels[value]}
            </Button>
          ))}
        </div>

        <div className="ml-auto flex items-center gap-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              aria-label={t(($) => $.page.search_placeholder)}
              placeholder={t(($) => $.page.search_placeholder)}
              className="h-8 w-52 pl-8 text-body"
            />
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="sm" className="h-8 gap-1 px-2.5 text-caption text-muted-foreground">
                  {t(($) => $.page.sort_label)}
                  <ChevronDown className="size-3.5" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuRadioGroup
                value={sort}
                onValueChange={(v) => setSort(v as FeedbackSort)}
              >
                <DropdownMenuRadioItem value="latest">
                  {t(($) => $.page.sort_latest)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="hot">
                  {t(($) => $.page.sort_hot)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="comments">
                  {t(($) => $.page.sort_comments)}
                </DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Body */}
      {isLoading ? (
        <div className="space-y-2 px-5 pt-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-20 w-full rounded-md" />
          ))}
        </div>
      ) : isError ? (
        <CollectionPageState
          icon={MessageSquare}
          title={t(($) => $.page.load_failed)}
          tone="destructive"
          actions={
            <Button size="sm" variant="outline" onClick={() => refetch()}>
              {t(($) => $.page.reload)}
            </Button>
          }
        />
      ) : items.length === 0 ? (
        filtered ? (
          <CollectionPageState
            icon={Search}
            title={t(($) => $.page.empty_search_title)}
            description={t(($) => $.page.empty_search_description)}
          />
        ) : (
          <CollectionPageState
            icon={MessageCircle}
            title={t(($) => $.page.empty_title)}
            description={t(($) => $.page.empty_description)}
            actions={
              <Button size="sm" variant="outline" onClick={() => setSubmitOpen(true)}>
                {t(($) => $.page.empty_action)}
              </Button>
            }
          />
        )
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="flex flex-col gap-2 px-5 py-2">
            {items.map((item) => (
              <FeedbackCard key={item.id} feedback={item} />
            ))}
          </div>
          <InfiniteScrollSentinel
            onVisible={() => {
              if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
            }}
            loading={isFetchingNextPage}
            label={t(($) => $.page.load_more)}
            className="flex items-center justify-center gap-1.5 py-3 text-caption text-muted-foreground"
          />
        </div>
      )}

      <FeedbackSubmitDialog open={submitOpen} onOpenChange={setSubmitOpen} />
    </div>
  );
}
