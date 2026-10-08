# 示例

[English](README.md)

## 本地部署

在项目根目录运行：

```sh
go run ./examples/local
```

需要 Linux 或 macOS、Go 1.25 或更新版本、Git 和 `/bin/sh`。无需外部仓库、Node.js、agent 或凭证。端口 8080 必须可用；也可以通过 `go run ./examples/local -port 8081` 选择其他端口。

示例将随附的 [HTTP 服务](local/service/main.go)复制到新建的临时 Git 仓库并提交，准备绑定该提交的计划，以仅所有者可读写的权限将规范化 Spec 与 Snapshot 保存到 `plan.json`，然后通过本地运行时检出固定提交、构建并启动服务，探测 `/health`。`result.json` 保存完整或部分执行证据。示例创建的全部文件都位于输出的临时目录中，不修改项目工作区。服务只监听 `127.0.0.1`。

出现 `Ready: http://127.0.0.1:8080` 后，在另一个终端访问：

```sh
curl http://127.0.0.1:8080/
# Hello from deployd's local example!
curl http://127.0.0.1:8080/health
# ok
```

启动 context 有两分钟期限，部署成功后会被取消；就绪的服务仍持续运行。在示例终端按 Ctrl+C，通过独立的五秒清理 context 停止已保存的精确进程。程序先关闭运行时，再删除临时目录。执行或清理失败时，命令以非零状态退出，并保留目录供检查；确认并处理可能残留的进程后，可手动删除输出的目录。库的 `Stop` 会保留工作区，本例由应用负责删除目录。强制杀死示例程序会绕过其清理逻辑。

该示例演示单个进程内本地运行时的开发用法，不实现持久化控制器所需的重启恢复、租约或自动重试。相关边界参见[控制器集成](../docs/controller-integration.zh.md)。

## API 示例

简短的 Go API 示例保留在对应包旁边：

| 文件 | 内容 | 默认验证方式 |
| --- | --- | --- |
| [example_test.go](../example_test.go) | Prepare、Apply 和 Stop | 仅编译，使用占位仓库 |
| [plan_example_test.go](../plan_example_test.go) | NewPlan 和 Restore | 执行并检查输出 |
| [recovery_example_test.go](../recovery_example_test.go) | 检查点持久化与进程核对 | 仅编译，使用占位 agent |
| [logs_example_test.go](../logs_example_test.go) | 有界脱敏的 FollowLogs 输出 | 仅编译，使用占位 agent |

`make examples` 编译完整程序，不执行它们。`make coverage` 也会编译完整程序并运行 API 示例测试；`examples/` 下的程序不参与库覆盖率门槛。默认检查不会启动该演示或访问外部服务。
