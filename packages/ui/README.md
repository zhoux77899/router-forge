# Router Forge shared UI

Desktop 与本机只读 Web 共用的 React 19、Tailwind 4、shadcn 组件和结果视图。数据类型仅引用 `@router-forge/api` 的生成 DTO；没有复制 Go/OpenAPI 合同或注入控制 token。

## 入口

- `src\components\ui`：由官方 shadcn CLI 从 `new-york-v4` registry 生成的 Button、Input、Select、Card、Table、Tabs、Checkbox、Alert/Dialog 等 primitives，别名在 `components.json` 与 `tsconfig.json` 中声明。
- `src\components\results-workspace.tsx`：只读分页、附着、详情、活跃运行轮询；Desktop 可以注入自己的明确运行操作，Web 不注入。
- `src\lib\read-client.ts`：冻结的结果 GET facade，不暴露保存、创建、启动或取消方法。
- `src\lib\format.ts`：未知/partial usage、精确十进制货币、生命周期、奖励、基础设施异常及诊断隐藏。
- `src\styles.css`：共享主题、紧凑布局、键盘 focus 和 reduced-motion 行为。

`ResultsWorkspace` 接收只读 client。Run 列表、冻结配置、attempt/task IDs、事件、实际 Agent 版本、请求表和 latency 取自真实后端；没有生产 fixtures。测试 fixture 文件只被测试导入。

## 计量边界

未知不等于 0；per-request 的实际 0 才显示 0。完整 base usage 不要求 optional cache/reasoning 已报告。汇总 DTO 没有 optional counter 的观察数量，因此其 0 subtotal 保守显示未知；partial/optional 已知 subtotal 带已知标识。费用按输入的完整十进制字符串格式化，不做浮点或币种转换。reward=0、CLI exit=0 和 infra 完成分别展示，不合并为成功。

## 开发

本 workspace manifest 定义：

```powershell
bun run typecheck
bun run test
```

使用根 workspace 的 Bun packageManager 和已安装依赖。应用 Vite 负责构建这个源码包；它没有单独的发布 dist。修改组件后运行本包测试和受影响 app 的 typecheck/build。测试覆盖未知/partial、精确多币种成本、奖励/infra、GET-only facade、附着分页和不展示 raw prompt/output。
