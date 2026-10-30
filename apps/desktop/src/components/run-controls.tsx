import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { APIClient, activeRunStates, type Run } from "@router-forge/api";
import { Ban, Play, ShieldAlert } from "lucide-react";
import { ErrorNotice, errorMessage, safeURL } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from "@router-forge/ui/components/ui/alert-dialog";
import { Button } from "@router-forge/ui/components/ui/button";
import { Checkbox } from "@router-forge/ui/components/ui/checkbox";
import { Label } from "@router-forge/ui/components/ui/label";
import { boundedDraft, legacyDraft, startConfirmedRun } from "../lib/run-controls";

export function RunControls({ api, run }: { api: APIClient; run: Run }) {
  const queryClient = useQueryClient();
  const confirmationKey = JSON.stringify([run.id, run.snapshot.config_hash, run.snapshot.resolution_hash]);
  const [confirmedSource, setConfirmedSource] = useState<string | null>(null);
  const paidConfirmed = confirmedSource === confirmationKey;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const { spec, suite, resolved_tasks } = run.snapshot;
  const task = resolved_tasks?.find(task => task.name === spec.task_names[0]);
  const canStart = boundedDraft(run);
  const legacy = legacyDraft(run);
  function update(updated: Run) {
    queryClient.setQueryData(["run", run.id], updated);
    void queryClient.invalidateQueries({ queryKey: ["runs"] });
    void queryClient.invalidateQueries({ queryKey: ["events", run.id] });
  }
  async function start() {
    if (busy || !paidConfirmed) return;
    setBusy(true); setError(null);
    try { update(await startConfirmedRun(api, run, paidConfirmed)); }
    catch (error) { setError(new Error(errorMessage(error))); }
    finally { setBusy(false); setConfirmedSource(null); }
  }
  async function cancel() {
    if (busy || run.state !== "draft" && !activeRunStates.has(run.state)) return;
    setBusy(true); setError(null);
    try { update(await api.cancelRun(run.id)); setCancelTarget(null); }
    catch (error) { setError(new Error(errorMessage(error))); }
    finally { setBusy(false); }
  }
  const cancelControl = <AlertDialog open={cancelTarget === run.id} onOpenChange={open => setCancelTarget(open ? run.id : null)}><AlertDialogTrigger asChild><Button variant="outline" disabled={busy || run.state === "cancelling"}><Ban data-icon="inline-start" aria-hidden="true" />{run.state === "cancelling" ? "后端正在取消…" : run.state === "draft" ? "取消此草稿" : "取消此运行"}</Button></AlertDialogTrigger><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>取消这个{run.state === "draft" ? "草稿" : "运行"}？</AlertDialogTitle><AlertDialogDescription>只取消 {run.id} 的任务 {spec.task_names.join(", ")}，不会删除已有结果或请求账本。已发出的上游请求仍可能产生费用。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={busy}>保留记录</AlertDialogCancel><AlertDialogAction disabled={busy} onClick={event => { event.preventDefault(); void cancel(); }}>确认取消</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>;
  if (run.state === "draft") return <section aria-labelledby="paid-confirm-title" className="flex flex-col gap-4 rounded-lg border bg-card p-5">
    <div className="flex items-start gap-3"><ShieldAlert className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" /><div><h3 id="paid-confirm-title" className="text-base font-semibold">单独确认 · 启动真实评测</h3><p className="mt-1 text-sm text-muted-foreground">此草稿尚未执行。这里展示创建草稿时实际保存的来源，不是目录预览；刷新、重连与创建草稿都不会触发模型请求。</p></div></div>
    <dl className="data-grid">
      <div><dt>已批准唯一任务（恰好 1 个）</dt><dd>{spec.task_names.join(", ")}</dd></div>
      <div><dt>完整数据集 / 版本</dt><dd>{suite.dataset || "未记录"} / {suite.version || "未提供"}</dd></div>
      <div><dt>已批准配置 Hash</dt><dd><code className="text-xs">{run.snapshot.config_hash || "未记录"}</code></dd></div>
      <div><dt>已批准来源 Hash</dt><dd><code className="text-xs">{run.snapshot.resolution_hash || "未记录"}</code></dd></div>
      <div><dt>来源解析时间（resolved_at）</dt><dd>{run.snapshot.resolved_at || "未记录"}</dd></div>
      {task && (task.kind === "package" || task.package_name ? <>
        <div><dt>任务 Package / Digest</dt><dd>{task.package_name || "未记录"}<code className="block text-xs">{task.ref || "未记录"}</code></dd></div>
        <div><dt>任务 Revision / 数据集 Digest</dt><dd>{task.revision || "未提供"}<code className="block text-xs">{run.snapshot.dataset_digest || "未提供"}</code></dd></div>
        <div><dt>Git Commit</dt><dd>未提供；使用 Package Digest</dd></div>
      </> : <>
        <div><dt>任务 Git Commit</dt><dd><code className="text-xs">{task.git_commit || "未记录"}</code></dd></div>
        <div><dt>任务 Git 仓库 / 路径</dt><dd>{task.git_url ? safeURL(task.git_url) : "未记录"}<code className="block text-xs">{task.path || "未记录"}</code></dd></div>
      </>)}
      <div><dt>固定模型 / 容器 Agent</dt><dd>{run.snapshot.model.name} · {spec.agent}</dd></div>
      <div><dt>最多请求 / 每响应输出上限</dt><dd>{spec.max_requests} 次 / {spec.max_output_tokens} Token</dd></div>
      <div><dt>执行 / Setup 超时</dt><dd>{spec.timeout_sec} s / {spec.setup_timeout_sec} s</dd></div>
      <div><dt>并发 / 尝试 / Trial 重试</dt><dd>{spec.concurrency} / {spec.attempts} / 0</dd></div>
      <div><dt>容器安装策略</dt><dd>{legacy ? "旧草稿须重新创建，不会自动迁移安装或任务来源。" : "Harbor 默认容器安装，不请求精确版本，不安装宿主工具。"}</dd></div>
    </dl>
    <Alert><AlertTitle>实际版本独立观测</AlertTitle><AlertDescription>允许使用开始时实际安装的 uv / Harbor，草稿预检版本只是提示。容器 Agent CLI 版本尚未观测：必须独立观测成功并落盘确认后才允许推理，不能用宿主版本或未知版本代替。</AlertDescription></Alert>
    <Alert><AlertTitle>费用与明文密钥说明</AlertTitle><AlertDescription>将产生真实模型请求，可能产生费用。请求与输出上限不是绝对金额上限，输入长度、价格和缓存计费仍可能影响费用。{run.snapshot.model.pricing ? "所配置价格只用于估算，请核对运行快照的价格来源。" : "未配置价格，账本成本可能未知。"}服务商 Key 以明文保存在权限保护 SQLite，不加密，也不会传到任务容器。</AlertDescription></Alert>
    {legacy ? <Alert variant="destructive"><AlertTitle>旧草稿不能启动</AlertTitle><AlertDescription>此草稿使用旧运行快照格式；请重新创建草稿以解析当前来源。历史记录仍可读取或取消，不会自动迁移旧来源。</AlertDescription></Alert> : !canStart && <Alert variant="destructive"><AlertTitle>此草稿不能启动</AlertTitle><AlertDescription>缺少精确选中来源、批准 Hash、凭据、独立版本命令或有效执行边界，或仍含已停用的请求版本。请重新创建草稿。</AlertDescription></Alert>}
    <div className="flex items-start gap-2"><Checkbox id={`confirm-paid-${run.id}`} checked={paidConfirmed} onCheckedChange={value => setConfirmedSource(value === true ? confirmationKey : null)} disabled={busy || !canStart} /><Label htmlFor={`confirm-paid-${run.id}`} className="leading-relaxed">我已核对这个运行快照的实际选中来源，理解容器默认安装、独立版本观测、明文密钥与真实费用，并明确授权按上述边界启动一个任务。</Label></div>
    {error && <ErrorNotice error={error} title="运行操作未完成" />}
    <div className="flex flex-wrap gap-3"><Button type="button" onClick={() => void start()} disabled={busy || !paidConfirmed || !canStart}><Play data-icon="inline-start" aria-hidden="true" />{busy ? "提交运行请求…" : "确认并启动这一个任务"}</Button>{cancelControl}</div>
  </section>;
  if (!activeRunStates.has(run.state)) return error ? <ErrorNotice error={error} /> : null;
  return <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h3 className="font-semibold">运行由独立后端管理</h3><p className="text-xs text-muted-foreground">关闭窗口不取消任务；再次打开会重新附着到相同运行 ID。</p></div>{cancelControl}</div>
    {error && <ErrorNotice error={error} title="取消未完成" />}
  </section>;
}
