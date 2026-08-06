# Board 看板树状结构 — 文档交付（CLO-225）

**Issue**: CLO-225（父 CLO-218）
**日期**: 2026-08-06
**角色**: Documentation（杜兰特 / sgd-durant）
**依据**: Review（CLO-224）approve with suggestions + Validation（CLO-223）最终 tip 重跑通过（B1 闭环）；代码 `feature/board-tree` @ `a72746d7`（实现 `23c6ed5c`，后续 W2 组件测试由 Coding 并行补回）

## 本目录内容

| 文件 | 读者 | 说明 |
|------|------|------|
| `board-tree-interaction.md` | 用户 / 产品 | 看板视图树状交互说明：展开/折叠、拖拽语义、子任务跟随父列、进度环、展示开关 |
| `board-tree-api.md` | 开发 / 对接方 | API 契约说明：零后端改动、复用端点、前端 view-store 状态与持久化、拖拽落库序列 |
| `release-notes-2026-08-05.md` | 用户 / 团队 | Release Note：新功能、交互变化、已知限制 |
| `CHANGELOG.md` | 团队 | ChangeLog 条目（可并入主仓库 changelog） |

## 与既有交付件的关系

- 设计契约：`../02-design/board-tree-design.md`（D1–D6 决策、R1–R7 风险）
- 代码：`../03-code/`（`feature/board-tree` 前端实现，`board-tree-model.ts` / `drag-utils.ts` / `board-view.tsx` 等）
- 测试：`../04-tests/`（最终 tip 重跑，B1 闭环）
- 审查：`../05-review/board-tree-review.md`（approve with suggestions；W2 组件测试待 CLO-264 补回）

## 关键结论（一句话）

**看板列内父子 issue 以树形展示：父卡可展开/折叠、子卡缩进；拖拽统一为「子树移动」（拖父整组、拖叶子子单独、拖子跨列转顶层）；子项跟随父列渲染；全部为前端实现，后端零改动，Table/List/Swimlane 不受影响。**
