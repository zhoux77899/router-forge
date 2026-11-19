# Router Forge shared UI

Desktop 与本机只读 Web 共用的 React 19、Tailwind 4、shadcn 组件和结果视图。数据类型仅引用 `@router-forge/api` 的生成 DTO；没有复制 Go/OpenAPI 合同或注入控制 token。

## 入口

- `src\components\ui`：官方 shadcn CLI 生成并在本仓库维护的 primitives；`components.json` 声明 `new-york`、`neutral`、HugeIcons 和 workspace 别名，`tsconfig.json` 配套解析。
- `src\components\results-workspace.tsx`：只读分页、附着、详情、活跃运行轮询；Desktop 可以注入自己的明确运行操作，Web 不注入。
- `src\lib\read-client.ts`：冻结的结果 GET facade，不暴露 Network 管理、受保护配置、保存、创建、启动或取消方法。
- `src\lib\format.ts`：未知/partial usage、精确十进制货币、生命周期、奖励、基础设施异常及诊断隐藏。
- `src\components\page-frame.tsx`：固定主标题、纵向细 `ScrollArea` 和 `workspace` 容器宽度；不重建已挂载表单。
- `src\components\theme-selector.tsx`、`src\lib\appearance.ts`、`src\lib\use-appearance.ts`：三态主题 Tabs、系统监听、安全字体偏好及独立 CSS 字体栈；不调用原生 bridge。
- `src\components\app-icon.tsx`：使用原始 `assets\icon.svg` 派生的透明 `icon-mark.svg` mask，继承 `currentColor`，保留可访问名称但不显示应用文字；圆角发行 PNG 和系统 favicon 属于各 app。
- `src\styles.css`：共享 Neutral 明暗主题、紧凑布局、outline / border 键盘 focus 和 reduced-motion 行为；Desktop 系统字体默认栈与 Web-only FontSource family。

`ResultsWorkspace` 接收只读 client。Run 列表、冻结配置、attempt/task IDs、事件、实际 Agent 版本、请求表和 latency 取自真实后端；没有生产 fixtures。测试 fixture 文件只被测试导入。

## 界面约定

- Desktop 全部使用 React / shadcn 组件组合，明暗主题沿用默认 Neutral 黑白语义 token；不另加品牌配色。
- 图标使用 HugeIcons，侧栏显示/隐藏功能按钮例外使用官方 `@remixicon/react`；组件、浮层、focus、错误态都不添加 ring 或 shadow。保留可见 outline / border 与键盘、无障碍和 reduced-motion 支持。
- Desktop 侧栏使用 `offcanvas`，隐藏后无图标栏、占位或顶部侧栏背景/分隔线；隐藏导航为 inert。Web 使用 `icon`，收起时保留可交互图标栏。唯一顶部触发器用 `RiLayoutLeft2Line` 展开、`RiLayoutLeftLine` 收起。移动端依据 `openMobile`，而非恒为 collapsed 的桌面 `state`；保留快捷键、展开偏好和 `aria-expanded`。
- 用布局说明流程，去掉重复副标签、占位说明和统计描述。可访问名称、非字体错误、服务器身份 / MITM 风险和费用确认不可省略；字体失败仅静默回退默认，不显示说明、错误或重试按钮。
- 页面标题不参与正文滚动；共享 ScrollArea 仅展示细纵向滚动条。Radix viewport 的内部 `display: table` 包装在页面区域重置为 block，避免 max-content 撑宽。
- 按 `workspace` container 宽度切换表格与面板布局；窄屏保留每个带标签的字段，长 ID、模型名、精确费用和诊断可换行，不增加横向滚动或仅用 overflow-hidden 裁剪。
- 保存 Light/Dark/System 偏好而非 System 的解析结果。字体偏好只保存两个 family 名；字体枚举和原生窗口主题协调属于 Desktop，CDN 加载属于 Web HTML。
- `--app-font-sans` 用于界面，`--app-font-mono` 用于 code/pre/numeric；输入不得直接拼接为 CSS，使用共享校验与引用转义。按 preference kind 校验：界面接受已安装字体，代码只接受等宽字体；选择器与 CSS 应用使用同一规则。目录成功后旧代码选择的持久迁移属于 Desktop，不在加载/失败时执行。
- Network 编辑与原生控制只属于 Desktop。共享结果组件和 Web 只读取批准快照，不能获得私有控制 token 或代理 / 模型秘密。

## 冻结网络与证据

hash format 3 保存 source / build / agent / verifier / model 五个公共 profile 及独立 credential revisions；不含凭据值。profile 目标分别是精确 Environment 或 Desktop-only model，模型不继承执行 TLS 放宽。保存风险、连接检查、开始时执行 / 模型 TLS 确认与付费确认是独立动作。

冻结配置展示批准的策略，不把 CA 路径、保存 / 读回、profile revision 或一次 HEAD 检查当作逐连接 / trial 成功。旧报告缺失网络字段或实际观测显示未记录，不补造 Direct / Strict / Native 默认值；旧草稿需重新创建。

## 计量边界

未知不等于 0；per-request 的实际 0 才显示 0。完整 base usage 不要求 optional cache/reasoning 已报告。汇总 DTO 没有 optional counter 的观察数量，因此其 0 subtotal 保守显示未知；partial/optional 已知 subtotal 带已知标识。费用按输入的完整十进制字符串格式化，不做浮点或币种转换。reward=0、CLI exit=0 和 infra 完成分别展示，不合并为成功。

## 开发

本 workspace manifest 定义：

```powershell
bun run typecheck
bun run test
```

使用根 workspace 的 Bun packageManager 和已安装依赖。应用 Vite 负责构建这个源码包；它没有单独的发布 dist。修改组件后运行本包测试和受影响 app 的 typecheck/build。测试覆盖未知/partial、精确多币种成本、奖励/infra、GET-only facade、附着分页和不展示 raw prompt/output。
