import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { FeedbackSummary, ListFeedbacksResponse } from "@multica/core/feedback";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { FeedbackCenterPage } from "./feedback-center-page";

const mocks = vi.hoisted(() => {
  const makeResponse = (items: FeedbackSummary[]): ListFeedbacksResponse => ({
    items,
    total: items.length,
    page: 1,
    page_size: 20,
    has_more: false,
  });
  const base: FeedbackSummary = {
    id: "fb-1",
    workspace_id: "ws-1",
    creator_id: "u-1",
    creator_name: "cy",
    creator_avatar_url: null,
    title: "Agent batch creation",
    description: "Support creating agents in batch.",
    type: "feature",
    vote_count: 32,
    comment_count: 8,
    my_vote: false,
    created_at: "2026-08-13T08:00:00Z",
    updated_at: "2026-08-13T08:00:00Z",
  };
  const bug: FeedbackSummary = {
    ...base,
    id: "fb-2",
    title: "Login is slow",
    type: "bug",
    vote_count: 2,
    comment_count: 1,
  };
  return {
    listResponse: makeResponse([base, bug]),
    emptyResponse: makeResponse([]),
    listFeedbacks: vi.fn(),
    infiniteQueryImpl: vi.fn(),
  };
});

function makeNavAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
  } as unknown as NavigationAdapter;
}

function infiniteQueryState(overrides: Record<string, unknown> = {}) {
  return {
    data: undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    ...overrides,
  };
}

vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: (options: unknown) => mocks.infiniteQueryImpl(options),
  useMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useQueryClient: () => ({ setQueriesData: vi.fn(), invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: { listFeedbacks: (...args: unknown[]) => mocks.listFeedbacks(...args) },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    feedback: () => "/acme/feedback",
    feedbackDetail: (id: string) => `/acme/feedback/${id}`,
  }),
}));

vi.mock("../issues/components/infinite-scroll-sentinel", () => ({
  InfiniteScrollSentinel: () => null,
}));

// The dialog shell is flattened so the list stays testable without the portal.
// It still respects the `open` prop, so a closed submit dialog's inner type
// buttons don't duplicate the toolbar's.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children: React.ReactNode }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

describe("FeedbackCenterPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.infiniteQueryImpl.mockReturnValue(infiniteQueryState());
  });

  function renderPage() {
    return renderWithI18n(
      <NavigationProvider value={makeNavAdapter()}>
        <FeedbackCenterPage />
      </NavigationProvider>,
    );
  }

  it("shows a loading skeleton while the list is pending", () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({ isLoading: true, data: undefined }),
    );
    renderPage();
    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
  });

  it("renders the header, tabs, search box, sort control and feedback cards", () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({
        data: { pages: [mocks.listResponse], pageParams: [1] },
      }),
    );
    renderPage();

    expect(screen.getByRole("heading", { name: "Feedback Center" })).toBeInTheDocument();
    expect(screen.getByText("Agent batch creation")).toBeInTheDocument();
    expect(screen.getByText("Login is slow")).toBeInTheDocument();
    // Type filter tabs
    expect(screen.getByRole("button", { name: "All" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bug report" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Feature suggestion" })).toBeInTheDocument();
    // Search + sort
    expect(screen.getByPlaceholderText("Search feedback…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sort/ })).toBeInTheDocument();
    // Submit action
    expect(screen.getByRole("button", { name: /Submit feedback/ })).toBeInTheDocument();
  });

  it("re-runs the query with the selected type filter when a tab is clicked", async () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({
        data: { pages: [mocks.emptyResponse], pageParams: [1] },
      }),
    );
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Bug report" }));
    await waitFor(() => {
      // The query key must carry the type filter after the click.
      const call = mocks.infiniteQueryImpl.mock.calls.at(-1)?.[0] as {
        queryKey?: readonly unknown[];
      };
      expect(call?.queryKey).toEqual([
        "feedback",
        "ws-1",
        "list",
        { type: "bug", keyword: "", sort: "latest" },
      ]);
    });
  });

  it("shows the empty state when there are no feedback items", () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({
        data: { pages: [mocks.emptyResponse], pageParams: [1] },
      }),
    );
    renderPage();
    expect(screen.getByText("No feedback yet")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Submit the first feedback/ }),
    ).toBeInTheDocument();
  });

  it("shows the search empty state when a keyword matches nothing", async () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({
        data: { pages: [mocks.emptyResponse], pageParams: [1] },
      }),
    );
    renderPage();
    fireEvent.change(screen.getByPlaceholderText("Search feedback…"), {
      target: { value: "nothing here" },
    });
    await waitFor(
      () => {
        expect(screen.getByText("No feedback found")).toBeInTheDocument();
      },
      { timeout: 1000 },
    );
  });

  it("shows an error state with a reload action when the list request fails", () => {
    mocks.infiniteQueryImpl.mockReturnValue(
      infiniteQueryState({ isError: true, data: undefined }),
    );
    renderPage();
    expect(screen.getByText("Failed to load feedback")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
  });
});
