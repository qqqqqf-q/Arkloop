后台管理 (Backend Management)

Admin (管理员)
职位 (Role/Position)
审计可见性 (Audit Visibility)
数据安全 (Data Security)
数据导出 (Data Export)
订阅管理 (Subscription Management)
数据检查 (Data Check/Inspection)

Agent Loop 架构图
智能体的运行逻辑、记忆机制和工具链。

1. 记忆与系统约束 (Top Section)
System Prompt: 规范约束
长期记忆: 名称、你的职位、公司地址等
短期记忆: 最近聊了什么、在推进什么
项目记忆: 单独存放，但并非完全独立

2. 核心循环与工具 (Loop Section)
Tools (工具):
Web search: 包括 web fetch 和 web search
Memory search (记忆搜索)
Code execute: 执行 excel/word 操作、math tool (数学工具)
Shell: sandbox shell (沙箱环境)
Skills / MCP support: (备注：不重要)
Subagent(用来处理超长的法律文献以及web fetch等等,使用其他模型)
PDF watcher(正如其名,拿来看PDF的)

给高频计算部分加上模型提供商的cache
3. 运行模式 (Operational Modes)
Plan Mode (规划模式): 旨在交付需求与解决方案，而不是虚空的问答。
Review Agent (审核智能体): 使用较强的模型 (Model) 和最短的上下文，分析单次输出的置信度。
优化建议：要把长对话拆分成短对话进行 Review。

4. 界面/侧边栏布局 (Sidebar)
通用聊天
法律咨询
财务审计
项目:
[xxx 案] 关于 xxx 的聊天
[xxx 案] 关于 xxx 的聊天
最近聊天:
关于 xxx 的可行性
关于 xxx 的人物

类Claude.ai官网样式,精致,简单动画

后端技术路线:
兼容Sqlite+PostgreSQL(同时支持本地部署和云端SaaS)
后期兼容Redis,这里不写
支持管理界面,有更多管理功能
且基于Python
重视数据安全问题,要支持完全的加密

此为企业级系统

前端:
React+Taliwind,主要方向为类Claude.ai,提供后台管理