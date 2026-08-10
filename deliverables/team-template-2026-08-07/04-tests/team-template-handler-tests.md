# Task C：team-template handler 单测报告

**日期**: 2026-08-07
**Issue**: SGD-25
**执行**: `cd server && go test ./internal/handler -run TeamTemplate -count=1 -v`

## 测试覆盖矩阵（api_design.md §8 契约对齐）

| 测试 | 覆盖 | 状态 |
|------|------|------|
| `TestListTeamTemplatesEmptyIsEmptyArray` | 空目录 → 200 `[]`（数组非 null） | PASS |
| `TestListTeamTemplatesReturnsSummaries` | 摘要字段：省略 content/instructions、squad member_count=leader+members、skills 数组 present | PASS |
| `TestGetTeamTemplateReturnsDetail` | 全文：skill content / agent instructions+visibility / squad instructions，数组非 null | PASS |
| `TestGetTeamTemplateUnknownSlug404` | slug 不存在 → 404 | PASS |
| `TestApplyTeamTemplateFirstApplyCreatesAll` | 首次 apply → 201 全 created（skills 1 / agents 2 / squads 1），reused 空数组非 null；DB 落库校验（skill content + squad leader 成员） | PASS |
| `TestApplyTeamTemplateRepeatApplyReusesAll` | 重复 apply → 201 全 reused，created=[]；reused id 与首次 created id 一致（幂等） | PASS |
| `TestApplyTeamTemplateRemoteSkillBranch` | source_url skill → 预 fetch 后建，内容 = 抓取正文 | PASS |
| `TestApplyTeamTemplateBadSourceURLReturns422WithNoResidue` | 坏 source_url → 422 + failed_urls；skill/agent/squad 零残留（未进事务） | PASS |
| `TestApplyTeamTemplateTxFailureRollsBackEverything` | 注入 mock 事务失败（squad INSERT 抛错）→ 500；三资源全量回滚零残留 | PASS |
| `TestApplyTeamTemplateModelOverrides` | model_overrides 覆盖对应 agent，其余用模板默认 | PASS |
| `TestApplyTeamTemplateValidationErrors` | 404 slug / 400 缺 runtime_id / 400 未知 override agent / 400 非法 runtime_id / 400 坏 body | PASS |
| `TestApplyTeamTemplateDefaultMaxConcurrentTasks` | 模板未指定 agent → 缺省 6 | PASS |

## 结果

```
ok  github.com/multica-ai/multica/server/internal/handler  0.253s
```

共 12 个测试函数全绿；`go build ./internal/handler` / `go vet` / `gofmt` 均通过。
