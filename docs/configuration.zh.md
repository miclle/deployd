# 执行参数

[English](configuration.md)

应用负责配置文件、格式、字段版本、环境选择、覆盖规则、解析限制和业务校验，再将已解析的配置映射为 `deploy.Spec`。deployd 不读取或解析配置文件，也不要求源码仓库中存在配置文件。

## Spec

`Spec` 描述一个前台 HTTP 服务：

| 字段 | 契约 |
| --- | --- |
| `WorkingDirectory` | 仓库相对目录，默认为 `.`，会进行规范化 |
| `InstallCommand` | 必需且非空白的安装或构建脚本，原样保留 |
| `StartCommand` | 必需且非空白的前台服务脚本，原样保留 |
| `Port` | 1～65535 的整数 |
| `Healthcheck.Path` | 相对于 origin 的 HTTP 路径，默认为 `/`；拒绝外部地址、查询参数、片段和不安全的编码路径 |
| `Healthcheck.TimeoutSeconds` | 零值使用 60 秒，负数会失败，执行时最多等待 300 秒 |

所有字符串须为有效 UTF-8，命令不得包含 NUL 字节。目录路径拒绝绝对路径、越界路径、控制字符、反斜杠和驱动器分隔符。安装前和启动前都会在运行时检查物理目录边界，包括符号链接解析。服务必须保持前台运行，并监听可访问接口，通常为 `0.0.0.0`。

`NormalizeSpec` 返回经过校验和规范化的副本。`NewPlan` 和 `Prepare` 在执行前进行相同校验。应用可以施加更严格的业务规则，例如在自己的配置格式中拒绝显式零时限。库无法区分未提供整数字段和显式零值。

命令不得包含凭证。运行时环境变量通过 `Options.Env` 临时传入，独立于不可变源码与参数身份。业务配置的格式、原始字节和文件路径不参与执行摘要。

## 不可变计划

已知完整提交时，使用 `NewPlan(resolvedSource, spec)`。如果配置位于仓库中，应先解析源码版本，再从**该精确提交**读取业务配置并映射为 Spec。参数不依赖仓库文件时，可以使用 `Prepare(ctx, source, spec)` 校验参数、一次性解析源码并创建计划。

创建资源前保存 `plan.Snapshot()` 和 `plan.Spec()`。`Restore(snapshot, spec)` 重新校验保存的参数与摘要，不读取文件，也不访问源码服务。须保存计划返回的规范化 Spec；恢复时拒绝缺失默认值或未规范化的目录路径。

`Snapshot.Version` 是库的计划证据版本，与业务配置字段版本独立。版本 1 的 `SpecHash` 是 `sha256:` 加上以下字节的 SHA-256 小写十六进制值：

1. `deployd/spec/v1` 的字节，随后是一个 NUL 字节。
2. 依次编码 `WorkingDirectory`、`InstallCommand`、`StartCommand`、`Port`、`Healthcheck.Path`、`Healthcheck.TimeoutSeconds`。

每个字符串编码为无符号 64 位大端字节长度，随后是 UTF-8 字节；每个整数编码为无符号 64 位大端值。计算摘要前先规范化参数。字段、规范化规则或执行语义变化须使用新的证据版本；缺失或不支持的版本直接失败。摘要用于检测参数漂移，不提供鉴权、授权或调用方存储未被改写的证明。须一起保护快照与参数。计划包含命令；生命周期事件只携带快照证据，不携带 Spec。

参见[控制器迁移说明](controller-integration.zh.md)和[可编译的计划与持久化示例](../plan_example_test.go)。
