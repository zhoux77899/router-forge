# Router Forge Web

本机结果页面，使用共享 React/shadcn 视图展示真实持久化运行、冻结配置、奖励与基础设施状态、事件、请求延迟、可用 token 和按币种精确费用。没有环境/模型管理、密钥输入、草稿、启动或取消控制；不依赖或导入原生 Desktop service。

## 本地开发

先打开 Desktop，读取标题区域显示的实际 loopback 后端 URL。从仓库根目录执行已定义的根脚本：

```powershell
$env:ROUTER_FORGE_API_URL = 'http://127.0.0.1:<Desktop 显示的实际端口>'
bun run dev:web
```

Web 开发服务固定绑定 `127.0.0.1:5173`。`ROUTER_FORGE_API_URL` 仅供 Vite server 的 `/api` proxy 使用；不能是远程 URL、带用户名/密码或额外路径的 URL。端口已被占用时失败，不静默换端口。未配置目标时 `/api` 返回带说明的 503，UI 不会用 mock/fixture 数据冒充结果。

不需要、也不能在 Web 的环境变量或构建内容中注入控制 token、API key。后端由 Desktop 持有和管理，网页仅通过冻结的只读 API facade 发 GET 请求。

## 发布与预览

```powershell
# 仓库根目录已定义的脚本
bun run build:web
```

Vite 输出本目录 `dist\index.html` 与 `dist\assets`，使用相对 asset 路径、不生成 sourcemap。实际生产运行由独立本机 backend 同源提供静态 Web 和只读 `/api`；浏览器使用 `new APIClient("")`，不需要生产端口环境变量。Desktop 的 MyGo build 将此 Web dist 打包为 executable 旁的 `dist` 资源。

本 app manifest 定义 `bun run preview`，固定 `127.0.0.1:4173`；预览需要上述 `ROUTER_FORGE_API_URL`，它不是独立后端或配置控制台。后台 PID 和 URL 由 Desktop 展示。

## 展示约定

- 无数据展示空态，不显示假任务、伪计数或示例运行。
- 运行按 ID 选择和分页；打开页面定位活跃持久化运行，包括第一页之外的记录。只读轮询不会创建、启动、重试或取消任务。
- 单独展示 Harbor reward、执行生命周期、异常与实际容器 Agent 版本；reward=0 或 CLI exit=0 不表示解题成功。
- null/missing 的计数显示未知。partial 使用已知部分，optional cache/reasoning 未报告时不捏造 0。
- 费用保持十进制字符串精度，按币种和来源展示，不转换汇率、不用浮点数重新计费。
- 不展示模型原始 prompt/output，配置只显示公共引用；诊断文本另作防御性凭据隐藏。

## 测试

在本目录使用 manifest 中定义的 `bun run typecheck`、`bun run test`、`bun run build`。`src\readonly.test.tsx` 验证没有控制/native 导入和 UI 操作；API facade 的 GET-only、精确费用、未知 token、选择及结果组件测试在 `packages\ui`。测试不需要真实服务凭据或模型调用。
