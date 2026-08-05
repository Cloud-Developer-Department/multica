# 测试与验证报告（CLO-187）— 模板正文带出修复

**日期**: 2026-08-05
**角色**: Coding（库里）

## 一、回归测试

### 新增用例：`pre-fills the title and body when a template is applied`

位置：`packages/views/modals/create-issue.test.tsx`

验证点：

1. 模板列表 mock 返回含 `body_template` 的模板（`## 问题现象\n\n复现步骤……`）。
2. 打开「Template」下拉并选择模板后，正文编辑器文本等于 `body_template`。
3. 继续输入标题后点击「Create Issue」，创建 payload 中的 `description` 等于 `body_template`、`title` 拼接了 `title_template` 前缀、`priority` 带出模板优先级。

该用例只有在前端对 `ContentEditor` 重挂载（`key={formResetKey}`）时才能通过——`ContentEditor` 的 `defaultValue` 是 mount-only。

### 测试有效性确认（修复前失败）

临时移除 `ContentEditor` 的 `key={formResetKey}` 后运行该用例：

```
❯ packages/views/modals/create-issue.test.tsx (37 tests | 1 failed | 36 skipped)
```

正文编辑器未带出模板内容，用例失败；恢复修复后用例通过。证明该用例能有效拦截本 Bug 回归。

## 二、验证结果

| 项目 | 命令 | 结果 |
|------|------|------|
| 回归测试（新用例） | `pnpm -C packages/views test --run modals/create-issue.test.tsx -t "pre-fills the title and body"` | 通过 |
| create-issue 全量用例 | `pnpm -C packages/views test --run modals/create-issue.test.tsx` | 37/37 通过 |
| quick-create-issue 用例 | `pnpm -C packages/views test --run modals/quick-create-issue.test.tsx` | 17/17 通过 |
| TypeScript 类型检查 | `pnpm -C packages/views typecheck` | 通过 |
| ESLint（改动文件） | `pnpm -C packages/views lint` | 改动文件无新增问题（仓库既有错误位于其他未改动文件） |

## 三、建议测试点（Validation 角色）

1. **手动验证**：创建 Issue 对话框 → 选择带 `body_template` 的模板 → 确认正文编辑器立即显示模板正文。
2. **模板字段带出完整性**：确认标题、优先级、负责人、项目、标签等其余字段带出不受影响。
3. **Create another**：创建成功后选择「Create another」，确认正文被清空（`resetForNextIssue` 的 key 递增路径仍正常）。
4. **切换模板**：先后选择两个不同正文的模板，确认正文跟随切换。
