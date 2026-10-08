# 测试

[English](testing.md)

`make lint` 检查 gofmt、errcheck、govet、ineffassign、staticcheck 及未使用代码。`make test` 禁用结果缓存并启用 race 检测。`make coverage` 生成 Git 忽略的 `coverage.out` 和逐函数覆盖率。

协议测试覆盖有界严格配置、不可变计划和可序列化进程引用。执行与适配器测试还应覆盖阶段失败、取消、未启动／提前退出、清理失败、陈旧进程身份、异常远端响应和输出上限。本地真实进程测试、协议 Fixture 与真实远程运行环境验收是不同证据。
