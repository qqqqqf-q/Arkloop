# functional 测试

此目录用于放置功能级测试：通常需要启动一个或多个外部依赖（例如 Postgres、HTTP 服务），验证最小业务链路能跑通。

默认执行 `python -m pytest` 不包含此目录下的用例；需要时用 `python -m pytest -m functional`。
