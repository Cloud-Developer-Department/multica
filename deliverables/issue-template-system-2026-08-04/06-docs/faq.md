# Issue 模板系统 — FAQ / 故障排查

**特性**: Issue 模板系统（CLO-159）
**日期**: 2026-08-04
**角色**: Documentation（杜兰特）

---

## 功能类

### Q1. 为什么我的工作区没有预置模板？

预置模板只在 **CreateWorkspace 事务内**对**新建**工作区播种。**已存在的工作区不会自动获得预置模板**，这是本版本的已知限制（审查/验证标记 P1）。需要人工在 Settings → Templates 手动创建，或等待后续提供的 seeding 端点/脚本。

### Q2. 预置模板可以删除吗？

不可以。删除预置模板返回 HTTP 409。但内容（标题、正文、优先级等）可以编辑。

### Q3. 我为什么看不到「模板」下拉按钮？

创建 Issue 对话框的模板选择器**仅当当前工作区存在模板时显示**。如果工作区一个模板都没有（例如既有工作区），按钮不会出现。

### Q4. 模板选择器为什么在 Agent 快速创建模式不可用？

模板选择器只在「手动创建」面板（ManualCreatePanel）提供。Agent 快速创建面板只有 prompt 输入，没有字段预填语义，因此未集成。这是已知限制。

### Q5. 选模板会丢我的草稿吗？

选择模板会**覆盖模板实际设置了的字段**，包括草稿中对应的内容；模板未设置的字段保持不变。这是有意设计——选择模板即表示「从模板开始」。如果你正在编辑重要内容，建议先提交或备份再切模板。

### Q6. 模板可以跨工作区使用吗？

不可以。模板按工作区隔离，访问其他工作区的模板返回 404。

---

## API 类

### Q7. 为什么删除模板返回 409？

该模板是预置模板（`is_preset=true`），受保护不可删除。

### Q8. PUT 更新时字段什么时候被清空？

- `assignee_type` / `assignee_id` / `project_id` / `stage` 传 `null` → **清空**
- 其余字段（name/description/title/body/status/priority/label_ids/icon/category）**省略 → 保留原值**，传 `null` 不被接受（用省略或直接传值）

### Q9. 创建模板报「label not found in this workspace」？

`label_ids` 里的每个 ID 都必须是**当前工作区**的 **issue 类型**标签。检查：标签 ID 是否正确、该标签是否属于当前工作区、是否 issue 类型。

### Q10. 创建模板报 400 校验错误，常见原因？

- `name` 空 / 含控制字符 / 超过 64 字符
- `description` 超过 500 字符 / `body_template` 超过 16000 字符
- `status` / `priority` 非法枚举
- `stage` < 1
- `assignee_type` + `assignee_id` 不匹配当前工作区的有效 member/agent/squad

### Q11. 我用模板创建 Issue 被 400 拒绝「project not found」？

如果模板里引用了他工作区的项目（本版本已知 P2：模板创建/更新时 `project_id` 不校验 workspace 归属），用该模板创建 Issue 时会被 CreateIssue 的 project 校验拒绝。属于数据完整性小问题，无越权读取；建议创建/更新模板时只选择当前工作区的项目。

---

## 运维 / 部署类

### Q12. 升级需要执行哪些迁移？

需要执行 `232_issue_template.up.sql` 和 `233_issue_template_workspace_index.up.sql`（索引为 `CREATE INDEX CONCURRENTLY`，独立文件）。遵循仓库既有迁移流程即可，无额外配置。

### Q13. 迁移会破坏既有数据吗？

不会。仅新增一张表和一条索引，不影响既有表。`is_preset` 播种仅发生在新建工作区事务中。

### Q14. 本次特性需要配置环境变量吗？

不需要。无配置项、无环境变量变更。

---

## 其他

### Q15. 有自动化测试吗？

本次特性未提交自动化测试（后端 handler 单测、前端模板 UI 测试缺失），验证靠手工 API 集成脚本（35/35 通过）。建议后续迭代补充（审查/验证标记 P1）。

### Q16. 模板与 Agent 模板、Autopilot 的 issue_title_template 有什么关系？

三者是不同概念：

- **Issue 模板**：预设 Issue 表单字段（本特性）
- **Agent 模板**：预设 Agent 指令/技能/模型
- **Autopilot issue_title_template**：Autopilot 运行时用 `{reason}` 变量替换生成的标题模板
