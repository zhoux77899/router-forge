import type { Run } from "@router-forge/api";
import { protocolLabels } from "@router-forge/api";
import { Alert, AlertDescription, AlertTitle } from "./ui/alert";
import { Badge } from "./ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { hasAgentObservation, missingObservation, runtimeToolVersion, safeMessage, safeURL } from "../lib/format";

export function RunSnapshotDetails({ run }: { run: Run }) {
  const snapshot = run.snapshot;
  const { spec, model, provider, suite, environment, agent, config_hash, harbor_version, runtime } = snapshot;
  const legacy = snapshot.format_version !== 2;
  const missing = missingObservation(run.state);
  const preflightTools = snapshot.preflight_tools ?? [];
  const harborHint = preflightTools.find(tool => tool.name === "harbor");
  const uvHint = preflightTools.find(tool => tool.name === "uv");
  const preflightHarbor = harborHint?.version || harborHint?.version_output || (legacy || !runtime ? harbor_version : "");
  const preflightUV = uvHint?.version || uvHint?.version_output || (legacy || !runtime ? snapshot.uv_version : "");
  const resolvedTasks = snapshot.resolved_tasks ?? [];
  return <div className="flex flex-col gap-6">
    <p className="text-sm text-muted-foreground">运行快照保存草稿创建时的配置与实际解析来源。后续编辑环境或模型不会改写已批准的配置和来源 Hash；开始后单独追加实际工具与容器观测，工具升级不改变来源批准。密钥不包含在快照中；凭据引用不是密钥本身。</p>
    {legacy && <Alert variant={run.state === "draft" ? "destructive" : "default"}><AlertTitle>历史运行快照格式</AlertTitle><AlertDescription>{run.state === "draft" ? "此草稿不能启动；请重新创建草稿以解析当前来源。历史记录仍可读取或取消，不会自动迁移旧来源。" : "此历史报告保留原样可读；未记录的来源或独立观测字段不会补造。"}</AlertDescription></Alert>}
    <section className="flex flex-col gap-3"><h3 className="font-semibold">已批准配置与执行边界</h3><dl className="data-grid">
      <div><dt>已批准配置 Hash</dt><dd><code className="text-xs">{config_hash || "未记录"}</code></dd></div>
      <div><dt>已批准来源 Hash</dt><dd><code className="text-xs">{snapshot.resolution_hash || "未记录"}</code></dd></div>
      <div><dt>路由模式</dt><dd>固定模型 · 不自动切换</dd></div>
      <div><dt>唯一任务</dt><dd>{spec.task_names.join(", ")}</dd></div>
      <div><dt>并发 / 尝试 / Trial 重试</dt><dd>{spec.concurrency} / {spec.attempts} / 0</dd></div>
      <div><dt>最多请求 / 每响应输出上限</dt><dd>{spec.max_requests} / {spec.max_output_tokens} Token</dd></div>
      <div><dt>执行 / Setup 超时</dt><dd>{spec.timeout_sec} s / {spec.setup_timeout_sec} s</dd></div>
    </dl></section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">草稿已解析的唯一任务来源</h3><dl className="data-grid">
      <div><dt>Suite / 完整数据集名</dt><dd>{suite.name}<code className="block text-xs">{suite.dataset || "未记录"}</code></dd></div>
      <div><dt>数据集版本 / Revision</dt><dd>{suite.version || "未提供"} / {snapshot.dataset_revision || "未提供"}</dd></div>
      <div><dt>注册表类型 / URL</dt><dd>{snapshot.registry_kind || "未记录"}<code className="block text-xs">{suite.registry_url ? safeURL(suite.registry_url) : "未记录"}</code></dd></div>
      <div><dt>注册表元数据 URL</dt><dd>{snapshot.registry_metadata_url ? safeURL(snapshot.registry_metadata_url) : "未提供"}</dd></div>
      <div><dt>数据集 Package Digest</dt><dd><code className="text-xs">{snapshot.dataset_digest || "未提供"}</code></dd></div>
      <div><dt>来源解析时间（resolved_at）</dt><dd>{snapshot.resolved_at ? <time dateTime={snapshot.resolved_at}>{snapshot.resolved_at}</time> : "未记录"}</dd></div>
      <div><dt>数据集实际任务数 / 本次选中</dt><dd>{suite.task_count} / {spec.task_names.length}</dd></div>
    </dl>
      {resolvedTasks.map(task => <dl key={task.name} className="data-grid rounded-md border p-4">
        <div><dt>完整、精确的任务名</dt><dd>{task.name}</dd></div>
        {task.kind === "package" || task.package_name ? <>
          <div><dt>任务 Package 名称</dt><dd>{task.package_name || "未记录"}</dd></div>
          <div><dt>任务 Package Digest / Ref</dt><dd><code className="text-xs">{task.ref || "未记录"}</code></dd></div>
          <div><dt>任务 Revision</dt><dd>{task.revision || "未提供"}</dd></div>
          <div><dt>Git Commit</dt><dd>未提供；使用 Package Digest</dd></div>
        </> : <>
          <div><dt>任务 Git Commit</dt><dd><code className="text-xs">{task.git_commit || "未记录"}</code></dd></div>
          <div><dt>任务 Git 仓库</dt><dd>{task.git_url ? safeURL(task.git_url) : "未记录"}</dd></div>
          <div><dt>仓库内任务路径</dt><dd><code className="text-xs">{task.path || "未记录"}</code></dd></div>
        </>}
      </dl>)}
      {!resolvedTasks.length && <dl className="data-grid"><div><dt>选中任务来源</dt><dd>未记录</dd></div><div><dt>历史任务 Git Commit</dt><dd><code className="text-xs">{suite.task_commit || "未记录"}</code></dd></div><div><dt>历史任务仓库</dt><dd>{suite.task_repository ? safeURL(suite.task_repository) : "未记录"}</dd></div></dl>}
      <p className="text-xs text-muted-foreground">开始使用此草稿保存的 Git SHA 或 Package Digest，不使用目录预览缓存，也不会替换为同名但不同命名空间的任务。</p>
    </section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">模型服务</h3><dl className="data-grid">
      <div><dt>服务商 / Provider ID</dt><dd>{provider.name}<code className="block text-xs text-muted-foreground">{provider.id}</code></dd></div>
      <div><dt>服务地址</dt><dd>{safeURL(provider.base_url)}</dd></div>
      <div><dt>上游模型 / Model ID</dt><dd>{model.name}<code className="block text-xs text-muted-foreground">{model.id}</code></dd></div>
      <div><dt>原生协议</dt><dd><Badge variant="outline">{protocolLabels[model.protocol]}</Badge></dd></div>
      <div><dt>凭据状态 / 不可变引用</dt><dd>{provider.has_key ? "已配置（写入后不可读取）" : "未配置"}<code className="block text-xs text-muted-foreground">{provider.key_revision || "引用未记录"}</code></dd></div>
      <div><dt>价格来源</dt><dd>{model.pricing ? `${model.pricing.currency} · ${model.pricing.source}` : "未配置 · 成本可能未知"}</dd></div>
      {model.pricing && <><div><dt>每百万输入 / 输出</dt><dd className="numeric">{model.pricing.input_per_million} / {model.pricing.output_per_million}</dd></div><div><dt>每百万缓存读取 / 写入</dt><dd className="numeric">{model.pricing.cache_read_per_million ?? "未知"} / {model.pricing.cache_write_per_million ?? "未知"}</dd></div></>}
    </dl></section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">环境与 Harbor 容器安装策略</h3><dl className="data-grid">
      <div><dt>执行环境 / ID</dt><dd>{environment.name} · {environment.type}<code className="block text-xs text-muted-foreground">{environment.id}</code></dd></div>
      <div><dt>配置目标 / 工作目录</dt><dd>{environment.host || environment.distribution || "本机"} · {environment.work_dir || "由后端解析"}</dd></div>
      <div><dt>Agent</dt><dd>{spec.agent}</dd></div>
      <div><dt>容器安装策略</dt><dd>{legacy ? agent.container_install || "历史安装方式未记录" : "Harbor 默认容器安装；不请求精确版本"}</dd></div>
      {spec.agent_version && <div><dt>历史请求的 Agent 版本（非实际观测）</dt><dd>{spec.agent_version}</dd></div>}
      <div><dt>宿主版本提示（不等于容器版本）</dt><dd>{agent.version || "宿主未报告版本"}</dd></div>
      <div><dt>独立容器版本命令</dt><dd><code className="text-xs">{agent.command ? `${agent.command} --version` : "未记录"}</code></dd></div>
    </dl>{Object.keys(spec.agent_kwargs ?? {}).length > 0 && <div><p className="mb-2 text-xs text-muted-foreground">Agent 标量参数</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(JSON.stringify(spec.agent_kwargs, null, 2))}</pre></div>}</section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">{legacy ? "历史工具版本记录" : "草稿预检工具（仅提示）"}</h3><dl className="data-grid">
      <div><dt>Harbor · {legacy ? "历史记录" : "草稿预检"}</dt><dd>{safeMessage(preflightHarbor || "未记录")}</dd></div>
      <div><dt>uv · {legacy ? "历史记录" : "草稿预检"}</dt><dd>{safeMessage(preflightUV || "未记录")}</dd></div>
    </dl><p className="text-xs text-muted-foreground">草稿预检不会限制开始时安装的版本。工具可能在草稿之后升级；下方实际执行观测与预检提示独立展示。</p>
      {preflightTools.length > 0 && <Table><TableHeader><TableRow><TableHead>预检工具</TableHead><TableHead>探测 / 版本</TableHead><TableHead>原始 --version 输出</TableHead></TableRow></TableHeader><TableBody>{preflightTools.map(tool => <TableRow key={tool.name}><TableCell>{tool.name}<code className="block text-xs">{safeMessage(tool.path || "路径未记录")}</code></TableCell><TableCell>{tool.available ? "探测成功" : "不可用"} · {tool.version || (tool.version_output ? "未提取数值版本" : "未记录")}</TableCell><TableCell className="max-w-96 whitespace-normal"><pre className="whitespace-pre-wrap break-words text-xs">{safeMessage(tool.version_output || "未记录")}</pre></TableCell></TableRow>)}</TableBody></Table>}
    </section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">开始后的实际执行观测（非批准配置）</h3><dl className="data-grid">
      <div><dt>实际 Harbor 版本 / 输出</dt><dd>{runtimeToolVersion(runtime, "harbor", missing)}</dd></div>
      <div><dt>实际 uv 版本 / 输出</dt><dd>{runtimeToolVersion(runtime, "uv", missing)}</dd></div>
      <div><dt>工具观测时间</dt><dd>{runtime?.observed_at || missing}</dd></div>
      <div><dt>实际工作目录 / Docker Gateway</dt><dd>{safeMessage(runtime?.work_dir || missing)} / {safeMessage(runtime?.docker_gateway || missing)}</dd></div>
      <div><dt>实际 Registry URL</dt><dd>{runtime?.registry_url ? safeURL(runtime.registry_url) : missing}</dd></div>
      <div><dt>实际任务名 / 路径</dt><dd>{safeMessage(runtime?.task_name || missing)}<code className="block text-xs">{safeMessage(runtime?.task_path || missing)}</code></dd></div>
    </dl>
      {(runtime?.tools ?? []).length > 0 && <Table><TableHeader><TableRow><TableHead>实际工具</TableHead><TableHead>探测 / 版本</TableHead><TableHead>原始 --version 输出</TableHead></TableRow></TableHeader><TableBody>{runtime?.tools.map(tool => <TableRow key={tool.name}><TableCell>{tool.name}<code className="block text-xs">{safeMessage(tool.path || "路径未记录")}</code></TableCell><TableCell>{tool.available ? "探测成功" : "不可用"} · {tool.version || (tool.version_output ? "未提取数值版本" : "未记录")}</TableCell><TableCell className="max-w-96 whitespace-normal"><pre className="whitespace-pre-wrap break-words text-xs">{safeMessage(tool.version_output || "未记录")}</pre>{tool.detail && <p className="text-xs text-muted-foreground">{safeMessage(tool.detail)}</p>}</TableCell></TableRow>)}</TableBody></Table>}
    </section>
    <section className="flex flex-col gap-3"><h3 className="font-semibold">独立容器 Agent 观测（推理前必须完成）</h3><dl className="data-grid">
      <div><dt>实际容器 Agent CLI 版本</dt><dd>{safeMessage(runtime?.agent_version || missing)}</dd></div>
      <div><dt>容器 Agent 观测时间</dt><dd>{runtime?.agent_observed_at || missing}</dd></div>
      <div><dt>独立观测确认</dt><dd>{hasAgentObservation(runtime) ? "独立容器 CLI 版本已观测并保存" : `${missing} · 未完成独立版本观测确认`}</dd></div>
    </dl>{runtime?.agent_version_output && <div><p className="mb-2 text-xs text-muted-foreground">容器 CLI 原始 --version 输出</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(runtime.agent_version_output)}</pre></div>}
      <p className="text-xs text-muted-foreground">必须成功独立观测实际容器 CLI 版本并落盘确认后才允许推理；宿主版本、未知版本或安装声明不能替代。后续评分失败或取消不抹去已经记录的版本。</p>
    </section>
  </div>;
}
