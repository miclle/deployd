# 适配器

[English](adapters.md)

## Git 源码

`source/git.New` 接受不含凭证的 HTTPS 仓库 URL，以及分支、Tag、完整 SHA 或 `HEAD`。控制进程和运行环境均需安装 Git。解析使用临时仓库、默认五分钟时限、有界 Blob 读取，且配置必须是普通文件。源码准备只拉取保存的 SHA 并校验 HEAD，不再读取原始可变引用。

本地绝对路径要求显式设置 `AllowLocal: true`，仅用于可信本地执行。`Env`、`CheckoutEnv` 分别提供控制进程和目标环境的临时 Git 认证环境；控制进程的凭证 Helper 不会自动出现在远端。调用方负责仓库 Host 白名单和私有源码授权。Git 错误丢弃 stderr，输出有界并根据 CheckoutEnv 脱敏。

## 本地 POSIX 运行时

`runtime/local` 支持 Linux、macOS，以宿主账号执行，适用于可信开发／测试或具有外部隔离的部署 Agent。有限命令受取消控制；长期服务使用独立进程组。停止校验内存中的运行时／进程／标签组合后终止对应组。Close 停止持有的工作负载。记录不能跨控制进程重启恢复。

监督进程通过私有管道报告工作负载退出码，并在清理完成前保留进程组 leader；运行时先终止进程组，再回收 leader，避免发送信号期间 PID 被复用。应用脚本不继承退出码报告描述符。在 macOS 上，外部终止监督进程可能使内核对空组返回 `EPERM`，此时使用 `/bin/ps` 核对组是否为空；检查受清理上下文和一秒时限约束。

## envd 运行时

`runtime/envd` 通过标准库 HTTP 使用 Connect JSON 访问 envd Process 服务。调用方提供已有 Agent 地址、运行时身份、临时 Token、用户和端口到就绪地址的映射。Agent 请求禁止重定向，每个响应／Envelope 最大 1 MiB，不无限累计输出。Start 确认 PID 后关闭观察流；Agent 必须在流断开后保持进程运行。库不持有持续日志订阅。

Inspect／Stop 校验运行时、PID 和标签；未知启动结果可通过唯一标签收敛。Agent API 不提供原子比较后发送信号的 fencing：调用方必须串行操作同一次执行，且不得复用操作标签。首版不含直接 SSH 适配或基础设施创建。

Linux agent 必须提供 `setsid`，POSIX shell 的 `kill` 必须支持负数进程组标识。进程组清理使用兼容 dash 和 bash 的 `kill -s KILL -- -PGID`。监督进程将工作负载放在独立 session 中；Stop 向监督进程发送 TERM，由它杀死并回收进程组后退出，正常退出也会清理剩余子进程。前台脚本不得后台化或逃逸到其他 session。有限命令失败时会按唯一标签核对清理，即使初始 PID 响应丢失；观察到的非零退出保留退出码，不保留 agent 原始错误字符串。

取消时先暂停子进程，再向进程组发送信号；若 `setsid` 尚未创建进程组，则直接终止该子进程，避免取消后服务仍继续启动。流异常清理必须先按执行标签核对身份，不会直接向曾观察到的 PID 发送信号。

使用新的 agent 版本前运行[显式启用的验收测试](envd-acceptance.zh.md)。默认测试不访问远端目标。

可选 `Logs` 通过 Process Connect 附着，由调用方通过 `FollowLogs` 管理订阅；其生命周期独立于进程。参见[日志](logs.zh.md)。
