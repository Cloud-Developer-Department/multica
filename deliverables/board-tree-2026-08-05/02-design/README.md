# 02-Design 架构设计 交付索引（本轮 CLO-221）

**Issue**: CLO-221 【设计】看板树状结构方案设计（展开/折叠 + 拖拽语义）
**日期**: 2026-08-05
**角色**: Architect（阿基米德）
**父 Issue**: CLO-218 看板视图支持父子 issue 树状结构

## 交付物清单

| 文件 | 说明 | 状态 |
|------|------|------|
| `board-tree-design.md` | **本轮**完整架构设计方案（数据层/跟随父列/拖拽语义/影响范围/风险） | 权威，以本文件为准 |
| `board-tree-architecture.md` | 上一轮（CLO-196）架构设计，已被本轮方案取代，仅作历史参考 | 归档 |

## 本轮关键裁定（速览）

1. **数据层（D1）**：前端组树，复用 Board 现有平铺查询 + `childrenByParentsOptions` 惰性补拉；不引入 `/table/rows` hierarchy 分支重构（管线无后端阶段，且其子分支为「组内」语义）。`childProgressMap` 驱动进度环与「有子」箭头（D6）。
2. **子项跟随父列（D2）**：采纳方案 B 的纯前端等价实现 —— 子渲染位置由父列决定，无论子自身 status/assignee/property；父不可见时子按自身值落列。不改后端，Table/List/Swimlane 零影响。
3. **拖拽语义（D3/D4）**：统一「子树移动」规则（拖任意卡 = 移动该卡及全部后代），叶子子卡即「单独移动」；拖子跨列 = `parent_issue_id` 置 null 转顶层任务。
4. **折叠状态（D5）**：`boardCollapsedParents` 新增持久化，独立于 Table，复用其状态管理模式。

## 涉及范围

- 前端：`board-view.tsx` / `board-column.tsx` / `board-card.tsx` / `drag-utils.ts` / `view-store.ts` + 新增 `board-tree-model.ts`
- 后端：零改动（无 DB / handler / 测试变更）

## 下一环节

交给 Frontend-Coding（科比）按 `board-tree-design.md` 实现（CLO-222）。建议实现顺序：树形渲染 + 折叠展开 → 拖拽子树移动 → 跟随父列 + 拖子脱离。
