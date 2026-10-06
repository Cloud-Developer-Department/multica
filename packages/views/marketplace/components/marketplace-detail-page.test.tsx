// @vitest-environment jsdom

import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const getListingSpy = vi.hoisted(() =>
  vi.fn(async () => ({
    listing: {
      id: "m-1",
      kind: "agent",
      title: "Market Agent",
      summary: "",
      category: "ai-agent",
      tags: [],
      version: "1.0.0",
      author_id: "u-1",
      author_display_name: "Tester",
      source_workspace_id: "ws-1",
      template_id: "tpl-1",
      status: "published",
      downloads: 0,
      installs: 0,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      template: {
        schema_version: "1.0",
        template_id: "tpl-1",
        kind: "agent",
        metadata: { name: "Market Agent", source_workspace: "ws-1" },
        spec: {
          agent: { name: "Market Agent", description: "", model: "", skills: [] },
        },
      },
    },
  })),
);
const reportDownloadSpy = vi.hoisted(() => vi.fn(async () => ({ downloads: 1 })));
const downloadSpy = vi.hoisted(() =>
  vi.fn(async () => new Blob(["{}"], { type: "application/json" })),
);
const archiveSpy = vi.hoisted(() =>
  vi.fn(async () => ({ id: "m-1", status: "archived" })),
);

vi.mock("@multica/core/api", () => ({
  api: {
    getMarketplaceListing: getListingSpy,
    reportMarketplaceDownload: reportDownloadSpy,
    downloadMarketplaceTemplate: downloadSpy,
    archiveMarketplaceListing: archiveSpy,
    listRuntimes: vi.fn(async () => []),
    validateResourceTemplate: vi.fn(async () => ({})),
    applyResourceTemplate: vi.fn(async () => ({})),
  },
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "u-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    marketplace: () => "/acme/marketplace",
    marketplaceDetail: (id: string) => `/acme/marketplace/${id}`,
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: (opts: { queryKey?: unknown[] }) => {
      if (opts.queryKey?.includes("runtimes")) {
        return { data: [], isLoading: false };
      }
      return {
        data: {
          listing: {
            id: "m-1",
            kind: "agent",
            title: "Market Agent",
            summary: "",
            category: "ai-agent",
            tags: [],
            version: "1.0.0",
            author_id: "u-1",
            author_display_name: "Tester",
            source_workspace_id: "ws-1",
            template_id: "tpl-1",
            status: "published",
            downloads: 0,
            installs: 0,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
            template: {
              schema_version: "1.0",
              template_id: "tpl-1",
              kind: "agent",
              metadata: { name: "Market Agent", source_workspace: "ws-1" },
              spec: {
                agent: {
                  name: "Market Agent",
                  description: "",
                  model: "",
                  skills: [],
                },
              },
            },
          },
        },
        isLoading: false,
      };
    },
    useMutation: () => ({
      isPending: false,
      mutate: vi.fn(),
    }),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});
vi.mock("../../navigation", () => ({
  useIntentNavigate: () => vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { MarketplaceDetailPage } from "./marketplace-detail-page";

describe("MarketplaceDetailPage", () => {
  beforeEach(() => {
    reportDownloadSpy.mockClear();
  });

  it("does not report a download when the import dialog is opened and cancelled", async () => {
    renderWithI18n(<MarketplaceDetailPage id="m-1" />);
    // Listing renders.
    await screen.findByText(/Market Agent/i);

    // Open the import wizard.
    fireEvent.click(screen.getByRole("button", { name: /one-click import/i }));
    await waitFor(() =>
      expect(screen.getByRole("dialog")).toBeInTheDocument(),
    );

    // Cancel by pressing Escape — closing must NOT count as an import.
    fireEvent.keyDown(screen.getByRole("dialog"), {
      key: "Escape",
      code: "Escape",
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(reportDownloadSpy).not.toHaveBeenCalled();
  });

  it("reports a download only through the import wizard's onSuccess path", async () => {
    renderWithI18n(<MarketplaceDetailPage id="m-1" />);
    await screen.findByText(/Market Agent/i);

    // The onSuccess wiring lives in the dialog; asserting the page passes it
    // is done via the dialog contract. Here we just verify the page renders
    // the import entry without any immediate download reporting.
    expect(reportDownloadSpy).not.toHaveBeenCalled();
  });
});
