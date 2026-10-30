import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Inspection, type Run } from "@router-forge/api";
import { ClipboardPlus, Search } from "lucide-react";
import { ErrorNotice, LoadingState, errorMessage, safeURL } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Input } from "@router-forge/ui/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Textarea } from "@router-forge/ui/components/ui/textarea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Field, TextField } from "./fields";
import { agentReasons, blankRun, validateRun, type FormErrors, type RunFields } from "../lib/forms";

export function RunComposer({ api, environmentID, onEnvironment, onDraft }: { api: APIClient; environmentID: string; onEnvironment: (id: string) => void; onDraft: (run: Run) => void }) {
  const queryClient = useQueryClient();
  const environments = useQuery({ queryKey: ["environments"], queryFn: () => api.environments() });
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models() });
  const providers = useQuery({ queryKey: ["providers"], queryFn: () => api.providers() });
  const suites = useQuery({ queryKey: ["benchmarks"], queryFn: () => api.benchmarks() });
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
  const model = models.data?.find(model => model.id === fields.modelID);
  const agents = inspection.data?.agents ?? [];
  const agent = agents.find(agent => agent.name === fields.agentName);
  const selectedTask = preview?.tasks.find(task => task.name === fields.taskName);
  useEffect(() => { setFields(current => ({ ...current, environmentID, agentName: "", taskName: "", kwargsText: "{}" })); setResolutionRequested(false); setTaskSearch(""); }, [environmentID]);
  function update<K extends keyof RunFields>(key: K, value: RunFields[K]) { setFields(current => ({ ...current, [key]: value })); }
  async function inspect() {
    if (!environmentID || busy) return;
    setBusy("inspect"); setOperationError(null); setResolutionRequested(false); update("agentName", ""); update("taskName", "");
    queryClient.setQueryData(["inspection", environmentID], null);
    try { queryClient.setQueryData(["inspection", environmentID], await api.inspectEnvironment(environmentID)); }
    catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  function resolve() {
    if (!environmentID || !inspection.data?.ready || busy || resolution.isFetching) return;
    setResolutionRequested(true); update("taskName", ""); void resolution.refetch();
  }
  async function createDraft() {
    if (busy || resolution.isFetching) return;
    const validated = validateRun({ ...fields, environmentID }, { environments: environments.data ?? [], models: models.data ?? [], providers: providers.data ?? [], suites: suites.data ?? [], resolution: preview, inspection: inspection.data ?? undefined });
    if (!validated.ok) { setErrors(validated.errors); return; }
    setErrors({}); setBusy("draft"); setOperationError(null);
    try {
      const draft = await api.createRun(validated.value);
      queryClient.setQueryData(["run", draft.id], draft); void queryClient.invalidateQueries({ queryKey: ["runs"] }); onDraft(draft);
    } catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  const filteredTasks = (preview?.tasks ?? []).filter(task => task.name === fields.taskName || task.name.toLowerCase().includes(taskSearch.toLowerCase()));
  return <div className="flex flex-col gap-6">
    <div><h2 className="text-xl font-semibold">创建单任务草稿</h2><p className="mt-1 text-sm text-muted-foreground">固定模型路由。创建草稿时重新解析实际任务来源并保存运行快照；核对草稿来源、另行确认后才启动。</p></div>
    {[environments, models, providers, suites].filter(query => query.isError).map((query, index) => <ErrorNotice key={index} error={query.error} retry={() => void query.refetch()} />)}
    <Card><CardHeader><CardTitle className="text-base">环境、固定模型与唯一任务</CardTitle></CardHeader><CardContent><form onSubmit={event => { event.preventDefault(); void createDraft(); }} className="space-y-5">
      <fieldset disabled={busy === "draft"} className="space-y-5">
        <div className="grid gap-4 md:grid-cols-2">
          <Field id="run-environment" label="执行环境" error={errors.environmentID}><Select value={environmentID || undefined} onValueChange={onEnvironment}><SelectTrigger id="run-environment" className="w-full" aria-invalid={Boolean(errors.environmentID)} aria-describedby="run-environment-help"><SelectValue placeholder="选择已保存的环境" /></SelectTrigger><SelectContent><SelectGroup>{environments.data?.map(environment => <SelectItem key={environment.id} value={environment.id}>{environment.name} · {environment.type.toUpperCase()}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
          <Field id="run-model" label="固定模型" error={errors.modelID}><Select value={fields.modelID || undefined} onValueChange={value => { update("modelID", value); update("agentName", ""); }}><SelectTrigger id="run-model" className="w-full" aria-invalid={Boolean(errors.modelID)} aria-describedby="run-model-help"><SelectValue placeholder="选择已保存模型" /></SelectTrigger><SelectContent><SelectGroup>{models.data?.map(model => <SelectItem key={model.id} value={model.id}>{model.display_name || model.name} · {protocolLabels[model.protocol]}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
        </div>
        <div className="flex flex-wrap items-center gap-3"><Button type="button" variant="outline" onClick={() => void inspect()} disabled={!environmentID || Boolean(busy)}><Search className="size-4" aria-hidden="true" />{busy === "inspect" ? "实际检查中…" : "检查选中环境"}</Button><Badge variant="outline">{inspection.data ? inspection.data.ready ? "已通过检查" : "环境未就绪" : "尚未检查"}</Badge>{model && <Badge variant="outline">原生 {protocolLabels[model.protocol]}</Badge>}</div>
        {inspection.data && !inspection.data.ready && <Alert variant="destructive"><AlertTitle>此环境不能开始评测</AlertTitle><AlertDescription>{inspection.data.issues?.join("；") || "请在环境页面查看工具与兼容性诊断。"}</AlertDescription></Alert>}
        {suite ? <div className="rounded-md border p-4"><div className="panel-heading"><div><h3 className="font-medium">{suite.name}</h3><p className="mt-1 text-xs text-muted-foreground"><code>{suite.dataset}</code> · 来源与版本由已安装 Harbor 默认解析</p></div><Button type="button" size="sm" variant="outline" disabled={!inspection.data?.ready || Boolean(busy) || resolution.isFetching} onClick={resolve}>{resolution.isFetching ? "解析来源…" : "解析默认任务来源"}</Button></div>
          {resolution.isFetching && <LoadingState label="正在通过所选环境已安装 Harbor 解析实际来源…" />}
          {resolutionRequested && resolution.isError && <div className="mt-3"><ErrorNotice error={resolution.error} retry={resolve} /></div>}
          {preview && <div className="mt-4 flex flex-col gap-4">
            <dl className="data-grid" aria-label="评测来源预览">
              <div><dt>实际数据集版本 / 任务数</dt><dd>{preview.suite.version || "未提供"} / {preview.suite.task_count}</dd></div>
              <div><dt>Registry 类型 / URL</dt><dd>{preview.registry_kind}<code className="block text-xs">{preview.suite.registry_url ? safeURL(preview.suite.registry_url) : "未提供"}</code></dd></div>
              <div><dt>Registry 元数据 URL</dt><dd>{preview.registry_metadata_url ? safeURL(preview.registry_metadata_url) : "未提供"}</dd></div>
              <div><dt>数据集 Package Digest</dt><dd><code className="text-xs">{preview.dataset_digest || "未提供"}</code></dd></div>
              <div><dt>数据集 Revision</dt><dd>{preview.dataset_revision || "未提供"}</dd></div>
              <div><dt>解析时间（resolved_at）</dt><dd><time dateTime={preview.resolved_at}>{preview.resolved_at}</time></dd></div>
              <div><dt>预览来源 Hash（非批准来源）</dt><dd><code className="text-xs">{preview.resolution_hash}</code></dd></div>
            </dl>
            <Alert><AlertTitle>这里仅是来源预览</AlertTitle><AlertDescription>创建草稿时后端会重新解析，不采用缓存预览作为批准来源。请在创建后的运行快照中核对实际选中来源，再单独确认启动。</AlertDescription></Alert>
            <div className="grid gap-4 md:grid-cols-2"><Field id="task-search" label="按任务名称搜索" hint={`此次解析实际返回 ${preview.tasks.length} 个任务预览。`}><Input id="task-search" value={taskSearch} onChange={event => setTaskSearch(event.target.value)} placeholder="完整精确任务名的片段" /></Field><Field id="run-task" label="唯一任务（恰好一个）" error={errors.taskName}><Select value={fields.taskName || undefined} onValueChange={value => update("taskName", value)} disabled={resolution.isFetching}><SelectTrigger id="run-task" className="w-full" aria-invalid={Boolean(errors.taskName)} aria-describedby="run-task-help"><SelectValue placeholder="从实际预览选择完整、精确的任务名" /></SelectTrigger><SelectContent><SelectGroup>{filteredTasks.map(task => <SelectItem key={task.name} value={task.name}>{task.name}</SelectItem>)}</SelectGroup></SelectContent></Select>{!filteredTasks.length && <p className="text-xs text-muted-foreground">没有匹配任务。请修改搜索，不接受预览外名称或其他命名空间的同名任务。</p>}</Field></div>
            {selectedTask && <dl className="data-grid" aria-label="选中任务来源预览"><div><dt>精确任务名</dt><dd>{selectedTask.name}</dd></div>{selectedTask.kind === "package" || selectedTask.package_name ? <><div><dt>任务 Package 名称</dt><dd>{selectedTask.package_name || "未提供"}</dd></div><div><dt>任务 Package Digest / Ref</dt><dd><code className="text-xs">{selectedTask.ref || "未提供"}</code></dd></div><div><dt>任务 Revision</dt><dd>{selectedTask.revision || "未提供"}</dd></div><div><dt>Git Commit</dt><dd>未提供；使用 Package Digest</dd></div></> : <><div><dt>任务 Git Commit</dt><dd><code className="text-xs">{selectedTask.git_commit || "未提供"}</code></dd></div><div><dt>任务 Git 仓库</dt><dd>{selectedTask.git_url ? safeURL(selectedTask.git_url) : "未提供"}</dd></div><div><dt>仓库内任务路径</dt><dd><code className="text-xs">{selectedTask.path || "未提供"}</code></dd></div></>}</dl>}
          </div>}
          {!preview && !resolution.isFetching && <p className="mt-3 text-xs text-muted-foreground">环境通过检查后解析实际来源；不预填版本、任务数量、Git Commit 或模拟任务列表。</p>}
        </div> : suites.isPending ? <LoadingState label="读取固定评测集描述…" /> : <Alert variant="destructive"><AlertTitle>唯一支持的 SWE 评测集不可用</AlertTitle><AlertDescription>仅支持 swe-bench/swe-bench-verified，请检查本机后端配置。</AlertDescription></Alert>}
        {errors.taskName && !preview && <p className="text-sm text-destructive" role="alert">{errors.taskName}</p>}
        <div className="grid gap-4 md:grid-cols-2"><Field id="run-agent" label="所选环境发现的 Agent" error={errors.agentName}><Select value={fields.agentName || undefined} disabled={!inspection.data?.ready || !model} onValueChange={name => { update("agentName", name); update("kwargsText", "{}"); }}><SelectTrigger id="run-agent" className="w-full" aria-invalid={Boolean(errors.agentName)} aria-describedby="run-agent-help"><SelectValue placeholder="先检查环境并选定原生协议" /></SelectTrigger><SelectContent><SelectGroup>{agents.map(agent => <SelectItem key={agent.name} value={agent.name} disabled={agentReasons(agent, model?.protocol).length > 0}>{agent.name}{agentReasons(agent, model?.protocol).length ? " · 不兼容" : ""}</SelectItem>)}</SelectGroup></SelectContent></Select></Field><Alert><AlertTitle>Harbor 默认容器安装</AlertTitle><AlertDescription>{agent?.version ? `宿主版本提示：${agent.version}；不填入运行请求，也不是容器版本证明。` : "不指定 Agent 精确版本；宿主未报告版本不阻止默认容器安装。"} 必须独立观测实际容器 CLI 版本并保存确认后才允许推理，未知版本不能替代。</AlertDescription></Alert></div>
        {agents.length > 0 && <div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow><TableHead>Agent</TableHead><TableHead>协议与兼容性</TableHead><TableHead>宿主提示 / 默认容器安装</TableHead></TableRow></TableHeader><TableBody>{agents.map(agent => { const reasons = agentReasons(agent, model?.protocol); return <TableRow key={agent.name}><TableCell>{agent.name}</TableCell><TableCell className="max-w-80 whitespace-normal text-xs">{agent.supported_protocols?.map(protocol => protocolLabels[protocol]).join(" / ") || "无协议能力"}<p className={reasons.length ? "text-destructive" : "text-muted-foreground"}>{reasons.length ? reasons.join("；") : model ? "与固定模型协议兼容" : "请先选择模型"}</p></TableCell><TableCell className="max-w-96 whitespace-normal text-xs">宿主：{agent.installed ? `已发现 · ${agent.version || "版本未报告"}` : "未安装（不阻止默认容器安装）"}<p className="text-muted-foreground">Harbor 默认容器安装；独立版本命令：{agent.command || "未声明"}{agent.reason ? ` · ${agent.reason}` : ""}</p></TableCell></TableRow>; })}</TableBody></Table></div>}
        <Field id="run-agent-kwargs" label="Agent 标量参数 · JSON 对象" hint="仅接受实际 schema 中的标量参数；不能覆盖模型、版本、凭据、环境、路径、MCP、skills 或提示配置。" error={errors.kwargsText}><Textarea id="run-agent-kwargs" value={fields.kwargsText} onChange={event => update("kwargsText", event.target.value)} rows={4} className="font-mono text-xs" aria-invalid={Boolean(errors.kwargsText)} aria-describedby="run-agent-kwargs-help" /></Field>
        {agent?.schema != null && <details className="rounded-md border p-3"><summary className="cursor-pointer text-sm">查看此 Agent 的实际参数 schema</summary><pre className="mt-3 max-h-64 overflow-auto text-xs">{JSON.stringify(agent.schema, null, 2)}</pre></details>}
        <section className="space-y-3 border-t pt-5"><h3 className="font-semibold">明确执行边界</h3><div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <TextField id="run-max-requests" label="max_requests" value={fields.maxRequests} onChange={value => update("maxRequests", value)} error={errors.maxRequests} inputMode="numeric" hint="最多 10000 次；不是金额上限。" />
          <TextField id="run-max-output" label="max_output_tokens" value={fields.maxOutputTokens} onChange={value => update("maxOutputTokens", value)} error={errors.maxOutputTokens} inputMode="numeric" hint="单个响应输出上限，最多 1000000。" />
          <TextField id="run-timeout" label="执行超时（秒）" value={fields.timeout} onChange={value => update("timeout", value)} error={errors.timeout} inputMode="numeric" />
          <TextField id="run-setup-timeout" label="Setup 超时（秒）" value={fields.setupTimeout} onChange={value => update("setupTimeout", value)} error={errors.setupTimeout} inputMode="numeric" />
        </div><p className="text-xs text-muted-foreground">固定：1 个任务、并发 1、尝试 1、Trial 重试 0。允许 Harbor 原生任务容器安装，不安装或升级宿主工具。</p></section>
      </fieldset>
      {operationError && <ErrorNotice error={operationError} title="草稿或检查未完成" />}
      <div className="flex flex-wrap items-center gap-3"><Button type="submit" disabled={Boolean(busy) || resolution.isFetching || !environmentID || !model || !inspection.data?.ready || !preview?.tasks.length}><ClipboardPlus data-icon="inline-start" aria-hidden="true" />{busy === "draft" ? "解析来源并保存快照…" : "创建运行草稿（不启动）"}</Button><span className="text-xs text-muted-foreground">创建后核对草稿实际来源并单独确认费用，不自动授权启动。</span></div>
    </form></CardContent></Card>
  </div>;
}
