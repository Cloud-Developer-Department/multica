import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueTemplateKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateIssueTemplateRequest,
  UpdateIssueTemplateRequest,
  ListIssueTemplatesResponse,
} from "../types";

/**
 * Create a new issue template. On success the new template is appended to
 * the list cache so the settings tab updates without waiting for the
 * settle-time invalidation refetch.
 */
export function useCreateIssueTemplate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateIssueTemplateRequest) => api.createIssueTemplate(data),
    onSuccess: (template) => {
      qc.setQueryData<ListIssueTemplatesResponse>(
        issueTemplateKeys.list(wsId),
        (old) =>
          old && !old.issue_templates.some((t) => t.id === template.id)
            ? {
                ...old,
                issue_templates: [...old.issue_templates, template],
                total: old.total + 1,
              }
            : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueTemplateKeys.all(wsId) });
    },
  });
}

/**
 * Update an existing template. No optimistic patch — edits happen in a
 * dialog where a round-trip is acceptable, and preset protection / label
 * validation is server-side anyway.
 */
export function useUpdateIssueTemplate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueTemplateRequest) =>
      api.updateIssueTemplate(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueTemplateKeys.all(wsId) });
    },
  });
}

/**
 * Delete a template. Preset templates are protected server-side (409), so
 * the mutation surfaces the error to the caller's onError for a toast.
 */
export function useDeleteIssueTemplate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueTemplate(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueTemplateKeys.all(wsId) });
    },
  });
}
