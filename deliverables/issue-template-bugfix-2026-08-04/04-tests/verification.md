# 端到端验证报告（CLO-172）

**日期**: 2026-08-04
**角色**: Coding（库里）

## 一、验证环境

| 项 | 值 |
|----|----|
| 后端 | 修复后重新编译 `bin/server`，:8080 |
| 前端 | 修复后重新 `next build` + `next start`，:3000 |
| 数据库 | Docker pgvector/pg17，:5432 |
| 验证账号 | final-test@test.com（dev 验证码 888888） |

## 二、验证结果

| # | 场景 | 期望 | 结果 |
|---|------|------|------|
| 1 | 后端 `/health` | `{"status":"ok"}` | ✅ |
| 2 | 前端 `GET /` | 200 | ✅ |
| 3 | 登录（dev 验证码） | token 返回 | ✅ |
| 4 | 创建工作区 | 200，自动播种 4 模板 | ✅ |
| 5 | `GET /api/issue-templates`（后端直连） | total=4，label_ids=[] | ✅ |
| 6 | `GET /api/issue-templates`（前端代理） | total=4，label_ids=[] | ✅ |
| 7 | 4 个预置模板名称 | 特性开发/Bug 修复/需求分析/周报月报 | ✅ |
| 8 | label_ids 类型 | list（非 null） | ✅ |

## 三、API 响应样本

```json
{
  "issue_templates": [
    {"name": "Bug 修复", "label_ids": [], ...},
    {"name": "周报/月报", "label_ids": [], ...},
    {"name": "特性开发", "label_ids": [], ...},
    {"name": "需求分析", "label_ids": [], ...}
  ],
  "total": 4
}
```

## 四、前端 schema 验证

zod v4 `IssueTemplateSchema` 对 `label_ids` 各输入的解析结果：

| 输入 | 结果 |
|------|------|
| `null` | `[]` ✅ |
| `undefined` | `[]` ✅ |
| `[]` | `[]` ✅ |
| `["a","b"]` | `["a","b"]` ✅ |

## 五、构建与静态检查

| 检查项 | 结果 |
|--------|------|
| `go build ./cmd/server` | ✅ |
| `go vet ./internal/handler/...` | ✅ |
| `pnpm typecheck`（全 6 包） | ✅ 6/6 通过 |
| `pnpm --filter @multica/core test` | ✅ 101 文件 / 1066 测试全过 |
| `pnpm --filter @multica/web build` | ✅ 产出 BUILD_ID |

## 六、结论

两个用户报告的症状均已修复：
1. **issue 展示不全** → Settings → Templates 页签现在正常显示 4 个预置模板
2. **无法使用模板创建** → 创建 Issue 对话框模板下拉现在会渲染（`issueTemplates.length > 0`）
