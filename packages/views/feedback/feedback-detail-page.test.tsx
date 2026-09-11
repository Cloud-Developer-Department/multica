import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Feedback } from "@multica/core/feedback";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { FeedbackDetailPage } from "./feedback-detail-page";

const mocks = vi.hoisted(() => {
  const feedback: Feedback = {
    id: "fb-1",
    workspace_id: "ws-1",
    creator_id: "u-1",
    creator_name: "cy",
    creator_avatar_url: null,
    title: "Agent batch creation",
    description: "Support creating agents in batch, including the description.",
    type: "feature",
    vote_count: 32,
    comment_count: 1,
    my_vote: false,
    created_at: "2026-08-13T08:00:00Z",
    updated_at: "2026-08-13T08:00:00Z",
  };
  return { feedback, queryImpl: vi.fn() };
});

function makeNavAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
  } as unknown as NavigationAdapter;
}

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: unknown) => mocks.queryImpl(options),
  useMutation: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn(), setQueriesData: vi.fn() }),
  queryOptions: <T,>(options: T) => options,
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: {} };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    feedback: () => "/acme/feedback",
    feedbackDetail: (id: string) => `/acme/feedback/${id}`,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "u-1", name: "cy" } }),
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null) => url,
}));

import { ApiError } from "@multica/core/api";

describe("FeedbackDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default: comments query returns an empty list.
    mocks.queryImpl.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      const key = options?.queryKey;
      const kind = key?.[2];
      if (kind === "detail") {
        return { data: mocks.feedback, isLoading: false, isError: false, refetch: vi.fn() };
      }
      return { data: [], isLoading: false, isError: false, refetch: vi.fn() };
    });
  });

  function renderPage(id = "fb-1") {
    return renderWithI18n(
      <NavigationProvider value={makeNavAdapter()}>
        <FeedbackDetailPage feedbackId={id} />
      </NavigationProvider>,
    );
  }

  it("renders the title, description, vote button and comments section", () => {
    renderPage();
    expect(screen.getByRole("heading", { name: "Agent batch creation" })).toBeInTheDocument();
    expect(
      screen.getByText("Support creating agents in batch, including the description."),
    ).toBeInTheDocument();
    expect(screen.getByText("Back to feedback center")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Support/ })).toBeInTheDocument();
    expect(screen.getByText("Comments")).toBeInTheDocument();
  });

  it("shows a friendly not-found state on 404", () => {
    mocks.queryImpl.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      const key = options?.queryKey;
      const kind = key?.[2];
      if (kind === "detail") {
        return {
          data: undefined,
          isLoading: false,
          isError: true,
          error: new ApiError("not found", 404, "Not Found"),
          refetch: vi.fn(),
        };
      }
      return { data: [], isLoading: false, isError: false, refetch: vi.fn() };
    });
    renderPage("missing");
    expect(screen.getByText("Feedback not found")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
  });

  it("shows a load-failed state for non-404 errors", () => {
    mocks.queryImpl.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      const key = options?.queryKey;
      const kind = key?.[2];
      if (kind === "detail") {
        return {
          data: undefined,
          isLoading: false,
          isError: true,
          error: new ApiError("boom", 500, "Internal Server Error"),
          refetch: vi.fn(),
        };
      }
      return { data: [], isLoading: false, isError: false, refetch: vi.fn() };
    });
    renderPage();
    expect(screen.getByText("Failed to load feedback")).toBeInTheDocument();
  });
});
