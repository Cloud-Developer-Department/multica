import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  ArtifactListResponse,
  CreateArtifactRequest,
  ReviewArtifactRequest,
  ReviewArtifactResponse,
} from "../types";

export const artifactKeys = {
  all: (wsId: string) => ["artifacts", wsId] as const,
  list: (wsId: string) => [...artifactKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...artifactKeys.all(wsId), "detail", id] as const,
  versions: (wsId: string, id: string) => [...artifactKeys.all(wsId), "versions", id] as const,
  reviews: (wsId: string, id: string) => [...artifactKeys.all(wsId), "reviews", id] as const,
  stats: (wsId: string) => [...artifactKeys.all(wsId), "stats"] as const,
};

export const reviewQueueKeys = {
  all: (wsId: string) => ["reviews", "queue", wsId] as const,
};

export function artifactListOptions(wsId: string, params?: { status?: string; type?: string }) {
  return queryOptions({
    queryKey: [...artifactKeys.list(wsId), params?.status, params?.type],
    queryFn: (): Promise<ArtifactListResponse> => api.listArtifacts(params),
  });
}

export function artifactDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: artifactKeys.detail(wsId, id),
    queryFn: () => api.getArtifact(id),
  });
}

export function artifactVersionsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: artifactKeys.versions(wsId, id),
    queryFn: () => api.getArtifactVersions(id),
  });
}

export function artifactReviewsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: artifactKeys.reviews(wsId, id),
    queryFn: () => api.getArtifactReviews(id),
  });
}

export function artifactDiffOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: [...artifactKeys.detail(wsId, id), "diff"],
    queryFn: () => api.getArtifactDiff(id, { from: 1 }),
  });
}

export function artifactStatsOptions(wsId: string) {
  return queryOptions({
    queryKey: artifactKeys.stats(wsId),
    queryFn: () => api.getArtifactStats(),
  });
}

export function reviewQueueOptions(wsId: string) {
  return queryOptions({
    queryKey: reviewQueueKeys.all(wsId),
    queryFn: () => api.getReviewQueue(),
  });
}

export function useCreateArtifact(wsId: string, nodeId?: string, _workflowId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateArtifactRequest) => api.createArtifact(data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: artifactKeys.list(wsId) });
      void qc.invalidateQueries({ queryKey: reviewQueueKeys.all(wsId) });
      if (nodeId) void qc.invalidateQueries({ queryKey: ["workflows"] });
    },
  });
}

export function useReviewArtifact(wsId: string, artifactId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: ReviewArtifactRequest): Promise<ReviewArtifactResponse> => api.reviewArtifact(artifactId, data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: artifactKeys.detail(wsId, artifactId) });
      void qc.invalidateQueries({ queryKey: artifactKeys.reviews(wsId, artifactId) });
      void qc.invalidateQueries({ queryKey: artifactKeys.list(wsId) });
      void qc.invalidateQueries({ queryKey: reviewQueueKeys.all(wsId) });
      void qc.invalidateQueries({ queryKey: artifactKeys.stats(wsId) });
      void qc.invalidateQueries({ queryKey: ["workflows"] });
    },
  });
}
