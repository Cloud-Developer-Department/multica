# Issue 模板系统交付索引

**特性**: Issue 模板系统（CLO-159）
**日期**: 2026-08-04

## 交付物

| 阶段 | 路径 | 角色 | 状态 |
|------|------|------|------|
| 调研 | `01-research/` | Research（魔术师） | ✅ 完成（CLO-160） |
| 后端代码 | `03-code/` | Coding（库里） | ✅ 完成（CLO-161） |
| 前端 | — | Coding（库里） | ⏳ 待执行（CLO-164） |
| 测试 | — | Validation（邓肯） | ⏳ 待执行（CLO-165） |
| 审查 | — | Review（保罗） | ⏳ 待执行（CLO-162） |
| 文档 | — | Documentation（杜兰特） | ⏳ 待执行（CLO-163） |

## 后端交付（CLO-161）

- `03-code/implementation.md` — 实现说明、API 路由、数据模型、校验规则、风险
- `03-code/backend-crud.diff` — 修改文件 diff
- `03-code/new-files.txt` — 新增文件清单

## 关键结论

- 后端 Issue 模板 CRUD API 已实现：`/api/issue-templates` 全套 REST 端点
- 数据库 `issue_template` 表 + 索引迁移（232/233），遵循无外键 + CONCURRENTLY 规范
- 4 个预置模板在 CreateWorkspace 事务中自动播种
- 预置模板受保护不可删除，但可编辑
- 编译 + go vet 通过
