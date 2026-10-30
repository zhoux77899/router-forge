import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { APIClient, activeRunStates, type Run } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { Alert02Icon, ArrowDown01Icon, Cancel01Icon, PlayIcon } from "@hugeicons/core-free-icons";
import { ErrorNotice, errorMessage, safeMessage, safeURL } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from "@router-forge/ui/components/ui/alert-dialog";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Checkbox } from "@router-forge/ui/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { FieldDescription, FieldGroup, FieldLegend, FieldSet } from "@router-forge/ui/components/ui/field";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Field } from "./fields";
import { LegacyNetworkDiagnostic } from "./environments";
import { executionNetworkStages } from "../lib/forms";
import { boundedDraft, emptyRunApprovals, legacyDraft, runConfirmationKey, runTLSRisks, startApprovalInput, startConfirmedRun, type RunApprovals } from "../lib/run-controls";

export function RunControls({ api, run }: { api: APIClient; run: Run }) {
  const queryClient = useQueryClient();
  const confirmationKey = runConfirmationKey(run);
  const [approvals, setApprovals] = useState<RunApprovals>(emptyRunApprovals);
  const startInput = startApprovalInput(run, approvals);
  const risks = runTLSRisks(run);
  const approvalsComplete = startInput.confirm_paid && (!risks.execution || startInput.confirm_execution_tls_risk === true) && (!risks.model || startInput.confirm_model_tls_risk === true);
  const [busy, setBusy] = useState<"start" | "cancel" | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const { spec, suite, resolved_tasks, environment, network } = run.snapshot;
  const task = resolved_tasks?.find(task => task.name === spec.task_names[0]);
  const canStart = boundedDraft(run);
  const legacy = legacyDraft(run);
  useEffect(() => { setApprovals(emptyRunApprovals()); }, [confirmationKey]);
  function approve(field: Exclude<keyof RunApprovals, "key">, checked: boolean) {
    setApprovals(current => ({ ...(current.key === confirmationKey ? current : emptyRunApprovals()), key: confirmationKey, [field]: checked }));
  }
  function update(updated: Run) {
    queryClient.setQueryData(["run", run.id], updated);
    void queryClient.invalidateQueries({ queryKey: ["runs"] });
    void queryClient.invalidateQueries({ queryKey: ["events", run.id] });
  }
  async function start() {
    if (busy || !canStart || !approvalsComplete) return;
    setBusy("start"); setError(null);
    try { update(await startConfirmedRun(api, run, startInput)); }
    catch (error) { setError(new Error(errorMessage(error))); }
    finally { setBusy(null); setApprovals(emptyRunApprovals()); }
  }
  async function cancel() {
    if (busy || run.state !== "draft" && !activeRunStates.has(run.state)) return;
    setBusy("cancel"); setError(null);
    try { update(await api.cancelRun(run.id)); setCancelTarget(null); }
    catch (error) { setError(new Error(errorMessage(error))); }
    finally { setBusy(null); }
  }
  const cancelControl = <AlertDialog open={cancelTarget === run.id} onOpenChange={open => { setCancelTarget(open ? run.id : null); if (open) setError(null); }}><AlertDialogTrigger asChild><Button type="button" variant="outline" disabled={Boolean(busy) || run.state === "cancelling"}><HugeiconsIcon icon={Cancel01Icon} data-icon="inline-start" aria-hidden="true" />{run.state === "cancelling" || busy === "cancel" ? "正在取消…" : run.state === "draft" ? "取消此草稿" : "取消此运行"}</Button></AlertDialogTrigger><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>取消这个{run.state === "draft" ? "草稿" : "运行"}？</AlertDialogTitle><AlertDialogDescription>只取消 {run.id} 的任务 {spec.task_names.join(", ")}。保留已有结果与账本；已发出的上游请求仍可能产生费用。</AlertDialogDescription></AlertDialogHeader>{error && <ErrorNotice error={error} title="取消未完成" />}<AlertDialogFooter><AlertDialogCancel disabled={Boolean(busy)}>保留记录</AlertDialogCancel><AlertDialogAction disabled={Boolean(busy)} onClick={event => { event.preventDefault(); void cancel(); }}>{busy === "cancel" ? "取消中…" : "确认取消"}</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>;
  if (run.state === "draft") return <Card role="region" aria-labelledby={`paid-confirm-title-${run.id}`}>
    <CardHeader><CardTitle id={`paid-confirm-title-${run.id}`} role="heading" aria-level={3}>启动真实评测</CardTitle><CardDescription>以下是此草稿实际保存的快照，不是目录预览。</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-5">
      <dl className="data-grid">
        <div><dt>唯一任务</dt><dd>{spec.task_names.join(", ")}</dd></div>
        <div><dt>数据集 / 版本</dt><dd>{suite.dataset || "未记录"} / {suite.version || "未提供"}</dd></div>
        <div><dt>固定模型 / Agent</dt><dd>{run.snapshot.model.name} · {spec.agent}</dd></div>
        <div><dt>最多请求 / 单次输出上限</dt><dd>{spec.max_requests} 次 / {spec.max_output_tokens} Token</dd></div>
        <div><dt>执行 / Setup 超时</dt><dd>{spec.timeout_sec} s / {spec.setup_timeout_sec} s</dd></div>
        <div><dt>并发 / 尝试 / Trial 重试</dt><dd>{spec.concurrency} / {spec.attempts} / 0</dd></div>
      </dl>
      {network && <Collapsible className="group/network-snapshot"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">Network snapshot<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/network-snapshot:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="pt-4 data-[state=closed]:hidden"><Table aria-label="Frozen network profiles"><TableHeader><TableRow><TableHead>Stage</TableHead><TableHead>Profile / revision</TableHead><TableHead>TLS policy</TableHead><TableHead>Trust</TableHead></TableRow></TableHeader><TableBody>{[...executionNetworkStages, "model" as const].map(stage => {
        const profile = network[stage];
        return <TableRow key={stage}><TableCell>{stage[0]?.toUpperCase() + stage.slice(1)}</TableCell><TableCell>{safeMessage(profile?.name || "Not recorded")}<code className="block text-xs">{profile?.id || "Not recorded"} / {profile?.revision || "Not recorded"}</code><FieldDescription>{profile?.scope === "desktop" ? "Desktop" : profile?.environment_id || "Host not recorded"}{profile?.credential_revision ? ` · credential revision ${profile.credential_revision}` : ""}</FieldDescription></TableCell><TableCell>{profile?.tls_mode === "skip" ? "Skip certificates" : profile?.tls_mode === "verify" ? "Verify certificates" : "Not recorded"}</TableCell><TableCell>{profile?.trust_mode === "custom" ? <>{safeMessage(profile.ca_cert_path || "CA not recorded")}{profile.tls_mode === "skip" && " (retained, ignored)"}</> : profile?.trust_mode === "system" ? "Host system CA" : "Not recorded"}</TableCell></TableRow>;
      })}</TableBody></Table></CollapsibleContent></Collapsible>}
      {legacy && <LegacyNetworkDiagnostic environment={environment} />}
      <Collapsible className="group/source"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">来源与快照<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/source:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="pt-4 data-[state=closed]:hidden"><dl className="data-grid" aria-label="已保存的任务来源与批准 Hash">
        <div><dt>配置 Hash</dt><dd><code className="text-xs">{run.snapshot.config_hash || "未记录"}</code></dd></div>
        <div><dt>来源 Hash</dt><dd><code className="text-xs">{run.snapshot.resolution_hash || "未记录"}</code></dd></div>
        <div><dt>来源解析时间</dt><dd>{run.snapshot.resolved_at || "未记录"}</dd></div>
        {task && (task.kind === "package" || task.package_name ? <><div><dt>任务 Package / Digest</dt><dd>{task.package_name || "未记录"}<code className="block text-xs">{task.ref || "未记录"}</code></dd></div><div><dt>任务 Revision / 数据集 Digest</dt><dd>{task.revision || "未提供"}<code className="block text-xs">{run.snapshot.dataset_digest || "未提供"}</code></dd></div><div><dt>Git Commit</dt><dd>未提供；使用 Package Digest</dd></div></> : <><div><dt>任务 Git Commit</dt><dd><code className="text-xs">{task.git_commit || "未记录"}</code></dd></div><div><dt>任务 Git 仓库 / 路径</dt><dd>{task.git_url ? safeURL(task.git_url) : "未记录"}<code className="block text-xs">{task.path || "未记录"}</code></dd></div></>)}
      </dl></CollapsibleContent></Collapsible>
      <Alert id={`paid-version-${run.id}`}><AlertTitle>Harbor 默认容器安装</AlertTitle><AlertDescription>使用启动时实际安装的 uv / Harbor；宿主版本不证明容器版本。容器 CLI 必须独立观测成功并落盘确认后才允许推理。</AlertDescription></Alert>
      <Alert id={`paid-cost-${run.id}`}><AlertTitle>真实费用与明文密钥</AlertTitle><AlertDescription>将产生真实模型请求。请求与输出上限不是绝对金额上限，输入长度、价格和缓存计费仍影响费用。{run.snapshot.model.pricing ? "价格仅用于估算，请核对快照中的价格来源。" : "未配置价格，账本成本可能未知。"}服务商 Key 以明文保存在权限保护 SQLite，不加密，不传入任务容器。</AlertDescription></Alert>
      {legacy ? <Alert variant="destructive"><AlertTitle>Historical draft cannot start</AlertTitle><AlertDescription>Recreate this draft with format-3 network profiles and current sources. History remains readable and cancellable. Legacy settings are not copied and sources are not migrated automatically.</AlertDescription></Alert> : !canStart && <Alert variant="destructive"><AlertTitle>This draft cannot start</AlertTitle><AlertDescription>Recreate it with complete host-scoped network profiles, explicit Docker mode, exact source hashes, credentials, an independent version command and valid execution limits.</AlertDescription></Alert>}
      {risks.execution && <Alert id={`execution-tls-risk-${run.id}`} variant="destructive"><HugeiconsIcon icon={Alert02Icon} aria-hidden="true" /><AlertTitle>Execution TLS verification is disabled</AlertTitle><AlertDescription>The frozen {executionNetworkStages.filter(stage => network?.[stage]?.tls_mode === "skip").join(", ")} profiles do not verify server certificates. A MITM can alter metadata, downloads or installation scripts. This approval does not affect model HTTPS, SSH host checks, Docker daemon pulls, original Dockerfile builds or verifier checks.</AlertDescription></Alert>}
      {risks.model && <Alert id={`model-tls-risk-${run.id}`} variant="destructive"><HugeiconsIcon icon={Alert02Icon} aria-hidden="true" /><AlertTitle>Model TLS verification is disabled</AlertTitle><AlertDescription>The frozen Desktop profile does not verify the model provider certificate. A MITM can intercept model requests, responses or the API Key. Execution TLS approval does not authorize this risk.</AlertDescription></Alert>}
      <FieldSet disabled={Boolean(busy) || !canStart}><FieldLegend className="sr-only">Independent start approvals</FieldLegend><FieldGroup>
        {risks.execution && <Field id={`confirm-execution-tls-${run.id}`} label="I accept the execution TLS risk for this snapshot." orientation="horizontal" disabled={Boolean(busy) || !canStart}><Checkbox id={`confirm-execution-tls-${run.id}`} checked={startInput.confirm_execution_tls_risk === true} onCheckedChange={value => approve("executionTLSRisk", value === true)} disabled={Boolean(busy) || !canStart} aria-describedby={`execution-tls-risk-${run.id}`} /></Field>}
        {risks.model && <Field id={`confirm-model-tls-${run.id}`} label="I accept the model TLS risk for this snapshot." orientation="horizontal" disabled={Boolean(busy) || !canStart}><Checkbox id={`confirm-model-tls-${run.id}`} checked={startInput.confirm_model_tls_risk === true} onCheckedChange={value => approve("modelTLSRisk", value === true)} disabled={Boolean(busy) || !canStart} aria-describedby={`model-tls-risk-${run.id}`} /></Field>}
        <Field id={`confirm-paid-${run.id}`} label="I reviewed this immutable snapshot and authorize one paid task within these limits." orientation="horizontal" disabled={Boolean(busy) || !canStart}><Checkbox id={`confirm-paid-${run.id}`} checked={startInput.confirm_paid} onCheckedChange={value => approve("paid", value === true)} disabled={Boolean(busy) || !canStart} aria-describedby={`paid-version-${run.id} paid-cost-${run.id}`} /></Field>
      </FieldGroup></FieldSet>
      {error && cancelTarget !== run.id && <ErrorNotice error={error} title="运行操作未完成" />}
    </CardContent>
    <CardFooter className="flex-wrap gap-3"><Button type="button" onClick={() => void start()} disabled={Boolean(busy) || !approvalsComplete || !canStart}><HugeiconsIcon icon={PlayIcon} data-icon="inline-start" aria-hidden="true" />{busy === "start" ? "提交运行请求…" : "确认并启动这一个任务"}</Button>{cancelControl}</CardFooter>
  </Card>;
  if (!activeRunStates.has(run.state)) return error ? <ErrorNotice error={error} /> : null;
  return <Card><CardHeader><CardTitle role="heading" aria-level={3}>运行由独立后端管理</CardTitle><CardDescription>关闭窗口不取消任务；重新打开后附着到相同运行 ID。</CardDescription></CardHeader>{error && cancelTarget !== run.id && <CardContent><ErrorNotice error={error} title="取消未完成" /></CardContent>}<CardFooter>{cancelControl}</CardFooter></Card>;
}
