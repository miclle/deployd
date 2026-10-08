# 测试

[English](testing.md)

## 必需检查

运行 `make` 或 `make check`，即可执行依赖文件一致性检查、lint，以及带有 CI 同一覆盖率门槛的 race 测试。也可以单独运行各项任务：

| 任务 | 用途 |
| --- | --- |
| `make fmt` | 直接格式化 Go 源码文件 |
| `make fmt-check` | 检查 Go 格式，不修改文件 |
| `make gomod` | 运行 `go mod tidy -diff`，不修改依赖文件 |
| `make lint` | 验证 linter 配置，并检查格式、errcheck、govet、ineffassign、staticcheck 和 unused |
| `make test` | 不使用缓存并运行 race 测试 |
| `make examples` | 编译可运行的示例程序，不执行它们 |
| `make coverage` | 运行 race 测试并检查每个包的语句覆盖率 |

`make coverage` 输出 Git 忽略的 `coverage.out`、函数覆盖率，并检查每个包：核心至少 95%，每个适配器及内部辅助包至少 90%。CI 在 Linux 的三个 Go 版本（1.25、1.26 和 1.27）上执行该门槛；macOS 也在 Go 1.27 上执行，包括 Darwin 专用的进程清理测试。CI 设置 `GOTOOLCHAIN=local`，遇到不兼容依赖时直接失败，避免静默切换到更新的 Go 工具链。独立的 Go 1.25 任务运行 `make gomod`，检查最低支持 Go 版本下的依赖文件一致性。其他 CI 检查复用 `make lint` 和 `make coverage`。

本地 Make 任务也默认设置 `GOTOOLCHAIN=local`，测试和依赖检查使用已安装的 Go 版本。需要复现其他 CI Go 版本时，可显式指定工具链，例如 `GOTOOLCHAIN=go1.25.6 make gomod` 或 `GOTOOLCHAIN=go1.25.6 make coverage`。CI 的操作系统与 Go 版本矩阵由工作流选择，本地任务在宿主平台上执行。

检查由 push、pull request 和手动触发。同一事件及 PR 或 ref 的新运行会取消旧运行。Linux 矩阵任务失败时不会互相取消。测试任务限时 20 分钟，lint 限时 10 分钟，依赖检查限时 5 分钟。Checkout 和 Go setup Actions 固定到发布版本对应的提交 SHA。

`make lint` 为 golangci-lint 设置 `GOTOOLCHAIN=go1.26.6`，与 CI lint 任务使用相同的 Go 次版本。CI 安装固定版本的 linter 后运行 `make lint LINT_GOTOOLCHAIN=local`，使用已设置好的 Go 1.26 工具链，避免加载 linter 无法解析的新版本地标准库。首次使用时，Go 命令会按需下载所需工具链；离线环境需要提前安装。可通过 `LINT_GOTOOLCHAIN` 覆盖，例如 `make lint LINT_GOTOOLCHAIN=go1.26.6`。linter 必须支持所选 Go 版本，且编译所用版本不得低于该版本，可通过 `golangci-lint version` 检查。仅设置 `.golangci.yml` 中的 Go 版本不会选择加载包时使用的工具链。测试与覆盖率继续使用默认 Go 工具链，lint 不会被跳过。

`make coverage` 首先运行 `make examples`。示例程序使用同一个 Go module，纳入 lint 和测试的包发现范围，但 `examples/` 下的包不参与库覆盖率门槛。本地演示无需独立 module 或外部依赖。

## 覆盖矩阵

| 范围 | 证据 |
| --- | --- |
| 执行参数 | UTF-8、空白或 NUL 命令、端口、默认值、幂等规范化、越界目录与健康 URL |
| 计划 | 源码与 Spec 副本不可变、固定样例验证版本化摘要、结构化持久化与恢复、所有参数漂移、旧版与未来版本拒绝、非法证据 |
| 内核 | 所有阶段确认失败、期限与取消、脚本错误、不确定或错误启动引用、进程退出、就绪重定向与失败、清理失败 |
| 输出 | 大小限制、跨块或重叠秘密值、截断边界处的完整或不完整凭证、UTF-8 分块边界、迟到回调 |
| Git | 真实本地仓库、解析后 HEAD 变化、无配置文件的固定版本检出、失败、超时、有界脱敏输出 |
| 本地运行时 | 真实进程、独立存活、有限命令取消、回收 leader 前清理进程组、进程与标签身份、监督进程被外部终止、关闭和真实退出码 |
| envd 运行时 | HTTP 协议夹具、流帧、鉴权、PID 与退出事件、查询与信号、陈旧或不唯一标签、缺失或畸形或超大响应、重定向、PID 丢失后的清理、PID 被复用时的流异常清理、停止期限、session 创建前后的真实监督进程与子进程清理 |
| 端到端 | 真实 Git 与 HTTP 服务：固定提交、安装产物、就绪、重复尝试、停止、健康失败清理、启动后取消、无配置文件、安装前后目录符号链接边界 |

API 示例通过编译检查；测试不会访问其占位仓库或 agent。NewPlan 和 Restore 示例还会执行并验证输出。[可运行的本地示例](../examples/README.zh.md)使用随附的 Go 服务和临时本地 Git 仓库，需要显式运行来验证部署、HTTP 就绪与信号触发的清理；默认检查只编译，不启动服务。集成测试将 Go 测试二进制作为子进程 HTTP 服务，不依赖 Node.js 或 Python。环境需要 Git 和 `/bin/sh`。本地执行支持 Linux 和 macOS；CI 在两个平台上验证。

监督进程测试覆盖 `/bin/sh`，安装了 dash 时还会显式覆盖 dash，包括 macOS 环境。Linux 使用原生 `setsid` 工具；macOS 使用调用真实 session 系统调用与 exec 的替代实现。测试检查正常退出和 TERM 停止时的子进程组清理；测试失败时也会清理独立进程组，并限制输出管道的等待时间。

协议夹具只证明适配器契约。真实 envd 验收仍需要已有隔离目标，验证源码鉴权、断开流后进程存活、入口路由、精确标签停止、取消清理和目标账号权限。自动测试不会创建或销毁远端资源。

[真实 envd 验收](envd-acceptance.zh.md)说明显式启用、环境配置、进程与适配器重建测试，以及固定版本部署验证。

实时输出测试验证阶段结束前交付、混合流、字节预算、UTF-8 边界、迟到回调，并与整流参考算法比对重叠或跨块脱敏。日志协议测试覆盖 PID 确认、错误标签、取消、畸形或失败流，以及不发送信号。

长凭证的重叠匹配覆盖单次与分块写入，以及末尾不完整凭证。每个秘密值的每轮匹配对每个待输出字节最多标记一次，避免重叠匹配造成重复处理。可运行 `go test ./internal/redact -run '^$' -bench BenchmarkStreamOverlappingMatches`，测量 1 KiB 和 32 KiB 重复字节凭证下的处理性能。
