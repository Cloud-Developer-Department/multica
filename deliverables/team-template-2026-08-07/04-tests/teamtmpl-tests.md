# Task A：teamtmpl 包测试报告

**日期**: 2026-08-07
**Issue**: SGD-23
**执行**: `cd server && go test ./internal/teamtmpl -count=1 -v`

## 测试覆盖矩阵

| 测试 | 覆盖 | 状态 |
|------|------|------|
| `TestLoad_RealTemplates` | 生产 `go:embed` 路径（boot 校验不炸） | PASS |
| `TestLoadFromFS_Valid` | 成功路径：多模板加载、`List()` 确定性排序、`Get()` 命中/未命中 | PASS |
| `TestLoadFromFS_SkipsUnderscorePlaceholders` | smoke 占位文件不进 catalog（`__` 前缀跳过） | PASS |
| `TestLoadFromFS_MalformedJSON` | **init() panic 路径**：坏 JSON → `Load()` 返回 error | PASS |
| `TestValidate_Rules` | 10 条校验规则逐条反例 | PASS |
| `TestValidate_ZeroSectionTemplates` | 0-skill/0-agent/0-squad 模板合法 | PASS |
| `TestLoadFromFS_DuplicateSlug` | 同 slug 跨文件 → duplicate slug 错误 | PASS |

`TestValidate_Rules` 反例覆盖（13 个子用例）：

- 规则1：空 slug / 坏 slug（`Bad_Slug`）/ slug 与文件名不匹配
- 规则2：name 为空
- 规则3：skill name 重复
- 规则4：agent name 重复
- 规则5：squad name 重复
- 规则6：content + source_url 同时非空 / 同时为空
- 规则7：agent.skills 悬空引用
- 规则8：squad.leader 悬空引用
- 规则9：squad.members[].agent_name 悬空引用
- 规则10：agent.instructions 为空

## 结果

```
ok  github.com/multica-ai/multica/server/internal/teamtmpl  0.008s
```

共 7 个测试函数、17 个子用例全绿；`go vet` 与 `gofmt` 均通过。
