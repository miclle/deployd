# 真实 envd 验收

[English](envd-acceptance.md)

这些可选测试以指定用户在**已有隔离 Linux 目标**上执行可信命令，不创建、续期或销毁 Sandbox 或主机。使用没有并发部署 Worker 的可丢弃目标。清理失败会使测试失败；不得忽略或复用操作标签。

## 配置

在当前 shell 或秘密管理器中配置环境变量。不要把令牌放进测试参数、提交到仓库，或在报告中输出环境变量全集。

| 变量 | 含义 |
| --- | --- |
| `DEPLOYD_ENVD_ACCEPTANCE=1` | 显式启用，否则两项测试跳过 |
| `DEPLOYD_ENVD_BASE_URL` | 已有 agent 的 HTTPS origin |
| `DEPLOYD_ENVD_RUNTIME_ID` | 当前 agent 实例的稳定身份，重建适配器后保持一致 |
| `DEPLOYD_ENVD_ACCESS_TOKEN` | 临时 agent 令牌 |
| `DEPLOYD_ENVD_USER` | 可选 POSIX 用户，默认 `user` |
| `DEPLOYD_ENVD_READINESS_ORIGIN` | 对应夹具配置端口、不含凭证的精确 HTTP(S) origin |
| `DEPLOYD_ENVD_REPOSITORY` | 部署测试使用的可信公开 HTTPS 夹具仓库 |
| `DEPLOYD_ENVD_COMMIT` | 部署测试使用的完整固定提交 |
| `DEPLOYD_ENVD_WORK_ROOT` | 部署测试使用的专用绝对路径 |
| `DEPLOYD_ENVD_CONFIG_PATH` | 可选仓库配置路径，默认 `deploy.yaml` |

显式启用后缺少配置会失败，不会悄悄跳过。部署夹具须绑定配置端口，并在健康路径返回 2xx。公开夹具不验证私有源码凭证。

```sh
go test -race -count=1 -timeout 2m ./runtime/envd -run '^TestLiveEnvdAcceptance$' -v
go test -race -count=1 -timeout 25m ./runtime/envd -run '^TestLiveEnvdDeployment$' -v
```

进程测试验证工具与权限、有限命令及其子进程的取消清理、流与请求关闭后的独立存活、适配器重建、只有标签的核对、错误标签拒绝与停止后确认，仅删除自己随机命名的 `/tmp/deployd-acceptance-*` 目录。部署测试验证固定 Git 检出、配置校验、安装、前台启动、路由后的 HTTP 就绪和重建适配器后的停止。部署工作区保留供诊断，其保留与清理由调用方负责。

## 证据与限制

记录测试日期、agent 版本、用户、源码提交、退出状态和清理结果，不包含秘密或仓库输出。默认 CI 验证本地及协议合同，并编译这些测试；跳过真实测试**不代表远程验收通过**。

令牌刷新、私有源码授权、PID 响应丢失的传输故障注入、下游原子 fencing、控制器进程崩溃和公网流量切换仍需 provider 或控制器单独验收。实时日志使用独立合同。

进程测试还会附着实时日志，检查字节预算，并确认观察结束后工作负载仍存活。
