# Issue 模板正文带出 Bug 修复交付（CLO-187）

**日期**: 2026-08-05
**角色**: Coding（库里）
**Issue**: CLO-187 【Bug修复】模板正文内容在新建 issue 时未带出
**父 Issue**: CLO-159 Issue 模板系统

## 交付物索引

| 文件 | 说明 |
|------|------|
| `03-code/bugfix-report.md` | 根因分析、修复方案、改动明细 |
| `04-tests/verification.md` | 回归测试与验证结果 |
| `07-deployment/redeploy-report.md` | 复部署报告（CLO-191，DevOps）—— PR #4 合入后本地复部署与端到端验证 |

## 关键结论

- **根因**：`applyTemplate` 选择模板正文后调用 `setFormResetKey` 以重挂载正文编辑器，但只有标题编辑器（`TitleEditor`）挂了 `key={formResetKey}`，正文编辑器（`ContentEditor`）没有 key。`ContentEditor` 将 `defaultValue` 视为仅挂载时读取，因此模板 `body_template` 永远无法显示到编辑器中。
- **修复**：为 `ContentEditor` 补上 `key={formResetKey}`，与 `TitleEditor` 一致，模板正文在重挂载后以 `defaultValue` 形式带出。
- **验证**：新增回归测试「pre-fills the title and body when a template is applied」，修复前失败、修复后通过；create-issue 套件 37 个用例全过，typecheck 通过。

## 代码变更

- `packages/views/modals/create-issue.tsx` — `ContentEditor` 增加 `key={formResetKey}`（1 行）
- `packages/views/modals/create-issue.test.tsx` — 新增模板带出回归测试（含 issueTemplateListOptions mock）

## 分支

- 分支：`agent/sgd-curry-coding/d03c06cd`（基于 `feature/issue-template`）
