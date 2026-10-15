import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
  mutate: vi.fn(),
  addVote: vi.fn(),
  removeVote: vi.fn(),
  invalidate: vi.fn(),
  setQueriesData: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useMutation: ({ mutationFn }: { mutationFn: (...args: unknown[]) => unknown }) => ({
    mutateAsync: (...args: unknown[]) => mocks.mutateAsync(mutationFn, ...args),
    mutate: (...args: unknown[]) => mocks.mutate(...args),
    isPending: false,
  }),
  useQueryClient: () => ({
    setQueriesData: mocks.setQueriesData,
    invalidateQueries: mocks.invalidate,
  }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "u-1", name: "cy" } }),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children: React.ReactNode }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { FeedbackSubmitDialog } from "./feedback-submit-dialog";
import { FeedbackVoteButton } from "./feedback-vote-button";

describe("FeedbackSubmitDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.mutateAsync.mockImplementation((fn: (...args: unknown[]) => Promise<unknown>, input: unknown) =>
      fn(input),
    );
    mocks.mutateAsync.mockResolvedValue({ id: "new-fb" });
  });

  function renderOpen() {
    return renderWithI18n(
      <FeedbackSubmitDialog open onOpenChange={vi.fn()} />,
    );
  }

  it("disables submit until a title and description are provided", () => {
    renderOpen();
    const submit = screen.getByRole("button", { name: "Submit" });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "A great idea" },
    });
    expect(screen.getByRole("button", { name: "Submit" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Here is why it matters." },
    });
    expect(screen.getByRole("button", { name: "Submit" })).not.toBeDisabled();
  });

  it("defaults to the feature type and submits type/title/description", async () => {
    renderOpen();
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Batch agents" },
    });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Allow creating many agents at once." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => {
      expect(mocks.mutateAsync).toHaveBeenCalled();
    });
    const [, input] = mocks.mutateAsync.mock.calls[0] as unknown[];
    expect(input).toEqual({
      type: "feature",
      title: "Batch agents",
      description: "Allow creating many agents at once.",
    });
  });

  it("switches the type via the segmented control", async () => {
    renderOpen();
    fireEvent.click(screen.getByRole("button", { name: "Bug report" }));
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Crash" },
    });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "App crashes on startup." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => {
      const [, input] = mocks.mutateAsync.mock.calls[0] as unknown[];
      expect((input as { type: string }).type).toBe("bug");
    });
  });
});

describe("FeedbackVoteButton", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  function renderVote(props: {
    myVote?: boolean;
    voteCount?: number;
    disabled?: boolean;
  } = {}) {
    return renderWithI18n(
      <FeedbackVoteButton
        feedbackId="fb-1"
        voteCount={props.voteCount ?? 5}
        myVote={props.myVote ?? false}
        disabled={props.disabled}
      />,
    );
  }

  it("shows the vote count and the support label when not voted", () => {
    renderVote();
    expect(screen.getByText("5")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Support/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Support/ })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("shows the supported label and pressed state once voted", () => {
    renderVote({ myVote: true, voteCount: 6 });
    expect(screen.getByRole("button", { name: /Supported/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("is disabled for logged-out users", () => {
    renderVote({ disabled: true });
    expect(screen.getByRole("button", { name: /Support/ })).toBeDisabled();
  });

  it("calls the add-vote mutation on click when not voted", () => {
    renderVote();
    fireEvent.click(screen.getByRole("button", { name: /Support/ }));
    expect(mocks.mutate).toHaveBeenCalledWith(
      "fb-1",
      expect.objectContaining({ onError: expect.any(Function), onSuccess: expect.any(Function) }),
    );
  });
});
