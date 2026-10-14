import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListFeedbacksParams, FeedbackSort, FeedbackType } from "./types";

export const feedbackKeys = {
  all: (wsId: string) => ["feedback", wsId] as const,
  list: (wsId: string, filters: { type?: FeedbackType; keyword?: string; sort?: FeedbackSort }) =>
    [...feedbackKeys.all(wsId), "list", filters] as const,
  detail: (wsId: string, id: string) =>
    [...feedbackKeys.all(wsId), "detail", id] as const,
  comments: (wsId: string, id: string) =>
    [...feedbackKeys.all(wsId), "comments", id] as const,
};

export function feedbackCenterListOptions(wsId: string, filters: ListFeedbacksParams) {
  return queryOptions({
    queryKey: feedbackKeys.list(wsId, filters),
    queryFn: () => api.listFeedbacks(filters),
  });
}

export function feedbackDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: feedbackKeys.detail(wsId, id),
    queryFn: () => api.getFeedback(id),
  });
}

export function feedbackCommentsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: feedbackKeys.comments(wsId, id),
    queryFn: () => api.listFeedbackComments(id),
  });
}
