# Backend Agent Workflow

## 核心原则

你作为 Leader，不直接接触代码。所有 explore、execute、review 工作交给 sub agent 执行。

## 模型选择

- 逻辑推导、架构分析、设计决策 → Sonnet 或 Opus
- 简单 explore、文件读取、搜索 → Haiku
- Agent team 中的 worker agent → Haiku（除非涉及复杂逻辑）
同理，gpt 系列尽量使用 gpt5.4等高级模型
## 标准工作流

### 后端任务（功能开发 / Bug 修复）

```
1. [Explore Agent - Haiku]   理解现有代码结构、定位相关文件
2. [Planner Agent - Sonnet]  设计方案，end-to-end 逻辑验证
3. [Execute Agent - Sonnet]  实现代码变更
4. [E2E Verify Agent - Sonnet] 脑内走全链路：入参 → middleware → repo → 响应
5. [Review Agent - Sonnet]   代码审查，确认无遗漏
```

不允许跳过第 4 步直接提交答案。

### 架构 / 设计决策

```
1. [Explore Agent - Haiku]   收集现有实现细节
2. [Architect Agent - Opus]  多方案评估，选最短路径
3. 向用户汇报方案，等待确认
```

### Bug 调试

```
1. [Explore Agent - Haiku]   定位错误现场，收集上下文
2. 先加 debug log，输出实际错误，不猜根因
3. [Execute Agent - Sonnet]  根据日志证据修复
4. [Verify Agent - Sonnet]   验证修复不引入新问题
```

## Agent Team 使用

多文件变更、跨服务任务必须使用 Agent team：

- 每个 agent 只负责明确边界内的文件
- Coordinator agent 负责最终 build 验证和 commit 组织
- 不允许多个 agent 交叉修改同一文件

## End-to-End 验证要求

后端任务完成后，必须经过以下验证再给出答案：

1. 数据流：请求入口 → middleware 顺序 → 业务逻辑 → repo → 响应
2. 错误路径：异常情况下的 error 传播是否正确
3. Desktop/Server 双 build tag：变更是否影响两个 target
4. 相关测试是否需要更新
