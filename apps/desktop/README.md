# Router Forge Desktop

MyGo v0.7.1 原生桌面是 Router Forge 的唯一配置、创建草稿、付费确认、启动和取消入口。React 19、TanStack Query、Vite 与共享 shadcn 组件提供执行环境、固定模型、单任务和结果视图。普通浏览器打开此 renderer 时只显示原生桥接不可用提示，不回退到浏览器控制请求。

## 开发

从仓库根目录执行已定义的根脚本：

```powershell
bun run dev:desktop
```

`mygo dev` 自动运行 app 的 `prepare:helpers`（根 `build:backend`、`build:web`），再启动 `http://127.0.0.1:5174` 的 Vite renderer。端口固定且不能占用。无需设置控制 token 或后端 URL；生成的原生绑定调用 `Console.Connect`，后端使用独立、动态 loopback 端口。

开发与发布均依赖现有 Windows WebView2 Runtime、Bun、Go 自动 toolchain 和固定的 `mygo-cli` 0.7.1。这里不安装系统服务、Docker 或宿主 Agent。第一版 SWE 执行不支持 Windows 原生 Harbor；使用真实检查通过的 WSL/Linux SSH 环境。工具 `harbor`、`docker`、`uv` 的路径必须属于所选执行宿主。

## 原生发布

从仓库根目录：

```powershell
bun run build:desktop
```

根脚本先构建只读 Web 和真实 backend/workers，再运行本 app 的 `mygo build`。Windows amd64 输出：

```text
apps\desktop\build\windows-amd64\
  Router Forge.exe
  router-forge-setup-0.1.0-amd64.exe
  server\router-forge-server.exe
  workers\router-forge-worker-windows-amd64.exe
  workers\router-forge-worker-linux-amd64
  workers\router-forge-worker-linux-arm64
  dist\index.html
  dist\assets\...
```

MyGo 将根 `build\server`、根 `build\workers`、`apps\web\dist` 按目录 basename 拷贝为 `server`、`workers`、`dist`。Desktop renderer 自身嵌入 executable；这里的 `dist` 是只读 Web，不是 Desktop renderer。便携运行必须保留 executable 旁的三个资源目录。默认只构建当前宿主平台；跨平台 app 必须配套该平台的 server helper。

`Console` 通过 MyGo 支持的 `App.Path(PathResources)` 定位发布资源，开发回退到根 runtime 的 `ResolvePaths`。窗口关闭不会停止独立后端。重开后按持久化运行状态附着和轮询，不自动启动草稿。

## 数据目录与隔离检查

```powershell
& '.\apps\desktop\build\windows-amd64\Router Forge.exe' --data-dir 'D:\Codes\MaaS\router-forge\apps\desktop\.mygo\isolated-profile'
```

`--data-dir` 必须为绝对路径，直接传给 backend Options，并将 MyGo renderer cache/logs 放在该目录下。优先级：显式 CLI 参数、既有 `ROUTER_FORGE_DATA_DIR`、runtime 默认目录。无 CLI 参数时保留既有环境变量行为。根 runtime 支持 `ROUTER_FORGE_SERVER`；显式指定 server 时配套解析 `ROUTER_FORGE_WORKERS`、`ROUTER_FORGE_WEB`。通常均不设置，保留开发/打包资源发现。

原生验收应使用独立目录，不污染真实配置。自动化可创建带 `--data-dir` 参数的 `.lnk`，由后台 Computer Use launcher 打开，不需要进程环境变量。关闭两次测试窗口后，检查同一公开后端 PID 仍响应 GET，再只清理自己创建的进程。不要运行真实模型，除非另行批准费用和边界。

## 操作与隐私

- 先保存环境并实际检查，再从该环境发现的 Agent 中选择。uv/Harbor 只检查 `--version` 是否成功，版本仅作记录；仍需通过原生配置、来源解析与协议能力检查。宿主 `Installed=false` 不否定 Harbor 的原生容器安装。
- 使用 Harbor 默认容器安装策略，不填写或请求精确 Agent 版本。独立容器 CLI 探测成功且实际版本持久化回执成功前，不能转发推理；宿主版本提示及 Harbor 自报值都不替代该观测。
- 仅支持 `swe-bench/swe-bench-verified`，由已安装 Harbor 原生默认路由解析当前数据集版本/摘要与任务 SHA/摘要；不固定 raw registry URL，也不回退旧来源或缓存。目录预览不代表已保存草稿。
- 一次只选一个完整具名任务；固定模型、并发 1、尝试 1、trial 重试 0。创建草稿保存本次精确来源，开始时不重新解析浮动数据集。草稿后工具升级允许继续，并记录开始时实际版本。
- 独立确认面板列出保存的任务来源、`max_requests`、单响应 `max_output_tokens`、执行/Setup 超时、默认容器安装和费用风险。勾选明确确认后才发送 `confirm_paid=true`。旧冻结草稿必须重新创建，旧报告仍可读且缺失观测明确显示未记录。
- 密钥仅写入。空白替换保留已有凭据；提交前清空输入，不放入 query mutation cache、日志或浏览器 storage。已有 key 只显示 hint/revision，不显示完整值。
- 宿主 SQLite 中密钥是明文、权限保护而非加密。原生控制 token 留在 Go 内存，不放入公开连接对象或浏览器 bundle。
- 原生 Request 只接受受限 GET/POST `/api/` 路径，无任意 URL/文件代理；不走系统 HTTP proxy、不跟随重定向，并限制 body 大小和防御性隐藏凭据。
- 结果独立展示生命周期、奖励和基础设施异常。未知 token/费用不当作 0；不展示或保存模型原始 prompt/output。

## 修改与验证

app manifest 已定义以下命令（在本目录执行；Bun 来自根 workspace 的 packageManager）：

```powershell
bun run generate
bun run typecheck
bun run test
bun run test:native
bun run build:renderer
```

`generate` 调用真正的 `mygo generate`，输出 `src\mygo.generated.ts`，不要手写 RPC 或编辑生成绑定。HTTP DTO 仅从 `@router-forge/api` 导入，根 `generate:contracts` 负责 OpenAPI/TS 生成。共享结果、未知值、货币精度和只读测试位于 `packages\ui`；本 app 测试覆盖表单、Agent/任务选择、写入 key 的保留边界及显式付费确认，不发送模型请求。
