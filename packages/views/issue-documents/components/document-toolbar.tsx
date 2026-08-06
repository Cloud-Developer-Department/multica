"use client";

import { FileStack, Search } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import type { IssueDocumentStatus, IssueDocumentType } from "@multica/core/types";
import { useT } from "../../i18n";
import { CollectionPageHeader } from "../../layout/collection-page";

export const ISSUE_DOCUMENT_TYPES: IssueDocumentType[] = [
  "requirements",
  "architecture",
  "development",
  "testing",
  "code_review",
  "security",
  "documentation",
  "deployment",
  "other",
];

export const ISSUE_DOCUMENT_STATUSES: IssueDocumentStatus[] = [
  "draft",
  "submitted",
  "approved",
  "rejected",
  "superseded",
];

export type DocumentTypeFilter = IssueDocumentType | "all";
export type DocumentStatusFilter = IssueDocumentStatus | "all";

interface DocumentToolbarProps {
  totalCount: number;
  typeFilter: DocumentTypeFilter;
  statusFilter: DocumentStatusFilter;
  search: string;
  onTypeFilterChange: (value: DocumentTypeFilter) => void;
  onStatusFilterChange: (value: DocumentStatusFilter) => void;
  onSearchChange: (value: string) => void;
}

/** Page header + filter/search toolbar for the Issue Documents tab. */
export function DocumentToolbar({
  totalCount,
  typeFilter,
  statusFilter,
  search,
  onTypeFilterChange,
  onStatusFilterChange,
  onSearchChange,
}: DocumentToolbarProps) {
  const { t } = useT("issue-documents");

  const typeItems = [
    { value: "all" as const, label: t(($) => $.filters.all_types) },
    ...ISSUE_DOCUMENT_TYPES.map((type) => ({
      value: type,
      label: t(($) => $.types[type]),
    })),
  ];
  const statusItems = [
    { value: "all" as const, label: t(($) => $.filters.all_statuses) },
    ...ISSUE_DOCUMENT_STATUSES.map((status) => ({
      value: status,
      label: t(($) => $.statuses[status]),
    })),
  ];

  return (
    <>
      <CollectionPageHeader
        icon={FileStack}
        title={t(($) => $.page.title)}
        count={totalCount}
        description={t(($) => $.page.tagline)}
      />
      <div className="flex flex-wrap items-center gap-2 border-b px-5 py-2">
        <Select
          items={typeItems}
          value={typeFilter}
          onValueChange={(v) => onTypeFilterChange((v as DocumentTypeFilter) ?? "all")}
        >
          <SelectTrigger className="h-8 w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {typeItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          items={statusItems}
          value={statusFilter}
          onValueChange={(v) =>
            onStatusFilterChange((v as DocumentStatusFilter) ?? "all")
          }
        >
          <SelectTrigger className="h-8 w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {statusItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative min-w-0 flex-1 md:max-w-xs">
          <Search
            aria-hidden="true"
            className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t(($) => $.filters.search)}
            className="h-8 pl-8"
          />
        </div>
      </div>
    </>
  );
}
