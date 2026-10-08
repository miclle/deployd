# 控制器集成

[English](controller-integration.md)

## 计划与源码身份

创建资源前，将 `Plan.Snapshot()` 与规范化的 `Plan.Spec()` 一起持久化。存储格式由调用方选择；恢复只需要这些结构化值，不需要原始业务配置字节。按需要限制存储权限或加密；Spec 包含命令，不得写入生命周期事件。参见[可执行恢复示例](../plan_example_test.go)。

配置位于仓库中时，先调用 `source.Resolve(ctx)`，从返回的完整提交读取并解析业务文件，再调用 `NewPlan(resolved, spec)`，不得单独读取变化中的分支。使用数据库或 API 配置时，在创建计划前固定选定的业务版本及其展开参数。参数已就绪时可用 `Prepare(ctx, source, spec)`。`Restore(snapshot, spec)` 不读取配置，也不重新解析源码。证据不可用时，应在创建资源或执行前失败。

Git 适配器将不含凭证的仓库 URL 用作 `SourceID`。拥有稳定仓库 ID 的控制器还须在源码适配器中验证 provider 的 ID、访问授权及重命名或转移规则。映射既有控制器记录时不得丢弃这些校验。

## 从 YAML 接口迁移

本次包含接口与持久化协议的破坏性变更：

| 原契约 | 新契约 |
| --- | --- |
| `Config`、`ParseConfig`、YAML `version` | 业务字段与解析器映射到 `Spec`，由 `NormalizeSpec` 校验执行参数 |
| `Prepare(ctx, source, configPath)` | `Prepare(ctx, source, spec)`，或先 `source.Resolve(ctx)` 再 `NewPlan(resolved, spec)` |
| `Source.Resolve(ctx, configPath)` 返回 YAML | `Source.Resolve(ctx)` 只返回 `SourceID` 与完整 `CommitSHA` |
| `Plan.Config()` / `ConfigBytes()` | `Plan.Spec()` 返回规范化的结构化参数 |
| `Snapshot.ConfigPath` / `ConfigHash` | `Snapshot.Version` / `SpecHash` 绑定计划证据规则与规范化参数 |
| `Restore(snapshot, yamlBytes)` | `Restore(snapshot, spec)` |
| `ErrInvalidConfig`、`NormalizeConfigPath`、`MaxConfigBytes` | `ErrInvalidInput` 表示非法执行输入；文件校验和解析限制由应用负责 |
| Git `ErrConfigUnavailable` | 配置读取和解析失败由应用负责 |

库不再定义 YAML 别名、重复或未知键、多文档规则及文件字段。应用解析器自行选择合适的严格程度与限制。执行仍拒绝越界目录、非法命令或端口、不安全的健康路径。零就绪时限现在使用 60 秒；业务字段可以另外拒绝显式零值。

旧快照不含支持的计划证据版本，会被拒绝。迁移由控制器执行：校验旧源码身份、完整提交和精确原文件哈希，按旧字段与默认值解析并映射为 Spec，再使用保存的源码证据调用 NewPlan，一起保存新 Snapshot 与规范化 Spec。不得重新解析原跟踪分支，也不得直接替换哈希绕过校验。按业务策略保留旧审计记录。新快照不授予重放进行中尝试或复用工作区与标签的许可；须先核对保存的进程与检查点。

[执行参数](configuration.zh.md)说明摘要和版本契约。安装前和启动前，库会验证工作目录边界；不要求或监测业务配置文件。

就绪要求 **2xx 和所引用进程存活**；3xx 不成功，也不跟随重定向。以前接受重定向的控制器需要调整入口或健康路径。`Result.Endpoint` 是就绪 origin；公网入口激活和流量切换须另行验证。

## 环境与生命周期映射

通过 `Options.Env` 提供可信的临时应用变量。动态入口后的 Vite 应用可由控制器将 `__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS` 设为精确公网 Host；无需写入版本化部署字段或允许任意 Host。Git 检出凭证属于源码适配器的 `CheckoutEnv`，不得进入应用环境；运行时令牌留在运行时适配器内。

将库阶段映射到控制器状态，在 lease 保护下确认 `OnCheckpoint`，并保存最终部分结果与清理失败。接管后遵循[恢复合同](architecture.zh.md#控制器接管)，库不会将 Revision 键解释为重放安装的许可。停止应用不删除工作区或目标；资源清理、到期、任务重试、活动版本指针、配额和 fencing 由控制器负责。
