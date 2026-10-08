# deployd

[English](README.md)

deployd 是一个面向已有运行环境的 Go 部署执行内核：解析固定 Git 提交，严格校验部署 YAML，执行安装或构建脚本，启动前台服务，并等待 HTTP 就绪。

导入包名为 `deploy`，模块为 `github.com/miclle/deployd`。要求 Go 1.25 或更新版本；内置本地运行时支持 Linux 和 macOS。采用 [MIT 协议](LICENSE)。

## 使用

```sh
go get github.com/miclle/deployd
```

调用方选择配置路径，通常为 `deploy.yaml`：

```yaml
version: 1
workingDirectory: .
installCommand: npm ci
startCommand: npm run start -- --host 0.0.0.0 --port 3000
port: 3000
healthcheck:
  path: /health
  timeoutSeconds: 60
```

执行流程如下：

```go
source, err := gitsource.New(repositoryURL, gitsource.Options{Ref: "main"})
// 检查 err 后再继续。
plan, err := deploy.Prepare(ctx, source, "deploy.yaml")
// 检查 err 后保存 plan.Snapshot() 和 plan.ConfigBytes()。
// 提供已创建的 Runtime。
result, err := deploy.Apply(ctx, source, runtime, plan, deploy.Options{
    WorkRoot: "/srv/deployments",
    OperationID: operationID, // 在该运行时内使用新的唯一标识。
})
// 即使失败也保留部分 result；检查 StageError.Cleanup。
// 就绪后取消 ctx 不会停止服务。
err = deploy.Stop(cleanupCtx, runtime, result.Process)
```

清理时使用有期限的 context，并处理所有错误。参考[可编译的完整 Go 示例](example_test.go)、[执行架构](docs/architecture.zh.md)、[配置协议](docs/configuration.zh.md)和[适配器](docs/adapters.zh.md)。Git、本地进程、envd Process 适配器分别位于独立包中；调用方也可以实现 `Source` 和 `Runtime`。

## 职责

应用负责鉴权、资源创建、任务调度、持久化、并发控制、重试决策、配额、到期、入口和资源销毁。库负责不可变源码与配置证据及执行协议。停止部署只终止应用进程，保留工作目录和运行环境。脚本以目标账号执行受信任的仓库代码，应按代码的可信程度选择隔离方式。

每次尝试使用新工作目录和唯一操作标识，不自动重试脚本。启动或探测失败会尝试有时限的进程清理，并返回包含不确定启动信息的部分结果。就绪证据是进程存活时获得的一次 HTTP 2xx 响应，不代表持续健康。输出回调提供有界、脱敏的阶段输出；持续应用日志由集成方负责。

## 开发

安装 golangci-lint 2.13 或更新版本，其编译所用 Go 版本必须与当前工具链兼容。集成测试需要 Git 和[适配器文档](docs/adapters.zh.md)中的 POSIX 工具。

```sh
make check
make coverage
```

`make coverage` 运行 race 测试并检查语句覆盖率：核心包至少 95%，每个适配器至少 90%。CI 在 Go 1.25、1.26 和 1.27 上执行同一门槛，固定 golangci-lint 2.14.0。[测试文档](docs/testing.zh.md)说明覆盖范围及本地集成与真实远端验收的区别。
