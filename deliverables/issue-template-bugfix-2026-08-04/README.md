# Issue 模板 Bug 修复交付（CLO-172）

**日期**: 2026-08-04
**角色**: Coding（库里）
**Issue**: CLO-172 【Bug修复】Issue 模板：issue 展示不全 + 无法使用模板创建
**父 Issue**: CLO-159 Issue 模板系统

## 交付物索引

| 文件 | 说明 |
|------|------|
| `03-code/bugfix-report.md` | 根因分析、修复方案、改动明细 |
| `04-tests/verification.md` | 端到端验证结果 |

## 关键结论

- **根因**：后端 `parseLabelIDsJSONB` 对空标签数组返回 `nil`，JSON 序列化为 `null`；前端 zod schema `z.array(z.string()).optional().default([])` 只对 `undefined` 应用默认值，对 `null` 校验失败 → 整个模板列表 parse 失败回退为空 → Settings 模板页显示「暂无模板」+ 创建 Issue 对话框模板下拉不渲染。
- **修复**：后端返回 `[]string{}`（序列化为 `[]`）；前端 schema 改为 `.nullish().transform(v => v ?? [])` 防御性兼容 `null`。
- **验证**：本地全链路通过，4 个预置模板正常展示，`label_ids=[]`。

## 代码变更

- `server/internal/handler/issue_template.go` — `parseLabelIDsJSONB` 返回空切片而非 nil
- `packages/core/api/schemas.ts` — `IssueTemplateSchema.label_ids` 兼容 null

## 分支

- `agent/sgd-curry-coding/bed6abb2`（基于 `agent/sgd-yaoming-devops/5f65e051-deploy`）
- 提交：`ad857419 fix(issues): issue template label_ids null → empty array (CLO-172)`
