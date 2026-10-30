import { useState } from "react";
import type { NetworkMirrors, NetworkProfile, NetworkSnapshot, Run } from "@router-forge/api";
import { protocolLabels } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { AlertCircleIcon, ArrowDown01Icon } from "@hugeicons/core-free-icons";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@router-forge/ui/components/ui/accordion";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { HarborTLSWarning } from "./feedback";
import { hasAgentObservation, missingObservation, runtimeToolVersion, safeMessage, safeURL } from "../lib/format";

const networkStages = [
  ["source", "来源下载"], ["build", "原始构建"], ["agent", "Agent"], ["verifier", "Verifier"], ["model", "模型服务"],
] as const satisfies readonly (readonly [keyof NetworkSnapshot, string])[];
const mirrorSources = [
  ["apt", "APT"], ["pypi", "PyPI"], ["npm", "npm"], ["node", "Node 下载"], ["uv", "uv 索引"], ["uv_python", "uv Python 下载"],
] as const satisfies readonly (readonly [keyof NetworkMirrors, string])[];

export function hasProfiledNetwork(run: Run) {
  return (run.snapshot.format_version ?? 0) >= 3;
}

export function legacyHarborTLSLabel(run: Run) {
  const skip = run.snapshot.environment.skip_tls_verification;
  return skip === true ? "跳过 Harbor TLS 验证（历史已保存）" : skip === false ? "未跳过 Harbor TLS 验证（历史已保存）" : "未记录（不推定默认策略）";
}

function networkURL(value?: string) {
  if (!value) return "未记录";
  try {
    const url = new URL(value);
    url.username = "";
    url.password = "";
    const sensitive = /user(?:name)?|pass(?:word)?|key|token|auth|credential/i;
    for (const key of new Set(url.searchParams.keys())) {
      if (sensitive.test(key)) url.searchParams.set(key, "[已隐藏]");
    }
    const fragment = new URLSearchParams(url.hash.slice(1));
    let redactedFragment = false;
    for (const key of new Set(fragment.keys())) {
      if (sensitive.test(key)) { fragment.set(key, "[已隐藏]"); redactedFragment = true; }
    }
    if (redactedFragment) url.hash = fragment.toString();
    return safeURL(url.toString());
  } catch {
    return "地址格式无效（已隐藏）";
  }
}

function networkText(value?: string) {
  return safeMessage(value ?? "未记录")
    .replace(/\bhttps?:\/\/[^\s"'<>]+/gi, url => networkURL(url))
    .replace(/((?:proxy[_-]?)?username["']?\s*[:=]\s*["']?)[^\s,"'}]+/gi, "$1[已隐藏]");
}

export function RunNetworkWarnings({ run }: { run: Run }) {
  if (!hasProfiledNetwork(run)) return <>
    {run.snapshot.environment.skip_tls_verification === true && <HarborTLSWarning />}
    <Alert variant={run.state === "draft" ? "destructive" : "default"}>
      <AlertTitle>{(run.snapshot.format_version ?? 0) < 2 ? "历史运行快照格式" : "历史网络未显式配置"}</AlertTitle>
      <AlertDescription>未显式配置分阶段网络；不推定代理、CA、TLS 或镜像策略。{run.state === "draft" ? "此草稿不能启动，请重新创建草稿。" : "保留历史记录，不补造来源或连接验证证据。"}</AlertDescription>
    </Alert>
  </>;
  const network = run.snapshot.network;
  const missingStages = networkStages.filter(([stage]) => !network?.[stage]);
  const executionSkip = networkStages.filter(([stage]) => (stage === "source" || stage === "agent") && network?.[stage]?.tls_mode === "skip");
  const boundarySkip = networkStages.filter(([stage]) => (stage === "build" || stage === "verifier") && network?.[stage]?.tls_mode === "skip");
  return <>
    {missingStages.length > 0 && <Alert variant={run.state === "draft" ? "destructive" : "default"}>
      <AlertTitle>{network ? "网络快照不完整" : "网络快照未记录"}</AlertTitle>
      <AlertDescription>{missingStages.map(([, label]) => label).join("、")}未记录；不从旧环境字段或当前配置补造。{run.state === "draft" && "请重新创建草稿。"}</AlertDescription>
    </Alert>}
    {executionSkip.length > 0 && <Alert variant="destructive">
      <HugeiconsIcon icon={AlertCircleIcon} aria-hidden="true" />
      <AlertTitle>执行侧 TLS 跳过配置（不安全）</AlertTitle>
      <AlertDescription>{executionSkip.map(([, label]) => label).join("、")}不验证服务器身份；MITM 可能替换元数据、下载内容或安装脚本。此选择不改变模型侧策略，也不放宽原始 Dockerfile 构建或 Verifier 校验。</AlertDescription>
    </Alert>}
    {network?.model?.tls_mode === "skip" && <Alert variant="destructive">
      <HugeiconsIcon icon={AlertCircleIcon} aria-hidden="true" />
      <AlertTitle>模型侧 TLS 跳过配置（不安全）</AlertTitle>
      <AlertDescription>模型服务 HTTPS 不验证服务器身份；MITM 可能读取凭据与请求内容或篡改响应。模型侧风险与执行侧独立，不代表原始构建或 Verifier 可跳过校验。</AlertDescription>
    </Alert>}
    {boundarySkip.length > 0 && <Alert variant="destructive">
      <AlertTitle>快照网络边界冲突</AlertTitle>
      <AlertDescription>{boundarySkip.map(([, label]) => label).join("、")}记录了跳过 TLS；原始 Dockerfile 构建与 Verifier 不允许放宽校验。保留快照原值，不据此推定配置可执行。</AlertDescription>
    </Alert>}
  </>;
}

function NetworkProfileDetails({ profile, phase }: { profile: NetworkProfile; phase: string }) {
  return <div className="flex flex-col gap-3">
      <dl className="data-grid">
        <div><dt>配置 ID</dt><dd><code>{networkText(profile.id)}</code></dd></div>
        <div><dt>配置版本</dt><dd><code>{networkText(profile.revision)}</code></dd></div>
        {profile.environment_id !== undefined && <div><dt>环境 ID</dt><dd><code>{networkText(profile.environment_id)}</code></dd></div>}
        <div><dt>创建时间</dt><dd>{networkText(profile.created_at)}</dd></div>
        <div><dt>代理地址</dt><dd>{networkURL(profile.proxy_url)}</dd></div>
        <div><dt>代理目标</dt><dd>{profile.proxy_targets ? profile.proxy_targets.length ? profile.proxy_targets.map(target => networkText(target)).join(", ") : "空列表" : "未记录"}</dd></div>
        <div><dt>NO_PROXY</dt><dd>{profile.no_proxy === "" ? "空" : networkText(profile.no_proxy)}</dd></div>
        <div><dt>代理认证方式</dt><dd>{networkText(profile.proxy_auth)}</dd></div>
        <div><dt>代理中继策略</dt><dd>{networkText(profile.relay_mode)}</dd></div>
        <div><dt>保存的代理凭据状态</dt><dd>{profile.has_credentials === true ? "已配置（不回显）" : profile.has_credentials === false ? "未配置" : "未记录"}</dd></div>
        <div><dt>代理凭据版本</dt><dd><code>{networkText(profile.credential_revision)}</code></dd></div>
        <div><dt>已保存 CA 路径</dt><dd>{networkText(profile.ca_cert_path)}</dd></div>
        <div><dt>此配置版本的风险确认</dt><dd>{profile.tls_risk_accepted === true ? "保存时已确认" : profile.tls_risk_accepted === false ? "保存时未确认" : "未记录"}</dd></div>
      </dl>
      <Table aria-label={`${phase}的镜像与下载源`}>
        <TableHeader><TableRow><TableHead>来源</TableHead><TableHead>已选策略</TableHead><TableHead>已保存地址 / Sources</TableHead></TableRow></TableHeader>
        <TableBody>{mirrorSources.map(([key, label]) => {
          const source = profile.mirrors?.[key];
          return <TableRow key={key}>
            <TableCell>{label}</TableCell>
            <TableCell>{source?.mode === "native" ? "原生源（显式选择）" : source?.mode === "custom" ? "自定义" : "未记录"}</TableCell>
            <TableCell className="max-w-96 whitespace-normal break-words">{source?.url && <code className="block">{networkURL(source.url)}</code>}{source?.sources && <pre className="whitespace-pre-wrap">{networkText(source.sources)}</pre>}{!source?.url && !source?.sources && "未记录"}</TableCell>
          </TableRow>;
        })}</TableBody>
      </Table>
  </div>;
}

export function FrozenNetworkDetails({ run }: { run: Run }) {
  const network = hasProfiledNetwork(run) ? run.snapshot.network : undefined;
  return <section aria-label="冻结网络配置" className="flex flex-col gap-3">
    <Table aria-label="分阶段冻结网络配置">
      <TableHeader><TableRow><TableHead>阶段</TableHead><TableHead>配置 / 宿主</TableHead><TableHead>代理</TableHead><TableHead>TLS / 信任</TableHead><TableHead>详情</TableHead></TableRow></TableHeader>
      {networkStages.map(([stage, label]) => {
        const profile = network?.[stage];
        if (!profile) return <TableBody key={stage}><TableRow data-network-stage={stage}><TableCell>{label}</TableCell><TableCell colSpan={4}>未记录（不推定默认策略）</TableCell></TableRow></TableBody>;
        return <Collapsible key={stage} asChild><TableBody>
          <TableRow data-network-stage={stage}>
            <TableCell className="align-top">{label}</TableCell>
            <TableCell className="align-top whitespace-normal">{networkText(profile.name)}<span className="block text-xs text-muted-foreground">{profile.scope === "desktop" ? "桌面宿主" : profile.scope === "environment" ? `执行环境 · ${networkText(profile.environment_id)}` : "宿主未记录"}</span></TableCell>
            <TableCell className="align-top whitespace-normal">{profile.proxy_mode === "direct" ? "直连" : profile.proxy_mode === "proxy" ? <>代理<code className="block">{networkURL(profile.proxy_url)}</code></> : "未记录"}</TableCell>
            <TableCell className="align-top"><Badge variant={profile.tls_mode === "skip" ? "destructive" : "outline"}>{profile.tls_mode === "verify" ? "验证 TLS" : profile.tls_mode === "skip" ? "跳过 TLS 验证" : "未记录"}</Badge><span className="block text-xs text-muted-foreground">{profile.trust_mode === "system" ? "系统信任库" : profile.trust_mode === "custom" ? "自定义 CA" : "信任策略未记录"}</span></TableCell>
            <TableCell className="align-top"><CollapsibleTrigger asChild><Button size="sm" variant="ghost" className="group" aria-label={`查看${label}的冻结网络配置`}>
              详情<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
            </Button></CollapsibleTrigger></TableCell>
          </TableRow>
          <CollapsibleContent forceMount asChild className="data-[state=closed]:hidden"><TableRow><TableCell colSpan={5} className="whitespace-normal"><NetworkProfileDetails profile={profile} phase={label} /></TableCell></TableRow></CollapsibleContent>
        </TableBody></Collapsible>;
      })}
    </Table>
    {network && <>
      <p className="text-xs text-muted-foreground">配置与凭据版本纳入批准 Hash；选择与风险确认不是实际 TLS 连接验证结果，也不代表每个 Trial 通过。仅显示公开字段，不含代理用户名或密码。</p>
      <p className="text-xs text-muted-foreground">执行边界：模型与来源 / Agent 的跳过风险独立；原始 Dockerfile 构建与 Verifier 不放宽校验，也不改变 SSH 主机校验或 Docker daemon 镜像拉取。</p>
    </>}
  </section>;
}

export function RunSnapshotDetails({ run }: { run: Run }) {
  const [openSections, setOpenSections] = useState(["configuration"]);
  const snapshot = run.snapshot;
  const { spec, model, provider, suite, environment, agent, config_hash, harbor_version, runtime } = snapshot;
  const legacy = (snapshot.format_version ?? 0) < 2;
  const profiledNetwork = hasProfiledNetwork(run);
  const missing = missingObservation(run.state);
  const preflightTools = snapshot.preflight_tools ?? [];
  const harborHint = preflightTools.find(tool => tool.name === "harbor");
  const uvHint = preflightTools.find(tool => tool.name === "uv");
  const preflightHarbor = harborHint?.version || harborHint?.version_output || (legacy || !runtime ? harbor_version : "");
  const preflightUV = uvHint?.version || uvHint?.version_output || (legacy || !runtime ? snapshot.uv_version : "");
  const resolvedTasks = snapshot.resolved_tasks ?? [];
  return <div className="flex flex-col gap-6">
    <RunNetworkWarnings run={run} />
    <Accordion type="multiple" value={openSections} onValueChange={setOpenSections}>
    <AccordionItem value="configuration"><AccordionTrigger>已批准配置与执行边界</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("configuration")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>运行 ID</dt><dd><code className="text-xs">{run.id}</code></dd></div>
      <div><dt>已批准配置 Hash</dt><dd><code className="text-xs">{config_hash || "未记录"}</code></dd></div>
      <div><dt>已批准来源 Hash</dt><dd><code className="text-xs">{snapshot.resolution_hash || "未记录"}</code></dd></div>
      <div><dt>路由模式</dt><dd>固定模型 · 不自动切换</dd></div>
      <div><dt>唯一任务</dt><dd>{spec.task_names.join(", ")}</dd></div>
      <div><dt>并发 / 尝试 / Trial 重试</dt><dd>{spec.concurrency} / {spec.attempts} / 0</dd></div>
      <div><dt>最多请求 / 每响应输出上限</dt><dd>{spec.max_requests} / {spec.max_output_tokens} Token</dd></div>
      <div><dt>执行 / Setup 超时</dt><dd>{spec.timeout_sec} s / {spec.setup_timeout_sec} s</dd></div>
    </dl><p className="text-xs text-muted-foreground">批准配置与来源 Hash 不随实际工具观测变化；凭据引用不含密钥。</p></section></AccordionContent></AccordionItem>
    <AccordionItem value="source"><AccordionTrigger>草稿已解析的唯一任务来源</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("source")}><section className="flex flex-col gap-3"><dl className="data-grid">
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
      <p className="text-xs text-muted-foreground">按快照保存的 Git SHA 或 Package Digest 执行；精确任务名不替换。</p>
    </section></AccordionContent></AccordionItem>
    <AccordionItem value="model"><AccordionTrigger>模型服务</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("model")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>服务商 / Provider ID</dt><dd>{provider.name}<code className="block text-xs text-muted-foreground">{provider.id}</code></dd></div>
      <div><dt>服务地址</dt><dd>{safeURL(provider.base_url)}</dd></div>
      <div><dt>上游模型 / Model ID</dt><dd>{model.name}<code className="block text-xs text-muted-foreground">{model.id}</code></dd></div>
      <div><dt>原生协议</dt><dd><Badge variant="outline">{protocolLabels[model.protocol]}</Badge></dd></div>
      <div><dt>凭据状态 / 不可变引用</dt><dd>{provider.has_key ? "已配置（写入后不可读取）" : "未配置"}<code className="block text-xs text-muted-foreground">{provider.key_revision || "引用未记录"}</code></dd></div>
      <div><dt>价格来源</dt><dd>{model.pricing ? `${model.pricing.currency} · ${model.pricing.source}` : "未配置 · 成本可能未知"}</dd></div>
      {model.pricing && <><div><dt>每百万输入 / 输出</dt><dd className="numeric">{model.pricing.input_per_million} / {model.pricing.output_per_million}</dd></div><div><dt>每百万缓存读取 / 写入</dt><dd className="numeric">{model.pricing.cache_read_per_million ?? "未知"} / {model.pricing.cache_write_per_million ?? "未知"}</dd></div></>}
    </dl></section></AccordionContent></AccordionItem>
    <AccordionItem value="network"><AccordionTrigger>冻结网络</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("network")}><FrozenNetworkDetails run={run} /></AccordionContent></AccordionItem>
    <AccordionItem value="environment"><AccordionTrigger>环境与 Harbor 容器安装策略</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("environment")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>执行环境 / ID</dt><dd>{environment.name} · {environment.type}<code className="block text-xs text-muted-foreground">{environment.id}</code></dd></div>
      <div><dt>配置目标 / 工作目录</dt><dd>{safeMessage(environment.host || environment.distribution || "本机")} · {safeMessage(environment.work_dir || "由后端解析")}</dd></div>
      {!profiledNetwork && <>
        <div><dt>历史 Harbor TLS 设置</dt><dd>{legacyHarborTLSLabel(run)}</dd></div>
        <div><dt>历史 CA 证书路径</dt><dd>{safeMessage(environment.ca_cert_path || "未记录")}</dd></div>
      </>}
      <div><dt>Agent</dt><dd>{spec.agent}</dd></div>
      <div><dt>容器安装策略</dt><dd>{legacy ? agent.container_install || "历史安装方式未记录" : "Harbor 默认容器安装；不请求精确版本"}</dd></div>
      {spec.agent_version && <div><dt>历史请求的 Agent 版本（非实际观测）</dt><dd>{safeMessage(spec.agent_version)}</dd></div>}
      <div><dt>宿主版本提示（不等于容器版本）</dt><dd>{safeMessage(agent.version || "宿主未报告版本")}</dd></div>
      <div><dt>独立容器版本命令</dt><dd><code className="text-xs">{safeMessage(agent.command ? `${agent.command} --version` : "未记录")}</code></dd></div>
    </dl>{!profiledNetwork && <p className="text-xs text-muted-foreground">历史字段保留原值，不是实际 TLS 连接验证结果；未记录值不推定默认策略。</p>}{Object.keys(spec.agent_kwargs ?? {}).length > 0 && <div><p className="mb-2 text-xs text-muted-foreground">Agent 标量参数</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(JSON.stringify(spec.agent_kwargs, null, 2))}</pre></div>}</section></AccordionContent></AccordionItem>
    <AccordionItem value="preflight"><AccordionTrigger>{legacy ? "历史工具版本记录" : "草稿预检工具（仅提示）"}</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("preflight")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>Harbor · {legacy ? "历史记录" : "草稿预检"}</dt><dd>{safeMessage(preflightHarbor || "未记录")}</dd></div>
      <div><dt>uv · {legacy ? "历史记录" : "草稿预检"}</dt><dd>{safeMessage(preflightUV || "未记录")}</dd></div>
    </dl><p className="text-xs text-muted-foreground">预检仅提示，不限制开始时安装的版本；实际观测独立记录。</p>
      {preflightTools.length > 0 && <Table aria-label="草稿预检工具"><TableHeader><TableRow><TableHead>预检工具</TableHead><TableHead>探测 / 版本</TableHead><TableHead>原始 --version 输出</TableHead></TableRow></TableHeader><TableBody>{preflightTools.map(tool => <TableRow key={tool.name}><TableCell>{safeMessage(tool.name)}<code className="block text-xs">{safeMessage(tool.path || "路径未记录")}</code></TableCell><TableCell>{tool.available ? "探测成功" : "不可用"} · {safeMessage(tool.version || (tool.version_output ? "未提取数值版本" : "未记录"))}</TableCell><TableCell className="max-w-96 whitespace-normal"><pre className="whitespace-pre-wrap break-words text-xs">{safeMessage(tool.version_output || "未记录")}</pre></TableCell></TableRow>)}</TableBody></Table>}
    </section></AccordionContent></AccordionItem>
    <AccordionItem value="runtime"><AccordionTrigger>开始后的实际执行观测（非批准配置）</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("runtime")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>实际 Harbor 版本 / 输出</dt><dd>{runtimeToolVersion(runtime, "harbor", missing)}</dd></div>
      <div><dt>实际 uv 版本 / 输出</dt><dd>{runtimeToolVersion(runtime, "uv", missing)}</dd></div>
      <div><dt>工具观测时间</dt><dd>{runtime?.observed_at || missing}</dd></div>
      <div><dt>实际工作目录 / Docker Gateway</dt><dd>{safeMessage(runtime?.work_dir || missing)} / {safeMessage(runtime?.docker_gateway || missing)}</dd></div>
      <div><dt>实际 Registry URL</dt><dd>{runtime?.registry_url ? safeURL(runtime.registry_url) : missing}</dd></div>
      <div><dt>实际任务名 / 路径</dt><dd>{safeMessage(runtime?.task_name || missing)}<code className="block text-xs">{safeMessage(runtime?.task_path || missing)}</code></dd></div>
    </dl>
      {(runtime?.tools ?? []).length > 0 && <Table aria-label="实际工具观测"><TableHeader><TableRow><TableHead>实际工具</TableHead><TableHead>探测 / 版本</TableHead><TableHead>原始 --version 输出</TableHead></TableRow></TableHeader><TableBody>{runtime?.tools.map(tool => <TableRow key={tool.name}><TableCell>{safeMessage(tool.name)}<code className="block text-xs">{safeMessage(tool.path || "路径未记录")}</code></TableCell><TableCell>{tool.available ? "探测成功" : "不可用"} · {safeMessage(tool.version || (tool.version_output ? "未提取数值版本" : "未记录"))}</TableCell><TableCell className="max-w-96 whitespace-normal"><pre className="whitespace-pre-wrap break-words text-xs">{safeMessage(tool.version_output || "未记录")}</pre>{tool.detail && <p className="text-xs text-muted-foreground">{safeMessage(tool.detail)}</p>}</TableCell></TableRow>)}</TableBody></Table>}
    </section></AccordionContent></AccordionItem>
    <AccordionItem value="agent"><AccordionTrigger>独立容器 Agent 观测（推理前必须完成）</AccordionTrigger><AccordionContent forceMount hidden={!openSections.includes("agent")}><section className="flex flex-col gap-3"><dl className="data-grid">
      <div><dt>实际容器 Agent CLI 版本</dt><dd>{safeMessage(runtime?.agent_version || missing)}</dd></div>
      <div><dt>容器 Agent 观测时间</dt><dd>{runtime?.agent_observed_at || missing}</dd></div>
      <div><dt>独立观测确认</dt><dd>{hasAgentObservation(runtime) ? "独立容器 CLI 版本已观测并保存" : `${missing} · 未完成独立版本观测确认`}</dd></div>
    </dl>{runtime?.agent_version_output && <div><p className="mb-2 text-xs text-muted-foreground">容器 CLI 原始 --version 输出</p><pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{safeMessage(runtime.agent_version_output)}</pre></div>}
      <p className="text-xs text-muted-foreground">独立容器 CLI 版本观测必须保存后才允许推理；宿主版本不能替代。后续失败或取消不抹去已经记录的版本。</p>
    </section></AccordionContent></AccordionItem>
    </Accordion>
  </div>;
}
