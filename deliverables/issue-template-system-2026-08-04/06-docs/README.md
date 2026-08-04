# 06-docs — 文档交付

**特性**: Issue 模板系统（CLO-159）
**日期**: 2026-08-04
**角色**: Documentation（杜兰特）
**Issue**: CLO-163 【文档】Issue 模板系统文档更新

## 文档清单

| 文档 | 读者 | 内容 |
|------|------|------|
| `feature-guide.md` | 用户 / 开发 | 功能说明：预置模板、使用场景、字段说明、边界与限制 |
| `api.md` | 开发 | API 参考：端点、数据模型、Request/Response/Error/Curl/SDK、WS 事件、数据库迁移 |
| `release-notes.md` | 用户 / 产品 | 发布说明：新增功能、迁移、兼容性、已知限制、验证摘要 |
| `faq.md` | 用户 / 运维 | FAQ 与故障排查：预置模板、409/400 错误、迁移、字段语义 |

## 文档更新结论

| 检查项 | 是否需要更新 | 说明 |
|--------|-------------|------|
| README | 否 | 主 README 无需变更；本特性文档以交付件形式入库 |
| API | 是 | 新增 `/api/issue-templates` 端点（见 `api.md`） |
| Release Note | 是 | 见 `release-notes.md`（目标版本 0.5.0 规划） |
| FAQ | 是 | 见 `faq.md` |
| Configuration | 否 | 无配置项 / 环境变量变更 |
| Deployment | 否 | 仅新增迁移 232/233，遵循既有迁移流程 |
| Architecture | 否 | 沿用 labels/properties 工作区目录模式，无架构变更 |
| ChangeLog | 视发布而定 | 上线时需在站点 changelog 补充条目（见 release-notes） |

## 角色交接

文档阶段完成后，本特性移交 **DevOps 角色（姚明）** 执行本地部署验证（CLO-169，Stage 7）。

## 文档一致性确认

以下内容均基于最终实现代码核对，非推测：

- 路由注册：`server/cmd/server/router.go`（`/api/issue-templates` 5 端点）
- 数据模型：`server/migrations/232_issue_template.up.sql`
- 预置模板：`server/internal/handler/issue_template.go`（`presetIssueTemplates`）
- 前端类型/API：`packages/core/types/issue-template.ts`、`packages/core/api/client.ts`
- WS 事件：`server/pkg/protocol/events.go`、`packages/core/types/events.ts`
- 已识别的已知限制：预置仅新建工作区播种（P1）、project_id 归属校验（P2）、无自动化测试（P1）、手动创建面板才提供模板选择器
