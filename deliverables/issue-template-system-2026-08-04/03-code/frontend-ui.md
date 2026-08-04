# 前端 Issue 模板管理界面实现说明 (CLO-164)

## 概述

为 Issue 模板系统 (CLO-159) 实现完整的前端界面，包括 Settings 模板管理 Tab 和创建 Issue 对话框中的模板选择器。后端 CRUD API 已在 CLO-161 中完成。

## API 路由

| 方法 | 路由 | 用途 |
|2------|------|------|
| GET | `/api/issue-templates` | 列出工作区所有模板 |
| POST | `/api/issue-templates` | 创建模板 |
| GET | `/api/issue-templates/{id}` | 获取单个模板 |
| PUT | `/api/issue-templates/{id}` | 更新模板 |
| DELETE | `/api/issue-templates/{id}` | 删除模板（预置模板受保护） |

## 数据模型

`IssueTemplate` 字段：id, workspace_id, name, description, title_template, body_template, status, priority, assignee_type, assignee_id, project_id, stage, label_ids[], icon, category, is_preset, created_by, created_at, updated_at

## 新增文件

| 文件 | 用途 |
|------|------|
| `packages/core/types/issue-template.ts` | 类型定义 |
| `packages/core/issue-templates/queries.ts` | React Query query keys + options |
| `packages/core/issue-templates/mutations.ts` | React Query mutations (CRUD) |
| `packages/core/issue-templates/index.ts` | re-exports |
| `packages/views/settings/components/templates-tab.tsx` | Settings 模板管理 Tab |

## 修改文件

| 文件 | 修改内容 |
|------|---------|
| `packages/core/types/index.ts` | re-export issue-template 类型 |
| `packages/core/types/events.ts` | 新增 `issue_template:created/updated/deleted` WS 事件类型 |
| `packages/core/api/schemas.ts` | 新增 `IssueTemplateSchema` + `ListIssueTemplatesResponseSchema` |
| `packages/core/api/client.ts` | 新增 5 个 API 方法 |
| `packages/core/package.json` | 注册 `./issue-templates` 子路径导出 |
| `packages/core/realtime/use-realtime-sync.ts` | 新增 `issue_template` 前缀处理器 |
| `packages/views/settings/components/settings-page.tsx` | 新增 "templates" workspace tab |
| `packages/views/settings/components/index.ts` | 导出 `TemplatesTab` |
| `packages/views/modals/create-issue.tsx` | 头部新增模板选择器下拉菜单 |
| `packages/views/locales/{en,zh-Hans,ja,ko}/settings.json` | 模板 Tab i18n |
| `packages/views/locales/{en,zh-Hans,ja,ko}/modals.json` | 模板按钮 i18n |

## 功能

### Settings → Templates Tab
- 模板列表（名称、描述、分类、更新时间）
- 搜索筛选（按名称/描述/分类）
- 新建模板按钮
- 编辑/删除操作菜单
- 预置模板标记（Lock badge，删除禁用）
- 模板编辑器 Dialog：
  - 名称、描述、图标、分类
  - 标题前缀、正文模板（Markdown）
  - 状态、优先级、阶段
  - 负责人类型 + 负责人（联动 Select）
  - 项目
  - 标签（Popover 多选 Checkbox）
- 删除确认 Dialog

### 创建 Issue 对话框模板选择器
- 头部面包屑旁新增「模板」下拉按钮
- 仅在有模板时显示
- 选择模板后预填：标题、描述、状态、优先级、负责人、项目、阶段、标签
- 仅覆盖模板实际设置了值的字段

## WS 实时同步

`issue_template:created/updated/deleted` 事件通过 `use-realtime-sync.ts` 的 `issue_template` 前缀处理器 invalidate 模板缓存，确保多端同步。

## 校验规则

- 名称：必填，最长 64 字符，无控制字符
- 描述：最长 500 字符
- 正文模板：最长 16000 字符
- 标签：最多 50 个，需为当前工作区的 issue 标签
- 负责人：需为当前工作区有效成员/智能体/小队
- 阶段：≥ 1
- 预置模板：不可删除（后端 409），可编辑内容

## 风险

1. **模板预填 vs 草稿恢复**：模板选择会覆盖当前表单状态（包括草稿），这是有意设计——用户选择模板即表示要从模板开始。草稿在模板应用后继续正常工作。
2. **预置模板编辑**：预置模板可编辑内容但不能删除，与后端保护逻辑一致。
3. **i18n parity**：4 个 locale 均已覆盖所有新增 key。预存的 `members.share_link_*` parity 失败为既有问题，与本改动无关。

## 验证

- `pnpm typecheck`：通过（6/6 packages）
- `pnpm lint`（改动文件）：无 error / warning
- `pnpm test`（views parity）：templates key 全 4 locale 对齐
