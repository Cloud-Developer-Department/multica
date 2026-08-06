# Bug 修复报告（CLO-172）

**日期**: 2026-08-04
**角色**: Coding（库里）

## 一、问题现象

用户本地部署后报告两个症状：

1. **issue 展示不全** — Settings → Templates 页签未显示预置模板
2. **无法使用模板创建** — 创建 Issue 对话框中没有「模板」下拉入口

## 二、根因定位

### 直击现场

用 curl 直接请求后端 API：

```bash
curl -s -H "X-Workspace-ID: ..." http://localhost:8080/api/issue-templates
```

返回的 4 个预置模板中，每个模板的 `label_ids` 字段值为 `null`：

```json
{"issue_templates":[{"name":"Bug 修复","label_ids":null,...}]}
```

### 前端 schema 校验失败

`packages/core/api/schemas.ts` 中的 `IssueTemplateSchema`：

```ts
label_ids: z.array(z.string()).optional().default([])
```

zod v4 行为：
- `undefined` → `[]`（default 生效）
- `null` → **校验失败**（`expected array, received null`）

`parseWithFallback` 在 schema 校验失败时返回 `EMPTY_LIST_ISSUE_TEMPLATES_RESPONSE`（空列表），导致前端拿到 0 个模板。

### 后端根因

`server/internal/handler/issue_template.go` 的 `parseLabelIDsJSONB`：

```go
func parseLabelIDsJSONB(raw []byte) []string {
    if len(raw) == 0 {
        return nil  // ← JSON 序列化为 null
    }
    ...
    if len(ids) == 0 {
        return nil  // ← JSON 序列化为 null
    }
    return ids
}
```

Go 中 `nil` 切片序列化为 `null`，`[]string{}` 序列化为 `[]`。预置模板播种时 `label_ids` 为空数组，经此函数处理后返回 `nil` → JSON 输出 `null`。

### 影响链

```
parseLabelIDsJSONB 返回 nil
  → JSON "label_ids": null
    → zod schema 校验失败（expected array, received null）
      → parseWithFallback 返回空列表
        → Settings 模板页显示「暂无模板」
        → 创建 Issue 对话框 issueTemplates.length === 0，模板下拉不渲染
```

## 三、修复方案

### 3.1 后端（根因修复）

`parseLabelIDsJSONB` 对所有空/无效路径返回 `[]string{}` 而非 `nil`，确保 JSON 输出 `[]`：

```go
func parseLabelIDsJSONB(raw []byte) []string {
    if len(raw) == 0 {
        return []string{}
    }
    var ids []string
    if err := json.Unmarshal(raw, &ids); err != nil {
        return []string{}
    }
    if len(ids) == 0 {
        return []string{}
    }
    return ids
}
```

### 3.2 前端（防御性兼容）

Schema 改为接受 `null` 并归一化为 `[]`，兼容旧服务端响应：

```ts
label_ids: z.array(z.string()).nullish().transform((v) => v ?? [])
```

zod v4 行为验证：
- `null` → `[]`
- `undefined` → `[]`
- `[]` → `[]`
- `["a","b"]` → `["a","b"]`

## 四、改动文件

| 文件 | 改动 |
|------|------|
| `server/internal/handler/issue_template.go` | `parseLabelIDsJSONB` 返回 `[]string{}` 而非 `nil` |
| `packages/core/api/schemas.ts` | `IssueTemplateSchema.label_ids` 改为 `.nullish().transform(v => v ?? [])` |

## 五、影响范围

- 仅影响 Issue 模板的列表/详情 API 响应序列化与前端解析
- 不影响数据库 schema、其他 API、其他实体
- 不影响模板创建/更新逻辑（写入路径 `marshalLabelIDsJSONB` 未变）

## 六、兼容性

- 后端：新版本返回 `label_ids: []`，旧前端 schema 能正确解析 `[]`
- 前端：新 schema 兼容 `null`（旧后端）和 `[]`（新后端），双向兼容
- 数据库：无 schema 变更，无需迁移

## 七、风险

- 🟢 低风险 — 改动范围极小（2 个文件，7 行变更），仅影响空数组的序列化表示
- 🟢 不影响既有数据 — 已存储的模板 `label_ids` JSONB 值不变，只是响应序列化方式从 `null` 变为 `[]`
