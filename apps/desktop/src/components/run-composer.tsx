import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Inspection, type Run } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { Add01Icon, ArrowDown01Icon, Search01Icon } from "@hugeicons/core-free-icons";
import { ErrorNotice, LoadingState, errorMessage, safeMessage, safeURL } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Empty, EmptyHeader, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { FieldDescription, FieldGroup, FieldLegend, FieldSet } from "@router-forge/ui/components/ui/field";
import { Separator } from "@router-forge/ui/components/ui/separator";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Textarea } from "@router-forge/ui/components/ui/textarea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Field, TextField } from "./fields";
import { agentReasons, blankRun, environmentNetworkErrors, validateRun, type FormErrors, type RunFields } from "../lib/forms";

export function RunComposer({ api, environmentID, onEnvironment, onDraft }: { api: APIClient; environmentID: string; onEnvironment: (id: string) => void; onDraft: (run: Run) => void }) {
  const queryClient = useQueryClient();
  const environments = useQuery({ queryKey: ["environments"], queryFn: () => api.environments() });
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models() });
  const providers = useQuery({ queryKey: ["providers"], queryFn: () => api.providers() });
  const suites = useQuery({ queryKey: ["benchmarks"], queryFn: () => api.benchmarks() });
  const networkProfiles = useQuery({ queryKey: ["network-profiles"], queryFn: () => api.networkProfiles() });
  const inspection = useQuery<Inspection | null>({ queryKey: ["inspection", environmentID], queryFn: () => api.inspectEnvironment(environmentID), enabled: false });
  const [fields, setFields] = useState<RunFields>(blankRun);
  const [errors, setErrors] = useState<FormErrors>({});
  const [taskSearch, setTaskSearch] = useState("");
  const [resolutionRequested, setResolutionRequested] = useState(false);
  const [busy, setBusy] = useState<"inspect" | "draft" | null>(null);
  const [operationError, setOperationError] = useState<Error | null>(null);
  const suite = suites.data?.find(suite => suite.id === "swebench-verified" && suite.dataset === "swe-bench/swe-bench-verified");
  const resolution = useQuery({ queryKey: ["benchmark-resolution", "swebench-verified", environmentID], queryFn: () => api.resolveBenchmark("swebench-verified", environmentID), enabled: false });
  const preview = resolutionRequested && !resolution.isError ? resolution.data : undefined;
  const environment = environments.data?.find(environment => environment.id === environmentID);
  const model = models.data?.find(model => model.id === fields.modelID);
  const provider = providers.data?.find(provider => provider.id === model?.provider_id);
  const sourceErrors = environment ? environmentNetworkErrors(environment, networkProfiles.data ?? []) : {};
  const networkErrors = environment ? environmentNetworkErrors(environment, networkProfiles.data ?? [], true) : {};
  const modelNetworkReady = Boolean(provider && networkProfiles.data?.some(profile => profile.id === provider.network_profile_id && profile.scope === "desktop"));
  const agents = inspection.data?.agents ?? [];
  const agent = agents.find(agent => agent.name === fields.agentName);
  const selectedTask = preview?.tasks.find(task => task.name === fields.taskName);
  useEffect(() => { setFields(current => ({ ...current, environmentID, agentName: "", taskName: "", kwargsText: "{}" })); setResolutionRequested(false); setTaskSearch(""); }, [environmentID]);
  function update<K extends keyof RunFields>(key: K, value: RunFields[K]) { setFields(current => ({ ...current, [key]: value })); }
  async function inspect() {
    if (!environmentID || !environment || busy || Object.keys(sourceErrors).length) return;
    setBusy("inspect"); setOperationError(null); setResolutionRequested(false); update("agentName", ""); update("taskName", "");
    queryClient.setQueryData(["inspection", environmentID], null);
    try { queryClient.setQueryData(["inspection", environmentID], await api.inspectEnvironment(environmentID)); }
    catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  function resolve() {
    if (!environmentID || !inspection.data?.ready || Object.keys(sourceErrors).length || busy || resolution.isFetching) return;
    setResolutionRequested(true); update("taskName", ""); void resolution.refetch();
  }
  async function createDraft() {
    if (busy || resolution.isFetching) return;
    const validated = validateRun({ ...fields, environmentID }, { environments: environments.data ?? [], models: models.data ?? [], providers: providers.data ?? [], suites: suites.data ?? [], networkProfiles: networkProfiles.data ?? [], resolution: preview, inspection: inspection.data ?? undefined });
    if (!validated.ok) { setErrors(validated.errors); return; }
    setErrors({}); setBusy("draft"); setOperationError(null);
    try {
      const draft = await api.createRun(validated.value);
      queryClient.setQueryData(["run", draft.id], draft); void queryClient.invalidateQueries({ queryKey: ["runs"] }); onDraft(draft);
    } catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  const filteredTasks = (preview?.tasks ?? []).filter(task => task.name === fields.taskName || task.name.toLowerCase().includes(taskSearch.toLowerCase()));
  const disabled = busy !== null || resolution.isFetching;
  const agentDisabled = disabled || !inspection.data?.ready || Object.keys(sourceErrors).length > 0 || !model;
  return <div className="flex flex-col gap-6">
    {[environments, models, providers, suites, networkProfiles].filter(query => query.isError).map((query, index) => <ErrorNotice key={index} error={query.error} retry={() => void query.refetch()} />)}
    <Card><CardHeader><CardTitle role="heading" aria-level={2}>单任务草稿</CardTitle><CardDescription>创建草稿时重新解析实际任务来源；核对快照后单独授权启动。</CardDescription></CardHeader><CardContent><form id="run-form" onSubmit={event => { event.preventDefault(); void createDraft(); }}>
      <FieldSet disabled={disabled}><FieldLegend className="sr-only">单任务草稿配置</FieldLegend><FieldGroup>
        <FieldSet><FieldLegend>环境与模型</FieldLegend><FieldGroup>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            <Field id="run-environment" label="执行环境" error={errors.environmentID} disabled={disabled}><Select disabled={disabled} value={environmentID || undefined} onValueChange={onEnvironment}><SelectTrigger id="run-environment" className="w-full" aria-invalid={Boolean(errors.environmentID)} aria-describedby={errors.environmentID ? "run-environment-help" : undefined}><SelectValue placeholder="选择环境" /></SelectTrigger><SelectContent><SelectGroup>{environments.data?.map(environment => <SelectItem key={environment.id} value={environment.id}>{environment.name} · {environment.type.toUpperCase()}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
            <Field id="run-model" label="固定模型" error={errors.modelID} disabled={disabled}><Select disabled={disabled} value={fields.modelID || undefined} onValueChange={value => { update("modelID", value); update("agentName", ""); update("kwargsText", "{}"); }}><SelectTrigger id="run-model" className="w-full" aria-invalid={Boolean(errors.modelID)} aria-describedby={errors.modelID ? "run-model-help" : undefined}><SelectValue placeholder="选择模型" /></SelectTrigger><SelectContent><SelectGroup>{models.data?.map(model => <SelectItem key={model.id} value={model.id}>{model.display_name || model.name} · {protocolLabels[model.protocol]}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
          </FieldGroup>
          {environment && networkProfiles.data && Object.keys(networkErrors).length > 0 && <Alert variant="destructive"><AlertTitle>Execution network setup required</AlertTitle><AlertDescription>{Object.values(networkErrors).join(" ")}</AlertDescription></Alert>}
          {model && networkProfiles.data && !modelNetworkReady && <Alert variant="destructive"><AlertTitle>Model network setup required</AlertTitle><AlertDescription>Bind this provider to a Desktop network profile in Models. Execution profiles do not supply model networking.</AlertDescription></Alert>}
          <div className="flex flex-wrap items-center gap-3"><Button type="button" variant="outline" onClick={() => void inspect()} disabled={!environmentID || disabled || Object.keys(sourceErrors).length > 0}><HugeiconsIcon icon={Search01Icon} data-icon="inline-start" aria-hidden="true" />{busy === "inspect" ? "检查中…" : "检查环境"}</Button><Badge variant="outline">{environment && networkProfiles.isPending ? "Reading network profiles…" : networkProfiles.isError ? "Network profiles unavailable" : Object.keys(sourceErrors).length > 0 ? "Source network required" : inspection.data ? inspection.data.ready ? "检查通过" : "环境未就绪" : "尚未检查"}</Badge></div>
          {inspection.data && !inspection.data.ready && <Alert variant="destructive"><AlertTitle>环境未就绪</AlertTitle><AlertDescription>{safeMessage(inspection.data.issues?.join("；") || "请在执行环境页面查看诊断。")}</AlertDescription></Alert>}
        </FieldGroup></FieldSet>
        <Separator />
        {suite ? <FieldSet><FieldLegend>{suite.name}</FieldLegend><FieldDescription><code>{suite.dataset}</code></FieldDescription><FieldGroup>
          <div className="flex flex-wrap items-center gap-3"><Button type="button" variant="outline" disabled={!inspection.data?.ready || disabled || Object.keys(sourceErrors).length > 0} onClick={resolve}><HugeiconsIcon icon={Search01Icon} data-icon="inline-start" aria-hidden="true" />{resolution.isFetching ? "解析来源…" : "解析默认任务来源"}</Button>{preview && <Badge variant="outline">{preview.tasks.length} 个任务</Badge>}</div>
          {resolution.isFetching && <LoadingState label="正在解析实际任务来源…" />}
          {resolutionRequested && resolution.isError && <ErrorNotice error={resolution.error} retry={resolve} />}
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            <TextField id="task-search" label="搜索任务" value={taskSearch} onChange={setTaskSearch} disabled={disabled || !preview} />
            <Field id="run-task" label="唯一任务" error={errors.taskName} disabled={disabled || !preview}><Select value={fields.taskName || undefined} onValueChange={value => update("taskName", value)} disabled={disabled || !preview}><SelectTrigger id="run-task" className="w-full" aria-invalid={Boolean(errors.taskName)} aria-describedby={errors.taskName ? "run-task-help" : undefined}><SelectValue placeholder={preview ? "选择完整任务名" : "先解析任务来源"} /></SelectTrigger><SelectContent><SelectGroup>{filteredTasks.map(task => <SelectItem key={task.name} value={task.name}>{task.name}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
          </FieldGroup>
          {preview && !filteredTasks.length && <Empty><EmptyHeader><EmptyTitle>没有匹配任务</EmptyTitle></EmptyHeader></Empty>}
          {preview && <><FieldDescription>创建草稿时后端会重新解析，不采用缓存预览作为批准来源。</FieldDescription><Collapsible className="group/source"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">来源预览<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/source:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="flex flex-col gap-5 pt-4 data-[state=closed]:hidden">
            <dl className="data-grid" aria-label="评测来源预览">
              <div><dt>实际数据集版本 / 任务数</dt><dd>{preview.suite.version || "未提供"} / {preview.suite.task_count}</dd></div>
              <div><dt>Registry 类型 / URL</dt><dd>{preview.registry_kind}<code className="block text-xs">{preview.suite.registry_url ? safeURL(preview.suite.registry_url) : "未提供"}</code></dd></div>
              <div><dt>Registry 元数据 URL</dt><dd>{preview.registry_metadata_url ? safeURL(preview.registry_metadata_url) : "未提供"}</dd></div>
              <div><dt>数据集 Package Digest</dt><dd><code className="text-xs">{preview.dataset_digest || "未提供"}</code></dd></div>
              <div><dt>数据集 Revision</dt><dd>{preview.dataset_revision || "未提供"}</dd></div>
              <div><dt>解析时间（resolved_at）</dt><dd><time dateTime={preview.resolved_at}>{preview.resolved_at}</time></dd></div>
              <div><dt>预览来源 Hash（非批准来源）</dt><dd><code className="text-xs">{preview.resolution_hash}</code></dd></div>
            </dl>
            {selectedTask && <dl className="data-grid" aria-label="选中任务来源预览"><div><dt>精确任务名</dt><dd>{selectedTask.name}</dd></div>{selectedTask.kind === "package" || selectedTask.package_name ? <><div><dt>任务 Package 名称</dt><dd>{selectedTask.package_name || "未提供"}</dd></div><div><dt>任务 Package Digest / Ref</dt><dd><code className="text-xs">{selectedTask.ref || "未提供"}</code></dd></div><div><dt>任务 Revision</dt><dd>{selectedTask.revision || "未提供"}</dd></div><div><dt>Git Commit</dt><dd>未提供；使用 Package Digest</dd></div></> : <><div><dt>任务 Git Commit</dt><dd><code className="text-xs">{selectedTask.git_commit || "未提供"}</code></dd></div><div><dt>任务 Git 仓库</dt><dd>{selectedTask.git_url ? safeURL(selectedTask.git_url) : "未提供"}</dd></div><div><dt>仓库内任务路径</dt><dd><code className="text-xs">{selectedTask.path || "未提供"}</code></dd></div></>}</dl>}
          </CollapsibleContent></Collapsible></>}
        </FieldGroup></FieldSet> : suites.isPending ? <LoadingState label="读取评测集…" /> : <Alert variant="destructive"><AlertTitle>SWE 评测集不可用</AlertTitle><AlertDescription>仅支持 swe-bench/swe-bench-verified，请检查本机后端配置。</AlertDescription></Alert>}
        <Separator />
        <FieldSet><FieldLegend>Agent</FieldLegend><FieldGroup>
          <Field id="run-agent" label="所选环境的 Agent" error={errors.agentName} disabled={agentDisabled}><Select value={fields.agentName || undefined} disabled={agentDisabled} onValueChange={name => { update("agentName", name); update("kwargsText", "{}"); }}><SelectTrigger id="run-agent" className="w-full" aria-invalid={Boolean(errors.agentName)} aria-describedby={errors.agentName ? "run-agent-help" : undefined}><SelectValue placeholder="先检查环境并选择模型" /></SelectTrigger><SelectContent><SelectGroup>{agents.map(agent => { const reasons = agentReasons(agent, model?.protocol); return <SelectItem key={agent.name} value={agent.name} disabled={reasons.length > 0}>{agent.name}{reasons.length ? " · 不兼容" : ""}</SelectItem>; })}</SelectGroup></SelectContent></Select></Field>
          <Alert><AlertTitle>Harbor 默认容器安装</AlertTitle><AlertDescription>宿主版本仅作提示。必须独立观测实际容器 CLI 版本并保存确认后才允许推理，未知版本不能替代。</AlertDescription></Alert>
          {agents.length > 0 && <Collapsible className="group/agents"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">Agent 兼容性<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/agents:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="pt-4 data-[state=closed]:hidden"><Table><TableHeader><TableRow><TableHead>Agent</TableHead><TableHead>原生协议 / 兼容性</TableHead><TableHead>宿主提示 / 独立命令</TableHead></TableRow></TableHeader><TableBody>{agents.map(agent => { const reasons = agentReasons(agent, model?.protocol); return <TableRow key={agent.name}><TableCell>{agent.name}</TableCell><TableCell className="max-w-80 whitespace-normal text-xs">{agent.supported_protocols?.map(protocol => protocolLabels[protocol]).join(" / ") || "无协议能力"}<FieldDescription>{reasons.length ? reasons.join("；") : model ? "与固定模型协议兼容" : "请先选择模型"}</FieldDescription></TableCell><TableCell className="max-w-96 whitespace-normal text-xs">{agent.installed ? `已发现 · ${safeMessage(agent.version || "版本未报告")}` : "宿主未安装，不阻止默认容器安装"}<FieldDescription>{safeMessage(agent.command || "未声明独立版本命令")}{agent.reason ? ` · ${safeMessage(agent.reason)}` : ""}</FieldDescription></TableCell></TableRow>; })}</TableBody></Table></CollapsibleContent></Collapsible>}
          <Field id="run-agent-kwargs" label="标量参数（JSON）" hint="仅接受实际 schema 的标量参数；不允许覆盖模型、版本、凭据或执行配置。" error={errors.kwargsText} disabled={disabled}><Textarea id="run-agent-kwargs" value={fields.kwargsText} onChange={event => update("kwargsText", event.target.value)} rows={4} disabled={disabled} aria-invalid={Boolean(errors.kwargsText)} aria-describedby="run-agent-kwargs-help" /></Field>
          {agent?.schema != null && <Collapsible className="group/schema"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">参数 schema<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/schema:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="pt-4 data-[state=closed]:hidden"><pre className="max-h-64 overflow-auto text-xs">{JSON.stringify(agent.schema, null, 2)}</pre></CollapsibleContent></Collapsible>}
        </FieldGroup></FieldSet>
        <Separator />
        <FieldSet><FieldLegend>执行边界</FieldLegend><FieldDescription>1 个任务 · 并发 1 · 尝试 1 · Trial 重试 0。请求与输出上限不是金额上限。</FieldDescription><FieldGroup className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <TextField id="run-max-requests" label="请求上限" value={fields.maxRequests} onChange={value => update("maxRequests", value)} error={errors.maxRequests} inputMode="numeric" disabled={disabled} />
          <TextField id="run-max-output" label="单次输出 Token 上限" value={fields.maxOutputTokens} onChange={value => update("maxOutputTokens", value)} error={errors.maxOutputTokens} inputMode="numeric" disabled={disabled} />
          <TextField id="run-timeout" label="执行超时（秒）" value={fields.timeout} onChange={value => update("timeout", value)} error={errors.timeout} inputMode="numeric" disabled={disabled} />
          <TextField id="run-setup-timeout" label="Setup 超时（秒）" value={fields.setupTimeout} onChange={value => update("setupTimeout", value)} error={errors.setupTimeout} inputMode="numeric" disabled={disabled} />
        </FieldGroup></FieldSet>
      </FieldGroup></FieldSet>
      {operationError && <div className="mt-4"><ErrorNotice error={operationError} title="草稿或检查未完成" /></div>}
    </form></CardContent><CardFooter><Button type="submit" form="run-form" disabled={disabled || !environmentID || !model || !inspection.data?.ready || !preview?.tasks.length || Object.keys(networkErrors).length > 0 || !modelNetworkReady}><HugeiconsIcon icon={Add01Icon} data-icon="inline-start" aria-hidden="true" />{busy === "draft" ? "解析来源并保存快照…" : "创建运行草稿（不启动）"}</Button></CardFooter></Card>
  </div>;
}
