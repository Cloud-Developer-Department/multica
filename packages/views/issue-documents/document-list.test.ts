import { describe, expect, it } from "vitest";
import {
  compareDocuments,
  DEFAULT_DOCUMENT_SORT,
  type DocumentSort,
  type IssueDocumentSummary,
} from "./components/document-list";

const base: IssueDocumentSummary = {
  id: "1",
  workspace_id: "ws",
  issue_id: "i1",
  issue_identifier: "MUL-1",
  issue_title: "Issue one",
  type: "requirements",
  title: "requirement.md",
  version: 1,
  status: "submitted",
  content_type: "markdown",
  file_attachment_id: null,
  author_type: "member",
  author_id: "u1",
  author_name: "Alice",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

function doc(overrides: Partial<IssueDocumentSummary>): IssueDocumentSummary {
  return { ...base, ...overrides };
}

describe("compareDocuments", () => {
  it("sorts by updated_at desc by default", () => {
    const a = doc({ id: "a", updated_at: "2026-01-01T00:00:00Z" });
    const b = doc({ id: "b", updated_at: "2026-01-03T00:00:00Z" });
    expect(compareDocuments(a, b, DEFAULT_DOCUMENT_SORT)).toBeGreaterThan(0);
    expect(compareDocuments(b, a, DEFAULT_DOCUMENT_SORT)).toBeLessThan(0);
  });

  it("sorts by title ascending when requested", () => {
    const sort: DocumentSort = { field: "title", direction: "asc" };
    const a = doc({ id: "a", title: "aaa.md" });
    const b = doc({ id: "b", title: "zzz.md" });
    expect(compareDocuments(a, b, sort)).toBeLessThan(0);
  });

  it("sorts by status descending when requested", () => {
    const sort: DocumentSort = { field: "status", direction: "desc" };
    const a = doc({ id: "a", status: "submitted" });
    const b = doc({ id: "b", status: "approved" });
    expect(compareDocuments(a, b, sort)).toBeLessThan(0);
  });
});
