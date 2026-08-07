"use client";

import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDocumentInfiniteListOptions } from "@multica/core/issue-documents/queries";
import type { IssueDocumentSummary } from "@multica/core/types";
import { useT } from "../i18n";
import {
  DocumentDetailDrawer,
} from "./components/document-detail-drawer";
import {
  DocumentList,
  DEFAULT_DOCUMENT_SORT,
  type DocumentSort,
  type DocumentSortField,
} from "./components/document-list";
import {
  DocumentListEmpty,
  DocumentListError,
  DocumentListNoMatches,
  DocumentListSkeleton,
} from "./components/document-states";
import {
  DocumentToolbar,
  type DocumentStatusFilter,
  type DocumentTypeFilter,
} from "./components/document-toolbar";

/** Debounce the search input so typing does not fire a request per keystroke. */
function useDebouncedValue(value: string, delay = 300): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

export function IssueDocumentsPage() {
  const { t } = useT("issue-documents");
  const wsId = useWorkspaceId();

  const [typeFilter, setTypeFilter] = useState<DocumentTypeFilter>("all");
  const [statusFilter, setStatusFilter] = useState<DocumentStatusFilter>("all");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<DocumentSort>(DEFAULT_DOCUMENT_SORT);
  const [selected, setSelected] = useState<IssueDocumentSummary | null>(null);

  const debouncedSearch = useDebouncedValue(search);

  const hasActiveFilters =
    typeFilter !== "all" || statusFilter !== "all" || debouncedSearch.trim() !== "";

  const query = useInfiniteQuery(
    issueDocumentInfiniteListOptions(wsId, {
      type: typeFilter === "all" ? undefined : typeFilter,
      status: statusFilter === "all" ? undefined : statusFilter,
      q: debouncedSearch.trim() || undefined,
      // Sorting is server-side (CLO-283 R1): the backend orders before
      // LIMIT/OFFSET, so a non-default sort stays stable across pages. The
      // sort is part of the query key, so changing it refetches from page 0.
      sort: sort.field,
      order: sort.direction,
    }),
  );

  const items = useMemo(
    () => (query.data?.pages ?? []).flatMap((page) => page.items),
    [query.data],
  );
  const total = query.data?.pages[0]?.total ?? 0;

  const handleSort = (field: DocumentSortField) => {
    setSort((prev) =>
      prev.field === field
        ? { field, direction: prev.direction === "asc" ? "desc" : "asc" }
        : { field, direction: "desc" },
    );
  };

  if (query.isError) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <DocumentToolbar
          totalCount={0}
          typeFilter={typeFilter}
          statusFilter={statusFilter}
          search={search}
          onTypeFilterChange={setTypeFilter}
          onStatusFilterChange={setStatusFilter}
          onSearchChange={setSearch}
        />
        <DocumentListError onRetry={() => query.refetch()} />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <DocumentToolbar
        totalCount={total}
        typeFilter={typeFilter}
        statusFilter={statusFilter}
        search={search}
        onTypeFilterChange={setTypeFilter}
        onStatusFilterChange={setStatusFilter}
        onSearchChange={setSearch}
      />

      {query.isLoading ? (
        <DocumentListSkeleton />
      ) : items.length === 0 ? (
        hasActiveFilters ? (
          <DocumentListNoMatches />
        ) : (
          <DocumentListEmpty />
        )
      ) : (
        <>
          <DocumentList
            items={items}
            sort={sort}
            onSort={handleSort}
            onSelect={setSelected}
          />
          {query.hasNextPage ? (
            <div className="flex shrink-0 justify-center border-t p-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={query.isFetchingNextPage}
                onClick={() => query.fetchNextPage()}
              >
                {t(($) => $.page.load_more)}
              </Button>
            </div>
          ) : null}
        </>
      )}

      <DocumentDetailDrawer document={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
