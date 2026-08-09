# 研发效能分析平台 —— UI/UX 设计交付索引（阶段8 补充：二期/三期页面）

**Issue**: CLO-238【阶段8：UI/UX补充】二期/三期页面设计（Git贡献/DORA/LDAP维度）
**父需求**: CLO-227（需求方韩维已确认基线；已更新为「全量交付，无分期」）
**上游依据**: CLO-237 阶段7 PRD 补充 v2.0（PRD / API 契约 / 数据口径 / 可行性校验）+ CLO-236 阶段2 UI/UX v1.0
**角色**: UI/UX 设计智能体
**日期**: 2026-08-05
**状态**: 待 Leader 审核（审核通过后放行前端开发，阶段3 体系）

## 定位说明

本阶段在 **CLO-236（阶段2，v1.0）已交付的 Tab1 ①–⑤ / Tab2 设计基础上补充**，覆盖 PRD v2.0 新增三大模块：Tab3 Git 贡献（ELOC/质量雷达/代码仓活跃）、Tab4 DORA（交付周期/部署频率/变更失败率/MTTR）、Tab1 生命周期与部门维度（⑥⑦⑧ + PageHeader 部门筛选器）。v1.0 设计**不推翻**，仅按 `07` 文档做定向微调。

## 交付物清单（阶段8 增量）

| 文件 | 说明 |
| --- | --- |
| `00-设计总览与四部分结论-阶段8补充.md` | 阶段8 完成结论 / 交付成果清单 / 风险 / 下一步（四部分输出） |
| `01-组件规范与Token对齐-阶段8增量.md` | 新增组件规范与 token 对齐：`SourceGuideState`（数据源未接入引导态）、`ElocRankingList`、`QualityRadar`、`RepoActivityTable`、`DoraTrendChart`、`DeployFrequencyChart`、`FailureDetailTable`、`LifecycleCard`、`CutoffSuccessCard`、`DepartmentSlicer`、`DepartmentFilter` |
| `02-页面设计稿-Tab3-Git贡献.md` | Tab3 全页面高保真（KPI / ELOC / 质量雷达 / 代码仓活跃），全状态 |
| `03-页面设计稿-Tab4-DORA.md` | Tab4 全页面高保真（KPI / 交付周期 / 部署频率 / 失败率+MTTR），全状态 |
| `04-页面设计稿-Tab1-生命周期与部门维度.md` | Tab1 ⑥⑦⑧ + PageHeader 部门筛选器，全状态 + LDAP 引导态 |
| `05-标注与资源-阶段8增量.md` | 像素级标注：间距/字号/圆角/颜色/组件路径/交互/新增资源 |
| `06-前端视觉还原校验点清单-阶段8增量.md` | 前端阶段8 增量视觉还原校验点（可勾选），Leader 放行依据 |
| `07-与一期设计一致性微调说明.md` | Tab Badge 语义、漏斗 Merged 三态化、引导态/空态区分、文案统一等一致性微调明细 |
| `mockups/tab3-git.html` + `shot-tab3-git.png` | Tab3 高保真视觉稿 + 截图 |
| `mockups/tab4-dora.html` + `shot-tab4-dora.png` | Tab4 高保真视觉稿 + 截图 |
| `mockups/tab1-lifecycle.html` + `shot-tab1-lifecycle.png` | Tab1 生命周期/部门维度区块视觉稿 + 截图 |
| `mockups/states-source.html` + `shot-states-source.png` | 「数据源未接入」引导态全场景视觉稿 + 截图 |

## 关键结论（四部分输出）

### ① 完成结论

基于 CLO-237 PRD v2.0 完成二期/三期页面**全量高保真补充设计**，覆盖 Tab3 Git 贡献、Tab4 DORA、Tab1 生命周期与部门维度，全部关键状态（正常/空态/加载/错误/**数据源未接入引导态**）齐备。核心设计决策：
- **「数据源未接入」引导态统一组件 `SourceGuideState`**（`source_status.ready=false` 契约驱动，含「前往配置」按钮），与空态严格区分。
- **新增 11 个组件**（ELOC 排行/质量雷达/仓库活跃表/交付周期趋势含口径自动降级/部署频率/失败明细/生命周期/熔断成功率/部门切片/部门筛选器/引导态），全部对齐 `packages/ui` + `docs/design.md` token，**零硬编码**。
- **Tab1/Tab2 一致性微调**：Tab3/Tab4 去「二期/三期」Badge；漏斗 Merged 段由「占位」改「数据源未接入引导态」（三态化）；文案统一 PRD §7。

### ② 交付成果清单

见上方表格：7 份核心设计文档（总览/组件规范/三份页面稿/标注/校验点/一致性说明）+ 4 份高保真 HTML 视觉稿 + 4 张截图，全部归档于 `deliverables/engineering-analytics-2026-08-05/设计/`。

### ③ 现存问题与风险

1. **外部数据源未接入是常态**：Git（二期）/部署流水线与 LDAP（三期）接入前，Tab3/Tab4/Tab1 ⑥⑦⑧ 长期呈引导态；前端必须区分引导态/空态，避免误渲染。
2. **交付周期口径自动降级**：Tab4 ② 默认「Issue→部署」，未接入时自动降级「Issue→Merged」+「替代口径」角标——降级判断须由前端依据 `source_status.ready` 触发。
3. **ELOC 工具链选型未定**：G1 依赖 AST 工具链输出（可行性 §7.3，行数兜底先行），设计已预留「工具链未配置」引导态。
4. **快照数据时效**：质量雷达展示最新快照 + 「数据截至 {date}」角标（E20），不做硬 TTL。
5. **部门维度跨域不关联**：Tab3/Tab4 不受部门筛选影响；未接入部门数据时传 `department_id` → 400，前端以 `ready=false` 隐藏筛选器规避。
6. **空值语义实现成本**：`source_status` 组合判定 + `null→-` / `[]→空态` / `0→数值零` 需前端严格按契约 §0.5/§0.7 落实。

### ④ 下一步协作建议

1. **Leader 审核阶段8 设计稿**（重点：`01` 新增组件规范、`02/03/04` 全状态覆盖、`07` 一致性微调、引导态与空态区分）。
2. 审核通过后**放行前端开发**（CLO-232 体系）：按 `06-前端视觉还原校验点清单-阶段8增量.md` 逐模块 1:1 还原；Tab1/Tab2 按 `07` 微调。
3. 前端同步将新组件纳入 `packages/views/analytics/` 或 `charts/`（新增须符合 design.md 规范）。
4. 后端实现 G1–G4 / D1–D2 / L1–L2 时与前端联调校准 `source_status` 判定、ELOC 人/机分组、雷达反轴、合并周期秒→天换算（见 `05` 标注）。
5. 数据接入节奏：二期 GitCode/本地仓库 + ELOC 工具链点亮 Tab3；三期部署事件上报 + LDAP/手动部门映射点亮 Tab4 与 Tab1 ⑥⑦⑧——接入后引导态自动切换为数据态，无需改设计。

> 配套产品文档（PM 侧）：`PRD-研发效能分析平台.md` / `API契约-研发效能分析平台.md` / `数据口径说明.md` / `可行性校验.md`（均为 v2.0），见同目录 README.md（产品设计交付索引）。
