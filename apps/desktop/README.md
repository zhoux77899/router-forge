# Router Forge Desktop

MyGo v0.7.1 原生桌面是 Router Forge 的唯一配置、创建草稿、付费确认、启动和取消入口。React 19、TanStack Query、Vite 与共享 shadcn 组件提供评测、结果及设置视图；设置统一管理外观、执行环境、模型、网络和诊断。界面使用默认 Neutral 黑白明暗主题、HugeIcons 与 outline / border focus，不使用 ring / shadow；让布局解释流程，保留必要的无障碍名称、错误、风险与费用确认。普通浏览器打开此 renderer 时只显示原生桥接不可用提示，不回退到浏览器控制请求。

## 导航与窗口

主导航仅保留评测和结果；设置与侧栏展开/折叠位于底部。运行任务数显示在评测项，折叠时仍可见。宽屏记住展开偏好，窄屏保留图标栏并覆盖展开，选择页面后关闭覆盖层；Ctrl/Cmd+B 和顶栏按钮均可切换侧栏。

MyGo 隐藏标题栏模式由 App 绘制 40px 顶栏，保留系统窗口控件和对应平台占位。顶栏只包含侧栏按钮与拖动区域；Windows/Linux 不显示窗口菜单栏，macOS 保留系统应用菜单。App 自绘文字统一 `text-xs`，不缩小图标、控件高度或点击热区。

设置及评测表单首次访问后保留在会话内，切换页面或设置分类不丢失未保存输入；不自动保存、不将表单或凭据写入浏览器存储，刷新不恢复草稿。后端 URL、版本/PID、重新附着及打开只读 Web 位于 **设置 → 诊断**。Web 仅有结果和外观设置，不提供原生控件或配置写入。

## 开发

从仓库根目录执行已定义的根脚本：

```powershell
bun run dev:desktop
```

Desktop 的 `dev` 脚本先运行 `prepare:helpers`（根 `build:backend`、`build:web`），完成后再运行 `mygo dev`，由 MyGo 启动 `http://127.0.0.1:7789` 的 Vite renderer。准备构建不占用 MyGo 等待前端服务的 30 秒启动窗口；请使用上述脚本，不要直接运行 `mygo dev` 来代替完整开发启动。Desktop renderer 预览端口为 `7791`；开发与预览端口固定且不能占用。无需设置控制 token 或后端 URL；生成的原生绑定调用 `Console.Connect`，后端使用独立、动态 loopback 端口。

开发与发布均依赖现有 Windows WebView2 Runtime、Bun、Go 自动 toolchain 和固定的 `mygo-cli` 0.7.1。这里不安装系统服务、Docker 或宿主 Agent。第一版 SWE 执行不支持 Windows 原生 Harbor；使用真实检查通过的 WSL/Linux SSH 环境。工具 `harbor`、`docker`、`uv` 的路径必须属于所选执行宿主。

## 原生发布

从仓库根目录：

```powershell
bun run build:desktop
```

根脚本先构建只读 Web 和真实 backend/workers，再运行本 app 的 `mygo build`。Windows amd64 输出：

```text
build\desktop\windows-amd64\
  Router Forge.exe
  router-forge-setup-0.1.0-amd64.exe
  server\router-forge-server.exe
  workers\router-forge-worker-windows-amd64.exe
  workers\router-forge-worker-linux-amd64
  workers\router-forge-worker-linux-arm64
  dist\index.html
  dist\assets\...
```

所有编译产物均位于仓库根 `build`：`server`、`workers` 是 helpers，`dist` 是只读 Web，`renderer` 是 Desktop 前端，`desktop` 是各平台原生包。MyGo 将根 `build\server`、`build\workers`、`build\dist` 按目录 basename 拷贝为 `server`、`workers`、`dist`。Desktop renderer 自身嵌入 executable；包内的 `dist` 是只读 Web，不是 Desktop renderer。便携运行必须保留 executable 旁的三个资源目录。默认只构建当前宿主平台；跨平台 app 必须配套该平台的 server helper。

`Console` 通过 MyGo 支持的 `App.Path(PathResources)` 定位发布资源，开发回退到根 runtime 的 `ResolvePaths`。窗口关闭不会停止独立后端。重开后按持久化运行状态附着和轮询，不自动启动草稿。

## 数据目录与隔离检查

```powershell
& '.\build\desktop\windows-amd64\Router Forge.exe' --data-dir "$env:LOCALAPPDATA\RouterForge-Test"
```

`--data-dir` 必须为绝对路径，直接传给 backend Options，并将 MyGo renderer cache/logs 放在该目录下。优先级：显式 CLI 参数、既有 `ROUTER_FORGE_DATA_DIR`、runtime 默认目录。无 CLI 参数时保留既有环境变量行为。根 runtime 支持 `ROUTER_FORGE_SERVER`；显式指定 server 时配套解析 `ROUTER_FORGE_WORKERS`、`ROUTER_FORGE_WEB`。通常均不设置，保留开发/打包资源发现。

原生验收应使用独立目录，不污染真实配置。自动化可创建带 `--data-dir` 参数的 `.lnk`，由后台 Computer Use launcher 打开，不需要进程环境变量。关闭两次测试窗口后，检查同一公开后端 PID 仍响应 GET，再只清理自己创建的进程。不要运行真实模型，除非另行批准费用和边界。

## Network 配置

**设置 → 网络** 页面先选择目标：Desktop 模型流量，或一个精确的已保存 Windows / WSL / SSH Environment。首次使用没有任何网络模式、地址、CA、TLS 或来源预选；Direct、System、Strict、Native 都必须显式选择。不从宿主环境变量或旧 Environment 字段推断、继承策略。

| 配置 | 显式选择 |
| --- | --- |
| 连接 | Direct / Proxy；代理协议、地址、认证方式与 `no_proxy`。URL 不含用户名或密码。 |
| CA 与 TLS | 目标宿主的 System / Custom CA；Strict / Skip。保存 Skip 需单独接受服务器身份 / MITM 风险。 |
| 来源 | `apt`、`pypi`、`npm`、`node`、`uv`、`uv_python` 各自选择 Native / Custom，不预填镜像地址。 |
| 代理凭据 | Retain / Replace / Clear；用户名、密码仅写入，不读回完整值。配置 revision 与 credential revision 独立。 |

Environment 精确绑定 **source / build / agent / verifier** 四个同宿主 profile；Provider 单独绑定 **Desktop-only model** profile。模型 HTTPS 使用自己的配置，不继承执行侧 Skip。旧环境、Provider 和历史仍可读取；没有绑定表示未配置，没有历史网络观测表示未记录，不能补成默认严格策略。旧冻结草稿必须重新创建。

Docker gateway 必须显式选择 **detect / manual**，manual 需填写地址。daemon proxy / mirror 仅作只读观测；不重配或重启 daemon，不为网络检查拉取镜像。原始任务镜像可用性与受控进程的网络检查是不同边界。

- **Build** 仅支持 Strict、System CA、Native 来源与无认证代理 build args；拒绝 Custom CA、镜像、Skip 和认证 relay，不改写原始 Dockerfile。
- **Verifier** 保持 Strict，与 Agent 独立，不继承 Agent 的 TLS 放宽。不修改原始 `test.sh`、solution 或 verifier 语义。
- 配置不改动 SSH known-host 校验、系统信任库、daemon / image-pull TLS 或宿主构建工具。Skip 不能解决代理 407 或 Docker IPAM 冲突。

检查必须选择已保存的 profile 并输入明确、无凭据的目标 URL；只发送 HEAD，不跟随重定向，不使用模型 API key、不推理、不自动猜测目的地址。保存、读取配置、填写 CA 路径或一次检查通过，都不等于所有连接或一次 trial 成功。

草稿的 **hash format 3** 冻结五个公共 profile 及各自 credential revision，不包含私有凭据。后续修改不会改写草稿。保存 Skip 风险、检查 profile / 目标、开始时确认执行 / 模型 TLS 风险、确认付费是独立动作：开始必须提交快照的精确 `config_hash`、`confirm_paid=true`，以及适用的 `confirm_execution_tls_risk` / `confirm_model_tls_risk`。确认执行侧风险不会代替模型侧确认，勾选付费也不会代替任何 TLS 确认。

## 操作与隐私

- 保存环境、绑定 Network 并实际检查，再从该环境发现的 Agent 中选择。uv/Harbor 只检查 `--version` 是否成功，版本仅作记录；仍需通过原生配置、来源解析与协议能力检查。宿主 `Installed=false` 不否定 Harbor 的原生容器安装。
- 使用 Harbor 默认容器安装策略，不填写或请求精确 Agent 版本。独立容器 CLI 探测成功且实际版本持久化回执成功前，不能转发推理；宿主版本提示及 Harbor 自报值都不替代该观测。
- 仅支持 `swe-bench/swe-bench-verified`，由已安装 Harbor 原生默认路由解析当前数据集版本/摘要与任务 SHA/摘要；不固定 raw registry URL，也不回退旧来源或缓存。目录预览不代表已保存草稿。
- 一次只选一个完整具名任务；固定模型、并发 1、尝试 1、trial 重试 0。草稿保存精确来源，开始不重新解析浮动数据集；工具升级允许继续并记录开始时实际版本。
- 确认面板展示已保存任务来源、限制、安装策略、五个冻结网络 profile / revision、各 TLS 风险与费用。结果中的批准策略不是独立连接观测；奖励、生命周期、基础设施异常分别展示。未知 token / 费用不当作 0。
- 模型 key 仅写入；空白替换保留已有 key，已有 key 只显示 hint / revision。提交前清空敏感输入，不放入 query mutation cache、日志或浏览器 storage。
- 模型 key 和代理凭据保存在本机受限权限的明文 SQLite，不是加密保险库。代理 Clear 会清空旧 credential revisions 的秘密值并使引用失效，不是 SQLite / WAL / 备份的密码学擦除，也不撤回已在内存中的凭据；需要停止使用时另行取消活动运行。
- 原生控制 token 留在 Go 内存，不放入公开连接对象或浏览器 bundle。model profile 的代理秘密不下发 worker；公共结果只有配置元数据和 revision 引用，不含私有凭据。
- 原生 Request 只接受受限 GET/POST `/api/` 路径，无任意 URL/文件代理；不走系统 HTTP proxy、不跟随重定向，并限制 body 大小和防御性隐藏凭据。只读 Web 只有结果 GET facade，不提供 Network 管理、受保护配置或控制能力。
- 不展示或保存模型原始 prompt / output；Harbor 私有产物仍可能含敏感内容，不能作为公共结果或截图发布。

## 修改与验证

本次 UI 重构已在实际 Windows 原生包验证：无窗口菜单栏、最大化/最小化/恢复正常状态、后端仍响应、40px 拖动区域、138px 右侧原生控件占位，以及正文/标题 12px。浏览器验收覆盖宽窄屏导航、明暗主题、只读 Web、会话草稿保留及不写入浏览器存储。Win32 状态检查不等同于鼠标交互验收：拖动、双击、边缘缩放、Snap Layouts 和关闭按钮仍需手动检查；macOS 信号灯/系统菜单/全屏及 Linux 装饰行为需在对应系统验收。

app manifest 已定义以下命令（在本目录执行；Bun 来自根 workspace 的 packageManager）：

```powershell
bun run generate
bun run typecheck
bun run test
bun run test:native
bun run build:renderer
```

`generate` 调用真正的 `mygo generate`，输出 `src\mygo.generated.ts`，不要手写 RPC 或编辑生成绑定。HTTP DTO 仅从 `@router-forge/api` 导入，根 `generate:contracts` 负责 OpenAPI/TS 生成。共享结果、未知值、货币精度和只读测试位于 `packages\ui`；本 app 测试覆盖空白首次配置、Network 表单与绑定、Agent/任务选择、仅写入凭据及独立 TLS / 付费确认，不发送模型请求。

原生 bundle fixture 已迁移到显式 Network 保存 / 读回和精确 Environment / Provider 绑定，检查初始空白。实际打包的 worker 已通过 loopback HEAD 验收：严格 TLS 拒绝、自定义 CA 与加载指纹、独立确认的跳过校验（忽略但保留无效 CA），以及恢复严格校验；不调用 provider，也不证明完整 trial 成功。Harbor format 3 trial fixture 也已迁移，但迁移后尚未运行；它要求显式 Windows 绝对路径 JSON 配置、Docker gateway 模式和独立的开始时 TLS 确认，具体参数见 [CONTRIBUTING](../../CONTRIBUTING.md#optional-real-harbor-trial-with-a-synthetic-provider)。之前完整真实 trial 的 `worker connection lost`、provider 请求 0、无 Agent CLI 回执和 verifier 完成证据不变；synthetic fixture 的付费确认不代表真实模型调用。
