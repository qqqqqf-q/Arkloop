# Frontend Agent Workflow

## 核心原则

你作为 Leader，不直接接触代码。explore、execute、review 交给 sub agent。

## 模型选择

- 组件审查、样式调整 → Haiku
- 复杂交互逻辑、状态设计 → Sonnet
- 架构决策 → Sonnet 或 Opus

同理，gpt 系列尽量使用 gpt5.4等高级模型
## 标准工作流

### UI 任务

```
1. [Explore Agent - Haiku]  读 design-system.md，扫描现有组件，确认复用方案
2. [Execute Agent - Haiku]  实现变更，只用现有组件和 CSS 变量
3. [Review Agent - Haiku]   检查：是否复用了共享组件、双语是否都更新了、有无硬编码颜色
```

### 复杂交互 / 状态逻辑

```
1. [Explore Agent - Haiku]   理解现有状态结构
2. [Planner Agent - Sonnet]  设计状态流转
3. [Execute Agent - Sonnet]  实现
4. [Review Agent - Sonnet]   验证交互路径完整
```

## UI 变更自检清单

execute agent 提交前必须确认：

- [ ] 使用了 `@arkloop/shared` 中已有组件，没有自造轮子
- [ ] 颜色全部使用 CSS 变量，无硬编码
- [ ] zh 和 en 两个 locale 都已更新
- [ ] 没有使用不存在的 Tailwind 类
- [ ] 没有用 inline style 覆盖 Tailwind
