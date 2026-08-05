# Bug 修复报告（CLO-187）— 模板正文内容在新建 issue 时未带出

**日期**: 2026-08-05
**角色**: Coding（库里）

## 一、问题现象

在创建 Issue 对话框中选择模板后，模板的标题、优先级、负责人等字段都能正确带出，但**正文（描述）内容为空**，即使模板配置了 `body_template`。

## 二、根因定位

### 2.1 模板应用逻辑

`packages/views/modals/create-issue.tsx` 的 `applyTemplate`（约第 407-426 行）：

```tsx
const applyTemplate = (templateId: string) => {
  const tpl = issueTemplates.find((t) => t.id === templateId);
  if (!tpl) return;
  const nextTitle = tpl.title_template || title;
  setTitle(nextTitle);
  setDraft({ title: nextTitle });
  if (tpl.body_template) {
    setDraft({ description: tpl.body_template });
    // Remount the description editor so it picks up the new defaultValue.
    setFormResetKey((k) => k + 1);
  }
  ...
};
```

选中模板后通过 `setFormResetKey` 递增重挂载 key，让正文编辑器以新的 `defaultValue`（即 `draft.description`）重新挂载。

### 2.2 重挂载 key 只挂在了标题编辑器上

渲染部分（约第 864-893 行）：

```tsx
<TitleEditor
  key={formResetKey}          // ← 标题编辑器挂了 key
  ref={titleEditorRef}
  defaultValue={draft.title}
  ...
/>

<ContentEditor
  ref={descEditorRef}          // ← 正文编辑器没有 key！
  defaultValue={draft.description}
  ...
/>
```

`TitleEditor` 有 `key={formResetKey}`，但 `ContentEditor` 没有。`ContentEditor` 将 `defaultValue` 视为**仅挂载时读取**（`packages/views/editor/content-editor.tsx` 文档注释：`Initial markdown, read once when the editor mounts`，并有单测「treats defaultValue as mount-only」佐证）。

因此 `setFormResetKey((k) => k + 1)` 只会重挂载标题编辑器，正文编辑器不会重新挂载，新的 `defaultValue` 也就永远不会被读取 → 模板正文无法带出。

### 2.3 影响链

```
applyTemplate 选择模板
  → setDraft({ description: tpl.body_template })
  → setFormResetKey(k + 1)   // 期望重挂载正文编辑器
    → 只有 TitleEditor 有 key={formResetKey}
      → ContentEditor 未重挂载
        → defaultValue 仅挂载时读取（mount-only）
          → 正文编辑器仍显示旧内容/空内容
```

## 三、修复方案

为 `ContentEditor` 补上与 `TitleEditor` 一致的 `key={formResetKey}`，使模板应用（以及 `resetForNextIssue` 清空表单）时正文编辑器同样被重挂载、读取最新 `defaultValue`：

```tsx
<ContentEditor
  key={formResetKey}
  ref={descEditorRef}
  defaultValue={draft.description}
  ...
/>
```

改动仅 1 行，与现有 `TitleEditor` 的既有模式保持一致。

## 四、改动文件

| 文件 | 改动 |
|------|------|
| `packages/views/modals/create-issue.tsx` | `ContentEditor` 增加 `key={formResetKey}` |
| `packages/views/modals/create-issue.test.tsx` | 新增回归测试 + `@multica/core/issue-templates` mock |

## 五、影响范围

- 仅影响创建 Issue 对话框手动模式（`ManualCreatePanel`）中正文编辑器的重挂载行为。
- 模板应用、`resetForNextIssue`（Create another 清空表单）两条路径都受益于同一 key 机制，行为一致。
- 不影响数据库、后端 API、其他编辑器宿主（`value` 受控模式不受影响）。

## 六、兼容性

- 无接口变更、无配置变更、无数据迁移。
- 未应用模板时 `formResetKey` 不变，编辑器行为与修复前完全一致。
- 既有「Create another 清空正文」逻辑（`clearContent()` + key 递增）在 key 补齐后行为不变，回归测试覆盖。

## 七、风险

- 🟢 低风险 — 1 行改动，与既有 `TitleEditor` 模式一致。
- 🟢 回归测试覆盖模板带出主路径，确认修复前失败、修复后通过。
