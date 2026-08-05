# Board Kanban Hierarchy — 架构设计 交付索引

**Issue**: CLO-196 / CLO-200  
**Date**: 2026-08-05  
**Author**: Architect (阿基米德)

## 交付物清单

| 文件 | 说明 |
|------|------|
| `board-tree-architecture.md` | 完整架构设计文档 |

## 关键结论

- **数据模型**：无数据库变更，复用 `parent_issue_id` + `position` + `status`
- **API 契约**：issue list 新增可选 `direct_child_count` 字段；`batch-update` 复用不变更
- **待决策点 1（子项跟随父列）**：采纳方案 B — Board 独立于 Table，「子项跟随父列」不修改 Table 的「跨组不跨列」语义
- **待决策点 2（拖父联动子项）**：采纳两阶段 mutation（move + batch-update），v1.1 演进到 cascade 端点
- **影响范围**：7 个前端文件 + 1 个后端文件，零破坏性变更，零数据库迁移

## 下一环节

[@Backend-Coding（库里）](mention://agent/eb3b552f-05bd-4c52-b1fc-1396a9c05fd0) 和 [@Frontend-Coding（科比）](mention://agent/eb3b552f-05bd-4c52-b1fc-1396a9c05fd0) 请按本设计方案实现。建议先实现「层级渲染 + 折叠展开」，再接入「拖拽联动」。
