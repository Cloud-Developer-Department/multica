"use client";

import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import type { IssueDocumentSummary } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useT, useTimeAgo } from "../../i18n";

export type DocumentSortField = "updated_at" | "title" | "type" | "status" | "version";
export type DocumentSortDirection = "asc" | "desc";

export interface DocumentSort {
  field: DocumentSortField;
  direction: DocumentSortDirection;
}

export const DEFAULT_DOCUMENT_SORT: DocumentSort = {
  field: "updated_at",
  direction: "desc",
};

export function compareDocuments(a: IssueDocumentSummary, b: IssueDocumentSummary, sort: DocumentSort): number {
  const dir = sort.direction === "asc" ? 1 : -1;
  let result = 0;
  switch (sort.field) {
    case "title":
      result = a.title.localeCompare(b.title);
      break;
    case "type":
      result = a.type.localeCompare(b.type);
      break;
    case "status":
      result = a.status.localeCompare(b.status);
      break;
    case "version":
      result = a.version - b.version;
      break;
    case "updated_at":
    default:
      result = Date.parse(a.updated_at) - Date.parse(b.updated_at);
      break;
  }
  return result * dir;
}

function SortIcon({ sort, field }: { sort: DocumentSort; field: DocumentSortField }) {
  if (sort.field !== field) return <ArrowUpDown className="size-3 text-muted-foreground/50" />;
  return sort.direction === "asc" ? (
    <ArrowUp className="size-3 text-muted-foreground" />
  ) : (
    <ArrowDown className="size-3 text-muted-foreground" />
  );
}

interface SortableHeadProps {
  sort: DocumentSort;
  field: DocumentSortField;
  label: string;
  onSort: (field: DocumentSortField) => void;
  className?: string;
}

function SortableHead({ sort, field, label, onSort, className }: SortableHeadProps) {
  return (
    <TableHead className={className}>
      <button
        type="button"
        onClick={() => onSort(field)}
        className="inline-flex items-center gap-1 hover:text-foreground"
      >
        {label}
        <SortIcon sort={sort} field={field} />
      </button>
    </TableHead>
  );
}

interface DocumentListProps {
  items: IssueDocumentSummary[];
  sort: DocumentSort;
  onSort: (field: DocumentSortField) => void;
  onSelect: (document: IssueDocumentSummary) => void;
}

/** Read-only list of issue-flow documents with sortable columns. */
export function DocumentList({ items, sort, onSort, onSelect }: DocumentListProps) {
  const { t } = useT("issue-documents");
  const timeAgo = useTimeAgo();

  if (items.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center py-16 text-sm text-muted-foreground">
        {t(($) => $.table.no_matches)}
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <SortableHead sort={sort} field="title" label={t(($) => $.table.title)} onSort={onSort} />
            <SortableHead sort={sort} field="type" label={t(($) => $.table.type)} onSort={onSort} />
            <TableHead>{t(($) => $.table.issue)}</TableHead>
            <SortableHead sort={sort} field="version" label={t(($) => $.table.version)} onSort={onSort} />
            <SortableHead sort={sort} field="status" label={t(($) => $.table.status)} onSort={onSort} />
            <TableHead>{t(($) => $.table.author)}</TableHead>
            <SortableHead sort={sort} field="updated_at" label={t(($) => $.table.updated)} onSort={onSort} />
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((doc) => (
            <TableRow
              key={doc.id}
              className="cursor-pointer"
              onClick={() => onSelect(doc)}
            >
              <TableCell className="max-w-64">
                <span className="block truncate font-medium">{doc.title}</span>
              </TableCell>
              <TableCell>
                <Badge variant="secondary">{t(($) => $.types[doc.type])}</Badge>
              </TableCell>
              <TableCell>
                <span className="block max-w-56 truncate">
                  {doc.issue_identifier}
                  {doc.issue_title ? ` · ${doc.issue_title}` : ""}
                </span>
              </TableCell>
              <TableCell className="tabular-nums">v{doc.version}</TableCell>
              <TableCell>
                <Badge
                  variant={
                    doc.status === "approved"
                      ? "default"
                      : doc.status === "superseded" || doc.status === "rejected"
                        ? "outline"
                        : "secondary"
                  }
                >
                  {t(($) => $.statuses[doc.status])}
                </Badge>
              </TableCell>
              <TableCell className="max-w-32">
                <span className="block truncate">{doc.author_name || "—"}</span>
              </TableCell>
              <TableCell className="whitespace-nowrap text-xs text-muted-foreground tabular-nums">
                {timeAgo(doc.updated_at)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
