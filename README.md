# capi-mock

本地模拟 CAPI 上游模型服务，使用 Go 标准库，无外部依赖、无需真实模型账号。

## 启动

```sh
cd ~/code/capi-mock
go run ./cmd/capi-mock
```

默认监听 `http://127.0.0.1:3221`，默认 API key 为 `mock-key`。在 CAPI 控制台添加 OpenAI-compatible channel：Base URL 填 `http://127.0.0.1:3221/v1`，密钥填 `mock-key`。可用模型：`mock-chat`、`mock-reasoning`、`mock-image`、`mock-video`、`typesafe-ai/jev`。

## 模拟接口

- `GET /v1/models`
- `POST /v1/chat/completions`（含 SSE 流式响应）
- `POST /v1/responses`
- `POST /v1/messages`（含基础 SSE）
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1/images/generations`、`/v1/images/edits`（返回仓库里的 `cmd/capi-mock/assets/deno.png`）
- `POST /v1/systemone`（处理 boolean/noul、string、number 问题）
- `POST /v1/videos`、`GET /v1/tasks/{id}`（内存任务，轮询后成功，返回样例视频路径）
- `GET /assets/deno.png`、`GET /assets/demo.mp4`（访问仓库内的样例媒体）
- `GET /healthz`（无需鉴权）

全部 `/v1` 接口需要 `Authorization: Bearer mock-key`。可通过 `CAPI_MOCK_ADDR`、`CAPI_MOCK_API_KEY`、`CAPI_MOCK_VIDEO_POLLS`、`CAPI_MOCK_CONFIDENCE`、`CAPI_MOCK_CHAT_TEXT` 配置监听地址、密钥、视频成功前的轮询次数、决策置信度和固定文本。请求带 `X-Mock-Status: 429`（或 400–599）可模拟上游错误。

样例文件很小：`cmd/capi-mock/assets/deno.png` 为 256×256 PNG，`cmd/capi-mock/assets/demo.mp4` 为 320×180、2 秒的 MP4。服务通过 Go embed 内嵌这两个文件，运行时不依赖额外路径；图片生成接口返回 PNG 的 Base64，视频任务完成后响应会带 `demo_url`。
