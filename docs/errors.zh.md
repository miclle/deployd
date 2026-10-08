# 错误处理

[English](errors.md)

使用 `errors.Is` 和 `errors.As`，不得解析打印文本。`StageError` 保存失败阶段与独立清理错误，其消息有意排除命令、执行参数和上游响应体。`CommandExitError` 保存安装或 Git 命令直接观测到的非零退出码。非零退出不证明脚本未产生副作用。

`deploy.ErrInvalidInput` 表示 Spec 字段、执行选项或运行时输入无效。`deploy.ErrSnapshotMismatch` 表示源码证据无效、计划版本不支持、保存的参数未规范化或摘要漂移。两者的打印消息均不包含调用方输入。业务配置读取和解析错误由应用负责。

Git 错误继续匹配 `gitsource.ErrSource`，并提供以下分类：

| 分类 | 含义 |
| --- | --- |
| `ErrInvalidInput` | 源码选项无效 |
| `ErrFetchFailed` | 引用或提交拉取失败，不区分网络、鉴权和引用不存在 |
| `ErrOutputLimit` | 控制器侧 Git 响应超过字节上限 |
| `ErrCommandFailed` | 控制器侧 Git 命令失败，保留观测到的退出码 |
| `ErrMaterializeFailed` | 目标检出失败，保留运行时错误身份与不确定清理 |

保留取消与超时身份。伴随不确定进程清理的传输失败仍可穿透源码适配器匹配 `deploy.ErrProcessUnknown`。不唯一标签、运行时不匹配和冲突由控制器核对，不能转为无条件重试。

Git 适配器丢弃 stderr，不从上游文本推断鉴权或临时重试属性。拉取失败可能永久存在。重试策略、退避、凭证刷新和新尝试由控制器负责。库的打印错误安全，但错误链中任意调用方或 provider 原因不保证可安全记录；只提取批准的分类和退出码，不直接输出 `Cause` 或递归打印错误链。
