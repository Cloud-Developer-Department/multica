"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Copy,
  FileText,
  Lock,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueTemplateListOptions,
  useCreateIssueTemplate,
  useDeleteIssueTemplate,
  useUpdateIssueTemplate,
} from "@multica/core/issue-templates";
import { labelListOptions } from "@multica/core/labels";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { ALL_STATUSES, PRIORITY_ORDER } from "@multica/core/issues/config";
import type {
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
  IssueTemplate,
  CreateIssueTemplateRequest,
  UpdateIssueTemplateRequest,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

const STATUS_ITEMS = ALL_STATUSES.map((s) => ({ value: s, label: s }));
const PRIORITY_ITEMS = PRIORITY_ORDER.map((p) => ({ value: p, label: p }));
const ASSIGNEE_TYPE_ITEMS: { value: IssueAssigneeType; label: string }[] = [
  { value: "member", label: "Member" },
  { value: "agent", label: "Agent" },
  { value: "squad", label: "Squad" },
];

interface TemplateDraft {
  name: string;
  description: string;
  title_template: string;
  body_template: string;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  project_id: string | null;
  stage: number | null;
  label_ids: string[];
  icon: string;
  category: string;
}

const EMPTY_DRAFT: TemplateDraft = {
  name: "",
  description: "",
  title_template: "",
  body_template: "",
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  project_id: null,
  stage: null,
  label_ids: [],
  icon: "",
  category: "",
};

function templateToDraft(t: IssueTemplate): TemplateDraft {
  return {
    name: t.name,
    description: t.description,
    title_template: t.title_template,
    body_template: t.body_template,
    status: t.status,
    priority: t.priority,
    assignee_type: t.assignee_type,
    assignee_id: t.assignee_id,
    project_id: t.project_id,
    stage: t.stage,
    label_ids: t.label_ids,
    icon: t.icon,
    category: t.category,
  };
}

export function TemplatesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<IssueTemplate | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<IssueTemplate | null>(null);

  const { data: templates = [], isLoading } = useQuery(
    issueTemplateListOptions(wsId),
  );
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return templates;
    return templates.filter(
      (tpl) =>
        tpl.name.toLowerCase().includes(normalized) ||
        tpl.description.toLowerCase().includes(normalized) ||
        tpl.category.toLowerCase().includes(normalized),
    );
  }, [templates, query]);

  return (
    <SettingsTab
      title={t(($) => $.templates.title)}
      description={t(($) => $.templates.description)}
    >
      <div className="space-y-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full sm:max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.templates.search_placeholder)}
              className="pl-9"
            />
          </div>
          <Button className="gap-2" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            {t(($) => $.templates.new_template)}
          </Button>
        </div>

        <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
          <div className="hidden grid-cols-[minmax(10rem,1fr)_minmax(10rem,1.2fr)_6rem_7rem_2rem] gap-4 border-b border-surface-border bg-muted/20 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid">
            <span>{t(($) => $.templates.columns.name)}</span>
            <span>{t(($) => $.templates.columns.description)}</span>
            <span>{t(($) => $.templates.columns.category)}</span>
            <span>{t(($) => $.templates.columns.updated)}</span>
            <span />
          </div>

          {isLoading ? (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              {t(($) => $.templates.loading)}
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-4 py-12 text-center">
              <FileText className="mx-auto size-6 text-muted-foreground/60" />
              <p className="mt-3 text-sm font-medium">
                {query
                  ? t(($) => $.templates.no_results)
                  : t(($) => $.templates.empty)}
              </p>
            </div>
          ) : (
            <div className="divide-y divide-surface-border">
              {filtered.map((tpl) => (
                <div
                  key={tpl.id}
                  className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(10rem,1fr)_minmax(10rem,1.2fr)_6rem_7rem_2rem] md:items-center md:gap-4"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">{tpl.name}</span>
                    {tpl.is_preset && (
                      <Badge variant="secondary" className="shrink-0 gap-1 text-xs font-normal">
                        <Lock className="size-3" />
                        {t(($) => $.templates.preset_badge)}
                      </Badge>
                    )}
                  </div>
                  <p className="min-w-0 truncate text-xs text-muted-foreground md:text-sm">
                    {tpl.description || "—"}
                  </p>
                  <span className="text-xs text-muted-foreground md:text-sm">
                    {tpl.category || "—"}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {new Date(tpl.updated_at).toLocaleDateString()}
                  </span>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.templates.actions.open, { name: tpl.name })}
                        >
                          <MoreHorizontal className="size-4" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => setEditing(tpl)}>
                        <Pencil className="size-4" />
                        {t(($) => $.templates.actions.edit)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        disabled={tpl.is_preset}
                        onClick={() => setPendingDelete(tpl)}
                      >
                        <Trash2 className="size-4" />
                        {t(($) => $.templates.actions.delete)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <TemplateEditorDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
      <TemplateEditorDialog
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        template={editing}
      />
      <DeleteTemplateDialog
        template={pendingDelete}
        onClose={() => setPendingDelete(null)}
      />
    </SettingsTab>
  );
}

function TemplateEditorDialog({
  open,
  onOpenChange,
  template,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  template?: IssueTemplate | null;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const create = useCreateIssueTemplate();
  const update = useUpdateIssueTemplate();
  const [draft, setDraft] = useState<TemplateDraft>(EMPTY_DRAFT);

  const { data: labels = [] } = useQuery(labelListOptions(wsId, "issue"));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  useEffect(() => {
    if (!open) return;
    setDraft(template ? templateToDraft(template) : EMPTY_DRAFT);
  }, [template, open]);

  const assigneeOptions = useMemo(() => {
    if (draft.assignee_type === "member") {
      return members.map((m) => ({ value: m.user_id, label: m.name }));
    }
    if (draft.assignee_type === "agent") {
      return agents.map((a) => ({ value: a.id, label: a.name }));
    }
    if (draft.assignee_type === "squad") {
      return squads.map((s) => ({ value: s.id, label: s.name }));
    }
    return [];
  }, [draft.assignee_type, members, agents, squads]);

  const submit = () => {
    const name = draft.name.trim();
    if (!name) return;

    const base: CreateIssueTemplateRequest = {
      name,
      description: draft.description.trim(),
      title_template: draft.title_template,
      body_template: draft.body_template,
      status: draft.status,
      priority: draft.priority,
      assignee_type: draft.assignee_type,
      assignee_id: draft.assignee_id,
      project_id: draft.project_id,
      stage: draft.stage,
      label_ids: draft.label_ids,
      icon: draft.icon.trim(),
      category: draft.category.trim(),
    };

    if (template) {
      const updates: UpdateIssueTemplateRequest = { ...base };
      update.mutate(
        { id: template.id, ...updates },
        {
          onSuccess: () => onOpenChange(false),
          onError: (error) =>
            toast.error(error instanceof Error ? error.message : t(($) => $.templates.save_failed)),
        },
      );
      return;
    }
    create.mutate(base, {
      onSuccess: () => onOpenChange(false),
      onError: (error) =>
        toast.error(error instanceof Error ? error.message : t(($) => $.templates.save_failed)),
    });
  };

  const busy = create.isPending || update.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {template ? t(($) => $.templates.editor.edit_title) : t(($) => $.templates.editor.create_title)}
          </DialogTitle>
          <DialogDescription>
            {template?.is_preset
              ? t(($) => $.templates.editor.preset_hint)
              : t(($) => $.templates.editor.description_hint)}
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[60vh] space-y-4 overflow-y-auto py-2 pr-1">
          <div className="space-y-2">
            <FieldLabel htmlFor="tpl-name">{t(($) => $.templates.editor.name)}</FieldLabel>
            <Input
              id="tpl-name"
              autoFocus
              maxLength={64}
              value={draft.name}
              onChange={(e) => setDraft((c) => ({ ...c, name: e.target.value }))}
              placeholder={t(($) => $.templates.editor.name_placeholder)}
            />
          </div>

          <div className="space-y-2">
            <FieldLabel htmlFor="tpl-desc">{t(($) => $.templates.editor.description)}</FieldLabel>
            <Input
              id="tpl-desc"
              maxLength={500}
              value={draft.description}
              onChange={(e) => setDraft((c) => ({ ...c, description: e.target.value }))}
              placeholder={t(($) => $.templates.editor.description_placeholder)}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <FieldLabel htmlFor="tpl-title">{t(($) => $.templates.editor.title_template)}</FieldLabel>
              <Input
                id="tpl-title"
                value={draft.title_template}
                onChange={(e) => setDraft((c) => ({ ...c, title_template: e.target.value }))}
                placeholder={t(($) => $.templates.editor.title_placeholder)}
              />
            </div>
            <div className="space-y-2">
              <FieldLabel htmlFor="tpl-icon">{t(($) => $.templates.editor.icon)}</FieldLabel>
              <Input
                id="tpl-icon"
                value={draft.icon}
                onChange={(e) => setDraft((c) => ({ ...c, icon: e.target.value }))}
                placeholder={t(($) => $.templates.editor.icon_placeholder)}
              />
            </div>
          </div>

          <div className="space-y-2">
            <FieldLabel htmlFor="tpl-body">{t(($) => $.templates.editor.body_template)}</FieldLabel>
            <Textarea
              id="tpl-body"
              rows={6}
              value={draft.body_template}
              onChange={(e) => setDraft((c) => ({ ...c, body_template: e.target.value }))}
              placeholder={t(($) => $.templates.editor.body_placeholder)}
              className="font-mono text-xs"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.templates.editor.status)}</FieldLabel>
              <Select
                items={STATUS_ITEMS}
                value={draft.status}
                onValueChange={(v) => v && setDraft((c) => ({ ...c, status: v as IssueStatus }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_ITEMS.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.templates.editor.priority)}</FieldLabel>
              <Select
                items={PRIORITY_ITEMS}
                value={draft.priority}
                onValueChange={(v) => v && setDraft((c) => ({ ...c, priority: v as IssuePriority }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRIORITY_ITEMS.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <FieldLabel htmlFor="tpl-stage">{t(($) => $.templates.editor.stage)}</FieldLabel>
              <Input
                id="tpl-stage"
                type="number"
                min={1}
                value={draft.stage ?? ""}
                onChange={(e) => {
                  const v = e.target.value;
                  setDraft((c) => ({ ...c, stage: v === "" ? null : Math.max(1, Number(v)) }));
                }}
                placeholder={t(($) => $.templates.editor.stage_placeholder)}
              />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.templates.editor.assignee_type)}</FieldLabel>
              <Select
                items={ASSIGNEE_TYPE_ITEMS}
                value={draft.assignee_type ?? undefined}
                onValueChange={(v) =>
                  setDraft((c) => ({
                    ...c,
                    assignee_type: (v as IssueAssigneeType) ?? null,
                    assignee_id: null,
                  }))
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t(($) => $.templates.editor.unassigned)} />
                </SelectTrigger>
                <SelectContent>
                  {ASSIGNEE_TYPE_ITEMS.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.templates.editor.assignee)}</FieldLabel>
              <Select
                items={assigneeOptions}
                value={draft.assignee_id ?? undefined}
                disabled={!draft.assignee_type}
                onValueChange={(v) => setDraft((c) => ({ ...c, assignee_id: v ?? null }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t(($) => $.templates.editor.select_assignee)} />
                </SelectTrigger>
                <SelectContent>
                  {assigneeOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.templates.editor.project)}</FieldLabel>
              <Select
                items={projects.map((p) => ({ value: p.id, label: p.title }))}
                value={draft.project_id ?? undefined}
                onValueChange={(v) => setDraft((c) => ({ ...c, project_id: v ?? null }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t(($) => $.templates.editor.no_project)} />
                </SelectTrigger>
                <SelectContent>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <FieldLabel htmlFor="tpl-category">{t(($) => $.templates.editor.category)}</FieldLabel>
              <Input
                id="tpl-category"
                value={draft.category}
                onChange={(e) => setDraft((c) => ({ ...c, category: e.target.value }))}
                placeholder={t(($) => $.templates.editor.category_placeholder)}
              />
            </div>
          </div>

          <div className="space-y-2">
            <FieldLabel>{t(($) => $.templates.editor.labels)}</FieldLabel>
            <Popover>
              <PopoverTrigger
                render={
                  <Button variant="outline" className="w-full justify-start gap-2" type="button">
                    <Copy className="size-3.5 text-muted-foreground" />
                    {draft.label_ids.length > 0
                      ? t(($) => $.templates.editor.labels_count, { count: draft.label_ids.length })
                      : t(($) => $.templates.editor.no_labels)}
                  </Button>
                }
              />
              <PopoverContent className="w-72 p-2" align="start">
                <div className="max-h-48 overflow-y-auto space-y-1">
                  {labels.length === 0 ? (
                    <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                      {t(($) => $.templates.editor.no_labels_available)}
                    </p>
                  ) : (
                    labels.map((label) => {
                      const checked = draft.label_ids.includes(label.id);
                      return (
                        <label
                          key={label.id}
                          className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent"
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={(v) =>
                              setDraft((c) => ({
                                ...c,
                                label_ids: v
                                  ? [...c.label_ids, label.id]
                                  : c.label_ids.filter((id) => id !== label.id),
                              }))
                            }
                          />
                          <span
                            className="size-2.5 shrink-0 rounded-full"
                            style={{ backgroundColor: label.color }}
                          />
                          <span className="truncate">{label.name}</span>
                        </label>
                      );
                    })
                  )}
                </div>
              </PopoverContent>
            </Popover>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.templates.editor.cancel)}
          </Button>
          <Button onClick={submit} disabled={!draft.name.trim() || busy}>
            {busy ? t(($) => $.templates.editor.saving) : t(($) => $.templates.editor.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteTemplateDialog({
  template,
  onClose,
}: {
  template: IssueTemplate | null;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const remove = useDeleteIssueTemplate();
  return (
    <AlertDialog open={Boolean(template)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.templates.delete_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.templates.delete_dialog.description, { name: template?.name ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.templates.delete_dialog.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (!template) return;
              remove.mutate(template.id, {
                onSuccess: onClose,
                onError: (error) =>
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : t(($) => $.templates.delete_dialog.failed),
                  ),
              });
            }}
          >
            {t(($) => $.templates.delete_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
