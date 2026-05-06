# Tasks
- [x] Task 1: 重构主体顶部 Bar 的浏览器入口
  - [x] SubTask 1.1: 从主体顶部 Bar 中移除浏览器 Tab 列表
  - [x] SubTask 1.2: 将当前 `+` 按钮改为右侧浏览器容器展开按钮

- [x] Task 2: 实现右侧浏览器独立容器
  - [x] SubTask 2.1: 在主内容区右侧增加独立浏览器容器，与左侧 `chat/work` 内容并排
  - [x] SubTask 2.2: 支持右侧浏览器容器的展开、收起和空状态展示

- [x] Task 3: 将浏览器 Tab 管理迁移到右侧容器顶部
  - [x] SubTask 3.1: 在右侧浏览器容器顶部左侧提供 `+` 按钮以新增浏览器 Tab
  - [x] SubTask 3.2: 在右侧浏览器容器顶部实现浏览器 Tab 的切换与关闭
  - [x] SubTask 3.3: 保持浏览器 Tab 的创建、切换、关闭、最近激活回退和地址输入行为不变

- [x] Task 4: 调整布局与状态模型
  - [x] SubTask 4.1: 让浏览器不再作为主内容路由替换 `chat/work` 内容
  - [x] SubTask 4.2: 确保侧边栏、主内容滚动、标题栏拖拽区和网页承载 bounds 与新右侧容器协同正常

- [x] Task 5: 验证与收尾
  - [x] SubTask 5.1: 更新必要的前端测试或校验，覆盖右侧浏览器容器和入口位置调整
  - [x] SubTask 5.2: 验证主体顶部 Bar、右侧浏览器容器和浏览器 Tab 交互符合新需求

# Task Dependencies
- Task 2 depends on Task 1
- Task 3 depends on Task 2
- Task 4 depends on Task 2 and Task 3
- Task 5 depends on Task 2, Task 3, and Task 4
