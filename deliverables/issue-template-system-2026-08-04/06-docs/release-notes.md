# Release Notes — Issue 模板系统

**特性**: Issue 模板系统（CLO-159）
**目标版本**: 0.5.0（规划）
**日期**: 2026-08-04
**角色**: Documentation（杜兰特）

---

## 概述

Multica 新增 **Issue 模板系统**：支持创建工作区级 Issue 模板（标题、描述、优先级、负责人、项目、阶段、标签等字段预设），创建 Issue 时可一键套用模板预填表单；预置 4 个常用模板开箱即用；Settings 提供模板管理入口。

## 新增功能

1. **Issue 模板 CRUD API** — `/api/issue-templates` 全套 REST 端点（List/Create/Get/Update/Delete），workspace-scoped，任何成员可读、owner/admin 可写。
2. **4 个预置模板** — 特性开发、Bug 修复、需求分析、周报/月报，在新建工作区时自动播种；可编辑、受保护不可删除。
3. **Settings → Templates 页签** — 模板列表 / 搜索 / 新建 / 编辑 / 删除确认 / 预置模板 Lock 标记。
4. **创建 Issue 对话框模板选择器** — 手动创建面板头部下拉选择模板，一键预填标题、描述、状态、优先级、负责人、项目、阶段、标签。
5. **WebSocket 实时同步** — `issue_template:created/updated/deleted` 三事件，多端模板数据实时刷新。

## 数据与迁移

- 新增数据库表 `issue_template`（迁移 232）+ workspace 索引（迁移 233）。
- 遵循仓库迁移规范：无外键、`CREATE INDEX CONCURRENTLY` 独立文件、up/down 可逆。
- 无配置项、无环境变量变更。

## 兼容性

- **向后兼容**：既有 API 无破坏性改动，仅新增路由。
- **既有工作区**：不受影响，但不会自动获得预置模板（仅新建工作区播种）。

## 已知限制（本版本）

1. 既有工作区无预置模板（需后续 seeding 端点/脚本，P1）。
2. `project_id` 未在模板创建/更新时校验 workspace 归属（P2，创建 Issue 时有二次校验兜底）。
3. 后端未提供 `template_id` 直接创建 Issue 参数（前端客户端侧应用模板）。
4. 模板选择器仅在手动创建面板提供（Agent 快速创建面板未集成）。
5. 本次未提交自动化测试（建议后续补充 handler 单测 + 前端 UI 测试）。

## 验证摘要

- 后端 `go build` / `go vet` / `gofmt` 通过
- 前端 `pnpm typecheck`（6/6）/ `pnpm lint` 通过
- API 集成测试 **35/35** 通过；WS 三事件通过；迁移 232/233 up/down 可逆
- 前端测试：core 1066、web 151、views 3053/3057（4 个失败为既有 `share_link` i18n parity，非本次引入）

## 文档清单

| 文档 | 路径 |
|------|------|
| 功能说明 | `06-docs/feature-guide.md` |
| API 参考 | `06-docs/api.md` |
| FAQ | `06-docs/faq.md` |
| 本发布说明 | `06-docs/release-notes.md` |
