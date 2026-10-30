# Router Forge Web

本机结果页面，使用共享 React/shadcn 视图展示真实持久化运行、冻结配置、奖励与基础设施状态、事件、请求延迟、可用 token 和按币种精确费用。没有环境/模型管理、密钥输入、草稿、启动或取消控制；不依赖或导入原生 Desktop service。

## 本地开发

先打开 Desktop，读取标题区域显示的实际 loopback 后端 URL。从仓库根目录执行已定义的根脚本：

```powershell
$env:ROUTER_FORGE_API_URL = 'http://127.0.0.1:<Desktop 显示的实际端口>'
bun run dev:web
```

Web 开发服务固定绑定 `127.0.0.1:7790`。`ROUTER_FORGE_API_URL` 仅供 Vite server 的 `/api` proxy 使用；不能是远程 URL、带用户名/密码或额外路径的 URL。端口已被占用时失败，不静默换端口。未配置目标时 `/api` 返回带说明的 503，UI 不会用 mock/fixture 数据冒充结果。

不需要、也不能在 Web 的环境变量或构建内容中注入控制 token、模型 API key 或代理凭据。后端由 Desktop 持有和管理；网页仅通过冻结的结果 GET-only facade 读取结果与批准的公共快照，不能读取 Network 管理或其他受保护配置。原生私有 API 不因 Web 同源发布而公开。

## 发布与预览

```powershell
# 仓库根目录已定义的脚本
bun run build:web
```

Vite 输出仓库根目录 `build\dist\index.html` 与 `build\dist\assets`，使用相对 asset 路径、不生成 sourcemap；重建只清空该专属输出目录。实际生产运行由独立本机 backend 同源提供静态 Web 和只读 `/api`；浏览器使用 `new APIClient("")`，不需要生产端口环境变量。Desktop 的 MyGo build 将此 Web dist 打包为 executable 旁的 `dist` 资源。

本 app manifest 定义 `bun run preview`，固定 `127.0.0.1:7792`；预览需要上述 `ROUTER_FORGE_API_URL`，它不是独立后端或配置控制台。后台 PID 和 URL 由 Desktop 展示。

## 展示约定

- 使用共享 shadcn Neutral 黑白明暗主题与 HugeIcons；以边框 / outline 保留键盘 focus，不使用 ring / shadow。布局说明流程，不堆叠重复副标签或描述。
- 无数据展示空态，不显示假任务、伪计数或示例运行。
- 运行按 ID 选择和分页；打开页面定位活跃持久化运行，包括第一页之外的记录。只读轮询不会创建、启动、重试或取消任务。
- hash format 3 的快照展示批准的 source / build / agent / verifier / model 公共 profile 与独立 credential revision，不显示私有代理凭据。配置 revision 不等于连接检查或 trial 成功；模型和执行 TLS 风险分别展示，不互相继承。
- 旧报告仍可读；缺失网络配置或运行观测显示未记录，不自动解释为 Direct、Strict 或其他默认策略。
- 单独展示 Harbor reward、执行生命周期、异常与实际容器 Agent 版本；reward=0 或 CLI exit=0 不表示解题成功。
- null/missing 的计数显示未知。partial 使用已知部分，optional cache/reasoning 未报告时不捏造 0。
- 费用保持十进制字符串精度，按币种和来源展示，不转换汇率、不用浮点数重新计费。
- 不展示模型原始 prompt/output，配置只显示批准的公共快照；诊断文本另作防御性凭据隐藏。

## 测试

在本目录使用 manifest 中定义的 `bun run typecheck`、`bun run test`、`bun run build`。`src\readonly.test.tsx` 验证没有控制/native 导入和 UI 操作；API facade 的 GET-only、精确费用、未知 token、选择及结果组件测试在 `packages\ui`。测试不需要真实服务凭据或模型调用。
