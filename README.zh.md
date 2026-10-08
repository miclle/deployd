# deployd

[English](README.md)

deployd 是用于已创建运行环境的 Go 部署执行内核：准备不可变源码快照，严格校验 YAML 配置，执行安装、前台服务启动和 HTTP 就绪检查。

导入包名为 `deploy`，模块路径为 `github.com/miclle/deployd`。要求 Go 1.25 或更新版本，采用 [MIT 协议](LICENSE)。

## 职责

调用方负责认证、资源创建、任务调度、持久化、重试决策、配额、到期和资源销毁。库负责源码与配置证据及部署执行协议。仓库命令属于可信可执行代码，必须在适当隔离的环境中运行。

## 配置

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

参阅[配置协议](docs/configuration.zh.md)、[架构](docs/architecture.zh.md)和[测试](docs/testing.zh.md)。

## 开发

安装与当前 Go 工具链兼容的 golangci-lint 2.13 或更新版本。

```sh
make check
make coverage
```

CI 在 Go 1.25、1.26 上运行 race 测试，lint 固定为 2.14.0。
