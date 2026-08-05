import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateWorkflowRequest, WorkflowNode } from "../types";

export const workflowKeys = {
  all: (wsId: string) => ["workflows", wsId] as const,
  list: (wsId: string) => [...workflowKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...workflowKeys.all(wsId), "detail", id] as const,
  nodes: (wsId: string, id: string) => [...workflowKeys.all(wsId), "nodes", id] as const,
  transitions: (wsId: string, id: string) => [...workflowKeys.all(wsId), "transitions", id] as const,
};

export function workflowListOptions(wsId: string, params?: { status?: string }) {
  return queryOptions({
    queryKey: [...workflowKeys.list(wsId), params?.status],
    queryFn: () => api.listWorkflows(params),
  });
}

export function workflowDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(wsId, id),
    queryFn: () => api.getWorkflow(id),
  });
}

export function workflowNodesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: workflowKeys.nodes(wsId, id),
    queryFn: (): Promise<WorkflowNode[]> => api.getWorkflowNodes(id),
  });
}

export function workflowTransitionsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: workflowKeys.transitions(wsId, id),
    queryFn: () => api.getWorkflowTransitions(id),
  });
}

export function useCreateWorkflow(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateWorkflowRequest) => api.createWorkflow(data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useAdvanceWorkflow(wsId: string, id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (reason?: string) => api.advanceWorkflow(id, reason),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, id) });
      void qc.invalidateQueries({ queryKey: workflowKeys.nodes(wsId, id) });
      void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}
