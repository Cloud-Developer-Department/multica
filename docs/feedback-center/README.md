# 反馈中心文档

反馈中心（Feedback Center）是 Multica 的全员可见公共反馈池：任何登录用户可浏览、搜索、筛选、查看详情，
并可提交反馈、点赞、评论。本目录集中维护与反馈中心相关的项目文档。

## 文档列表

| 文档 | 说明 |
|---|---|
| [功能说明](features.md) | 功能清单、权限模型、反馈类型、搜索排序、数据模型、兼容性 |
| [系统使用说明](user-guide.md) | 面向普通用户：入口、浏览、提交、点赞、评论、常见问题 |
| [API 文档](api.md) | Feedback 相关 API：路径、参数、请求/响应、校验规则、错误码 |
| [部署说明](deployment.md) | 数据库迁移、本地/自托管部署、部署后验证、运维注意 |
| [项目架构说明](architecture.md) | 面向开发人员：分层架构、前后端模块、数据库设计、数据流、安全 |

## 快速导航

- 进入反馈中心：左侧一级导航「💬 反馈」或底部帮助菜单「反馈」；
- 权限模型：Public Read + Authenticated Write，**无管理员角色 / 无审核流程 / 无独立 RBAC**；
- 反馈类型：`bug`（问题反馈）/ `feature`（功能建议）/ `improvement`（体验优化）/ `other`（其他）；
- 新增/修改 API：`/api/feedbacks*` 系列（详见 [API 文档](api.md)）；
- 数据库迁移：`285_feedback_center` / `286_feedback_vote` / `287_feedback_comment`。

## 关联

- 需求来源：WS-24「将现有反馈功能升级为全员可见的反馈中心」
- 需求分析：`requirement.md`（WS-25）
- 架构设计：`architecture.md`（WS-26）
- 测试报告：`test_report.md`（WS-29）
- 代码审查：`review_report.md`（WS-30）
- 安全审计：`security_report.md`（WS-31）
