# 03-backend — Board 树状展示后端改造（CLO-198）

**作者**: Backend-Coding（库里）
**日期**: 2026-08-05
**工作分支**: `feature/board-tree`

## 改动文件

| 文件 | 变更 |
|------|------|
| `server/internal/handler/issue.go` | `IssueResponse` 新增可选 `direct_child_count`；`ListIssues` 支持 `?hierarchy=true`；新增 `directChildCountsByIssue` 批量子节点计数 |
| `server/internal/handler/issue_hierarchy_test.go` | 新增：`?hierarchy` 参数行为测试（缺省缺席 / 计数正确 / 仅统计直接子） |

## API 契约定稿

### `GET /api/issues` — 新增 `hierarchy` 查询参数

```
Query:    ?workspace_id=<uuid>&...&hierarchy=true
Response: [{ ...issueFields..., direct_child_count?: number }, ...]
```

- `hierarchy=true` 时，响应中**每一行**都带 `direct_child_count`（int64）：
  - `> 0`：该 issue 有 N 个直接子节点（渲染折叠切换 + 计数徽章）
  - `= 0`：无子节点（不渲染折叠按钮/徽章）
- 不传 `hierarchy`（默认）：字段**整体缺席**（`*int64` + `omitempty`），现有消费方零感知。
- 计数语义：**一级直接子、workspace 全量统计**，不受当前列表筛选（状态/分配人/属性等）影响 —— Board 父卡片始终展示完整子任务树。子查询按 `parent_issue_id` 分组（`idx_issue_parent` 已存在，无新增索引）。

实现为一次批量查询（`WHERE parent_issue_id = ANY($1::uuid[]) GROUP BY parent_issue_id`），非逐卡片 N+1，且失败时降级为空 map 而不是整请求 500（与 `labelsByIssue` 同模式）。

### 复用端点（零改动）

| 端点 | 用途 |
|------|------|
| `POST /api/issues/:id/move` | 拖父卡片（同列排序 / 跨列含 status 变更 + 锚点定位） |
| `POST /api/issues/batch-update` | 拖父跨列后，所有直接子卡片批量同步 status |
| `GET /api/issues/child-progress` | 父卡片子任务进度环（done/total） |

`POST /api/issues/query`（QueryIssues）通过重建 query string 委托给 ListIssues，`hierarchy` 参数自动透传，无需额外改动。

## 影响范围

- **后端**：仅 `issue.go` 一个业务文件（+1 测试文件）。
- **零数据库迁移**：无 schema / 索引 / 外键变更（`parent_issue_id`、`idx_issue_parent` 均已存在）。
- **零破坏性变更**：`direct_child_count` 为可选新增字段，缺省缺席，向后兼容。
- **性能**：仅在 `?hierarchy=true` 时多一次批量计数查询，命中 `idx_issue_parent` 索引；默认路径无任何开销。

## 测试结果

```
go test ./internal/handler/ -count=1     # 全量 handler 测试包通过（含新增用例）
go vet ./internal/handler/
go build ./...
```

`TestListIssues_HierarchyDirectChildCount` 覆盖：
1. 缺省（无 `hierarchy`）时 `direct_child_count` 缺席（向后兼容）；
2. `hierarchy=true` 时每行都有计数：父=2、有子代的子=1、叶子=0；
3. 孙节点不归到祖父（仅统计直接子）。

## 前后端契约对齐

Board 前端经 `api.listIssues({ ..., hierarchy: true })` 消费本字段；同列/跨列拖拽复用 `move` + `batch-update`，与 Architect 方案 §4 一致。字段名 `direct_child_count`、缺省缺席语义以本文为准。
