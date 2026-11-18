# ai-gateway

[English](README.md)

一个使用 Go 编写的轻量级配置驱动 HTTP 反向代理。它根据 URL 路径中的标签，将请求转发到预先配置的上游服务，不解析业务请求内容。

## 功能

- 通过 `/opencode-go/openai/v1/chat/completions` 这样的路径标签选择上游。
- 透明转发 HTTP 方法、路径、查询参数、请求体、响应状态码及端到端请求头。
- 支持 SSE 等流式响应，不缓存完整响应后再返回。
- 从服务端配置或环境变量注入上游请求头和凭证。
- 使用同一种路由方式连接托管服务和自建服务。
- 提供请求体大小、并发数、连接超时和响应头超时限制。
- 支持优雅关闭。
- 默认只监听本机回环地址；监听其他地址必须显式启用，并配置共享访问密钥。
- 不包含用户账户、数据库、Prompt 处理、模型解析或协议转换。

## 路由方式

为上游配置一个标签和基础 URL：

```yaml
upstreams:
  opencode-go:
    base_url: https://opencode.ai/inference/go
    headers:
      Authorization: "Bearer ${OPENCODE_GO_API_KEY}"

  local-qwen:
    base_url: http://127.0.0.1:8000
```

请求：

```text
http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions?stream=true
```

会被转发到：

```text
https://opencode.ai/inference/go/openai/v1/chat/completions?stream=true
```

Gateway 会移除路由标签，将上游 `base_url` 的路径与剩余请求路径拼接，并保留查询参数和请求体。

同一个上游也可以访问模型列表：

```text
http://127.0.0.1:8787/opencode-go/v1/models
```

OpenCode Go 的推理请求要求提供 `X-Opencode-Session` 请求头。每个会话应使用稳定的 Session ID；Gateway 会原样转发该请求头。

## 快速开始

需要 Go 1.25 或更高版本。

1. 复制示例配置：

   ```sh
   cp configs/config.example.yaml config.yaml
   ```

2. 设置托管上游所需的密钥：

   ```sh
   export OPENCODE_GO_API_KEY="your-api-key"
   ```

3. 启动 Gateway：

   ```sh
   go run ./cmd/ai-gateway -config config.yaml
   ```

4. 检查健康状态：

   ```sh
   curl http://127.0.0.1:8787/healthz
   ```

5. 查看 OpenCode Go 可用模型：

   ```sh
   curl http://127.0.0.1:8787/opencode-go/v1/models
   ```

调用模型时，使用 OpenAI 兼容路由，并填写模型列表返回的 Model ID：

```sh
curl http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Opencode-Session: ses_replace-with-a-stable-session-id" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Say hello"}]}'
```

Gateway 本身不实现模型 API。调用方负责按照目标上游的协议构造请求并解释响应。

## 架构

见[架构说明](docs/architecture.md)，了解本机与可信内网部署方式、请求流转、共享上游凭证及明确不包含的功能。架构图使用 Mermaid 编写，源码直接保存在该文档中。

## 配置

完整配置说明见[配置文档](docs/configuration.md)，包括环境变量展开、路由规则和远程监听要求。

示例配置包含：
- opencode-go：通过服务端 Authorization 请求头连接 OpenCode Go。
- local-qwen：不需要 Gateway 管理 API Key 的自建 HTTP 服务。

## 安全

默认监听地址为 `127.0.0.1:8787`。如需监听非回环地址，必须设置 `server.allow_remote: true`，并配置 `server.access_token_env`。对应环境变量必须包含非空共享密钥。客户端通过 `X-Gateway-Token` 发送该密钥；Gateway 会在转发前移除这个请求头。

如需供其他机器访问，应使用可信网络边界，并在需要 HTTPS 时配置可信的 TLS 反向代理。不要将未认证的实例直接暴露到公网。详见 [SECURITY.md](SECURITY.md)。

`Proxy-Authorization` 请求头始终会被移除。如果上游配置了 `Authorization`，它会替换调用方提供的 `Authorization`；如果没有配置，则调用方的 `Authorization` 会原样转发。请求头配置值支持 `${ENV_VAR}` 展开；缺少或为空的环境变量会导致启动失败。

## 开发

```sh
go test ./...
go vet ./...
go build ./cmd/ai-gateway
```

开发约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 范围与限制

- 上游目标只能来自静态配置，请求不能传入任意目标 URL。
- Gateway 不解析或修改 JSON、Prompt、模型名称或 Provider 专有请求内容。
- Gateway 不重试请求、不在多个实例间负载均衡，也不转换协议。
- 默认不启用 CORS。浏览器集成应使用同源接入，或配置可信的前置代理。
- 内置 HTTP 服务不负责 TLS 终止。需要 HTTPS 时，请使用可信反向代理。

## 发布

推送符合 `v*` 格式的版本 Tag 后，会自动构建 Linux、Windows、macOS 的 AMD64 和 ARM64 发布包。详见[发布说明](docs/releasing.md)。

## 许可证

MIT，详见 [LICENSE](LICENSE)。
