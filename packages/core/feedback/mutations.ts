import { useMutation } from "@tanstack/react-query";
import { api } from "../api";
import type { FeedbackContext, FeedbackKind } from "./types";

export interface CreateFeedbackInput {
  message: string;
  url?: string;
  workspace_id?: string;
  kind?: FeedbackKind;
  context?: FeedbackContext;
}

export function useCreateFeedback() {
  return useMutation({
    mutationFn: (input: CreateFeedbackInput) => api.createFeedback(input),
  });
}

export interface CreateCenterFeedbackInput {
  type: "bug" | "feature" | "improvement" | "other";
  title: string;
  description: string;
}

export function useCreateCenterFeedback() {
  return useMutation({
    mutationFn: (input: CreateCenterFeedbackInput) =>
      api.createFeedbackCenter(input),
  });
}

export function useAddFeedbackVote() {
  return useMutation({
    mutationFn: (feedbackId: string) => api.addFeedbackVote(feedbackId),
  });
}

export function useRemoveFeedbackVote() {
  return useMutation({
    mutationFn: (feedbackId: string) => api.removeFeedbackVote(feedbackId),
  });
}

export function useCreateFeedbackComment() {
  return useMutation({
    mutationFn: ({ feedbackId, content }: { feedbackId: string; content: string }) =>
      api.createFeedbackComment(feedbackId, content),
  });
}
