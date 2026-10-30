import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Environment, type Inspection } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowDown01Icon, CheckmarkCircle02Icon, FloppyDiskIcon, Search01Icon, ServerStack01Icon } from "@hugeicons/core-free-icons";
import { ErrorNotice, LoadingState, errorMessage, safeMessage, safeURL } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { FieldDescription, FieldGroup, FieldLegend, FieldSet } from "@router-forge/ui/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Separator } from "@router-forge/ui/components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Field, TextField } from "./fields";
import { blankEnvironment, environmentFields, environmentNetworkErrors, executionNetworkStages, validateEnvironment, type EnvironmentFields, type FormErrors } from "../lib/forms";
import { NetworkProfileSelect } from "./networks";

export function LegacyNetworkDiagnostic({ environment }: { environment: Environment | null | undefined }) {
  if (!environment) return null;
  const legacyGateway = !environment.docker_gateway_mode ? environment.docker_host_gateway : undefined;
  if (!environment.proxy_url && !environment.no_proxy && !environment.ca_cert_path && environment.skip_tls_verification !== true && !legacyGateway) return null;
  return <Collapsible className="group/legacy-network"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between">Legacy network settings<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/legacy-network:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="flex flex-col gap-4 pt-4 data-[state=closed]:hidden">
    <FieldDescription>Read-only historical values. New drafts do not use or inherit them; bind network profiles explicitly.</FieldDescription>
    <dl className="data-grid" aria-label="Legacy network migration diagnostic">
      {environment.proxy_url && <div><dt>Proxy</dt><dd>{safeURL(environment.proxy_url)}</dd></div>}
      {environment.no_proxy && <div><dt>NO_PROXY</dt><dd>{safeMessage(environment.no_proxy)}</dd></div>}
      {environment.ca_cert_path && <div><dt>CA path</dt><dd>{safeMessage(environment.ca_cert_path)}</dd></div>}
      <div><dt>Historical TLS</dt><dd>{environment.skip_tls_verification === true ? "Skip (historical)" : environment.skip_tls_verification === false ? "Verify (historical)" : "Not recorded"}</dd></div>
      {legacyGateway && <div><dt>Historical Docker gateway</dt><dd>{safeMessage(legacyGateway)}</dd></div>}
    </dl>
  </CollapsibleContent></Collapsible>;
}

export function InspectionResults({ inspection }: { inspection: Inspection | null | undefined }) {
  if (!inspection) return <Empty><EmptyHeader><EmptyMedia variant="icon"><HugeiconsIcon icon={Search01Icon} aria-hidden="true" /></EmptyMedia><EmptyTitle>尚未检查环境</EmptyTitle></EmptyHeader></Empty>;
  return <Card><CardHeader><CardTitle role="heading" aria-level={2}>检查结果</CardTitle></CardHeader><CardContent className="flex flex-col gap-6">
    <Alert variant={inspection.ready ? "default" : "destructive"}><HugeiconsIcon icon={ServerStack01Icon} aria-hidden="true" /><AlertTitle>{inspection.ready ? "环境检查通过" : "环境尚未就绪"}</AlertTitle><AlertDescription><p>{inspection.os || "OS 未知"} / {inspection.arch || "架构未知"} · {inspection.work_dir || "工作目录未知"}</p>{inspection.issues?.length > 0 && <ul className="flex list-inside list-disc flex-col gap-1">{inspection.issues.map((issue, index) => <li key={index}>{safeMessage(issue)}</li>)}</ul>}</AlertDescription></Alert>
    <section aria-label="宿主工具" className="flex flex-col gap-3"><h3 className="font-semibold">宿主工具</h3>{inspection.tools?.length ? <Table><TableHeader><TableRow><TableHead>工具</TableHead><TableHead>状态</TableHead><TableHead>版本 / 路径</TableHead><TableHead>诊断</TableHead></TableRow></TableHeader><TableBody>{inspection.tools.map(tool => <TableRow key={tool.name}><TableCell>{tool.name}</TableCell><TableCell><Badge variant={tool.available ? "secondary" : "outline"}>{tool.available ? "可用" : "不可用"}</Badge></TableCell><TableCell><span className="block">{safeMessage(tool.version || (tool.version_output ? "未提取数值版本" : "版本未报告"))}</span><code className="block break-all text-xs text-muted-foreground">{safeMessage(tool.path || "路径未知")}</code>{tool.version_output && <pre className="mt-1 whitespace-pre-wrap break-words text-xs text-muted-foreground">{safeMessage(tool.version_output)}</pre>}</TableCell><TableCell className="max-w-96 whitespace-normal text-xs">{safeMessage(tool.detail || "—")}</TableCell></TableRow>)}</TableBody></Table> : <Empty><EmptyHeader><EmptyTitle>未返回工具诊断</EmptyTitle></EmptyHeader></Empty>}</section>
    <Separator />
    <section aria-label="Agent 能力" className="flex flex-col gap-3"><h3 className="font-semibold">Agent</h3><FieldDescription>宿主版本不代表容器版本。Harbor 使用默认容器安装；成功独立观测实际容器 CLI 版本并保存确认后才允许推理。</FieldDescription>
      {inspection.agents?.length ? <Table><TableHeader><TableRow><TableHead>Agent</TableHead><TableHead>宿主版本</TableHead><TableHead>Harbor / 原生协议</TableHead><TableHead>容器安装与诊断</TableHead></TableRow></TableHeader><TableBody>{inspection.agents.map(agent => <TableRow key={agent.name}><TableCell>{agent.name}<code className="mt-1 block break-all text-xs text-muted-foreground">{safeMessage(agent.path || agent.command || "路径未报告")}</code></TableCell><TableCell>{agent.installed ? "已发现" : "宿主未安装"}<span className="block text-xs text-muted-foreground">{safeMessage(agent.version || "版本未报告")}</span></TableCell><TableCell><Badge variant="outline">{agent.harbor_supported ? "Adapter 支持" : "Adapter 不支持"}</Badge><p className="mt-1 text-xs">{agent.supported_protocols?.map(protocol => protocolLabels[protocol]).join(" / ") || "无已确认协议"}</p></TableCell><TableCell className="max-w-96 whitespace-normal text-xs">{safeMessage(agent.container_install || "未声明安装方式")}<p className="text-muted-foreground">{agent.command?.trim() ? `独立容器版本命令：${safeMessage(agent.command)} --version` : "缺少独立容器版本命令，不能执行"}{agent.reason ? ` · ${safeMessage(agent.reason)}` : ""}</p></TableCell></TableRow>)}</TableBody></Table> : <Empty><EmptyHeader><EmptyTitle>未发现 Agent Adapter</EmptyTitle><EmptyDescription>请查看 Harbor 诊断。</EmptyDescription></EmptyHeader></Empty>}
    </section>
    <dl className="text-sm"><dt className="text-muted-foreground">Docker Gateway</dt><dd>{inspection.docker_gateway || "未就绪"}</dd></dl>
  </CardContent></Card>;
}

export function EnvironmentsPanel({ api, selectedID, onSelect }: { api: APIClient; selectedID: string; onSelect: (id: string) => void }) {
  const queryClient = useQueryClient();
  const environments = useQuery({ queryKey: ["environments"], queryFn: () => api.environments() });
  const networkProfiles = useQuery({ queryKey: ["network-profiles"], queryFn: () => api.networkProfiles() });
  const [fields, setFields] = useState<EnvironmentFields>(() => {
    const environment = environments.data?.find(environment => environment.id === selectedID);
    return environment ? environmentFields(environment) : blankEnvironment();
  });
  const [errors, setErrors] = useState<FormErrors>({});
  const [busy, setBusy] = useState<"save" | "inspect" | null>(null);
  const [notice, setNotice] = useState("");
  const [operationError, setOperationError] = useState<Error | null>(null);
  const saved = environments.data?.find(environment => environment.id === selectedID);
  useEffect(() => { if (saved && fields.id !== saved.id) setFields(environmentFields(saved)); }, [saved, fields.id]);
  const inspection = useQuery<Inspection | null>({ queryKey: ["inspection", selectedID], queryFn: () => api.inspectEnvironment(selectedID), enabled: false });
  const dirty = !saved || JSON.stringify(fields) !== JSON.stringify(environmentFields(saved));
  const sourceErrors = saved ? environmentNetworkErrors(saved, networkProfiles.data ?? []) : { source: "Save this environment before assigning host-scoped network profiles." };
  function update<K extends keyof EnvironmentFields>(key: K, value: EnvironmentFields[K]) { setFields(current => ({ ...current, [key]: value })); setNotice(""); }
  function choose(id: string) {
    const environment = environments.data?.find(environment => environment.id === id);
    setFields(environment ? environmentFields(environment) : blankEnvironment()); onSelect(environment?.id ?? ""); setErrors({}); setOperationError(null); setNotice("");
  }
  async function save() {
    const result = validateEnvironment(fields, networkProfiles.data ?? []);
    if (!result.ok) { setErrors(result.errors); return; }
    setErrors({}); setBusy("save"); setOperationError(null);
    try {
      const environment = await api.saveEnvironment(result.value);
      queryClient.setQueryData<Environment[]>(["environments"], current => [...(current ?? []).filter(item => item.id !== environment.id), environment]);
      queryClient.setQueryData(["inspection", environment.id], null);
      onSelect(environment.id); setFields(environmentFields(environment)); setNotice("环境已保存。");
    } catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  async function inspect() {
    if (!selectedID || dirty || Object.keys(sourceErrors).length || busy) return;
    setBusy("inspect"); setOperationError(null); setNotice("");
    queryClient.setQueryData(["inspection", selectedID], null);
    try { const result = await api.inspectEnvironment(selectedID); queryClient.setQueryData(["inspection", selectedID], result); }
    catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  const disabled = busy !== null;
  return <div className="flex flex-col gap-6">
    {environments.isError && <ErrorNotice error={environments.error} retry={() => void environments.refetch()} />}
    {environments.isPending && <LoadingState />}
    <Card><CardHeader><CardTitle role="heading" aria-level={2}>连接</CardTitle></CardHeader><CardContent>
      <form id="environment-form" onSubmit={event => { event.preventDefault(); void save(); }}>
        <FieldSet disabled={disabled}><FieldLegend className="sr-only">执行环境</FieldLegend><FieldGroup>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            <Field id="env-profile" label="已保存环境" disabled={disabled}><Select disabled={disabled} value={selectedID || "__new__"} onValueChange={choose}><SelectTrigger id="env-profile" className="w-full"><SelectValue placeholder="新建环境" /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="__new__">新建环境</SelectItem>{environments.data?.map(environment => <SelectItem key={environment.id} value={environment.id}>{environment.name} · {environment.type.toUpperCase()}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
            <TextField id="env-name" label="名称" value={fields.name} onChange={value => update("name", value)} error={errors.name} disabled={disabled} />
            <Field id="env-type" label="连接方式" error={errors.type} disabled={disabled}><Select disabled={disabled} value={fields.type} onValueChange={value => { if (value === "local" || value === "wsl" || value === "ssh") update("type", value); }}><SelectTrigger id="env-type" className="w-full" aria-invalid={Boolean(errors.type)} aria-describedby={errors.type ? "env-type-help" : undefined}><SelectValue /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="local">Local</SelectItem><SelectItem value="wsl">WSL</SelectItem><SelectItem value="ssh">SSH</SelectItem></SelectGroup></SelectContent></Select></Field>
            <TextField id="env-workdir" label="工作目录" value={fields.workDir} onChange={value => update("workDir", value)} disabled={disabled} hint="目标环境路径；留空由后端解析。" />
          </FieldGroup>
          {fields.type === "wsl" && <TextField id="env-distribution" label="WSL 发行版" value={fields.distribution} onChange={value => update("distribution", value)} disabled={disabled} hint="留空使用系统默认发行版。" />}
          {fields.type === "ssh" && <FieldSet><FieldLegend>SSH</FieldLegend><FieldGroup className="grid gap-4 md:grid-cols-2">
            <TextField id="env-host" label="主机" value={fields.host} onChange={value => update("host", value)} error={errors.host} disabled={disabled} />
            <TextField id="env-user" label="用户" value={fields.user} onChange={value => update("user", value)} error={errors.user} disabled={disabled} />
            <TextField id="env-port" label="端口" value={fields.port} onChange={value => update("port", value)} error={errors.port} inputMode="numeric" disabled={disabled} aria-required="true" />
            <Field id="env-arch" label="Linux Worker 架构" error={errors.workerArch} disabled={disabled}><Select disabled={disabled} value={fields.workerArch} onValueChange={value => update("workerArch", value)}><SelectTrigger id="env-arch" className="w-full" aria-invalid={Boolean(errors.workerArch)} aria-describedby={errors.workerArch ? "env-arch-help" : undefined}><SelectValue /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="amd64">amd64</SelectItem><SelectItem value="arm64">arm64</SelectItem></SelectGroup></SelectContent></Select></Field>
            <TextField id="env-identity" label="Identity 文件" value={fields.identityFile} onChange={value => update("identityFile", value)} disabled={disabled} hint="Windows 宿主路径；不保存 SSH 密钥内容。" />
            <TextField id="env-known-hosts" label="known_hosts 文件" value={fields.knownHostsFile} onChange={value => update("knownHostsFile", value)} disabled={disabled} hint="Windows 宿主路径；不自动接受未知指纹。" />
          </FieldGroup></FieldSet>}
          <Separator />
          <Collapsible className="group/tools"><CollapsibleTrigger asChild><Button type="button" variant="ghost" className="w-full justify-between" disabled={disabled}>工具路径<HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" aria-hidden="true" className="transition-transform group-data-[state=open]/tools:rotate-180" /></Button></CollapsibleTrigger><CollapsibleContent forceMount className="pt-4 data-[state=closed]:hidden"><FieldSet><FieldLegend className="sr-only">工具路径</FieldLegend><FieldDescription>留空检查目标 PATH；不安装、升级或重启宿主工具。</FieldDescription><FieldGroup className="grid gap-4 md:grid-cols-2">
            <TextField id="env-harbor" label="Harbor" value={fields.harborPath} onChange={value => update("harborPath", value)} disabled={disabled} />
            <TextField id="env-uv" label="uv" value={fields.uvPath} onChange={value => update("uvPath", value)} disabled={disabled} />
            <TextField id="env-docker" label="Docker" value={fields.dockerPath} onChange={value => update("dockerPath", value)} disabled={disabled} />
          </FieldGroup></FieldSet></CollapsibleContent></Collapsible>
          <Separator />
          <FieldSet><FieldLegend>Execution networks</FieldLegend>{!fields.id && <FieldDescription>Save this environment, then create its host-scoped profiles in Network.</FieldDescription>}<FieldGroup className="grid gap-4 md:grid-cols-2">
            {executionNetworkStages.map(stage => <Field key={stage} id={`env-network-${stage}`} label={stage[0]?.toUpperCase() + stage.slice(1)} error={errors[stage]} disabled={disabled || !fields.id}><NetworkProfileSelect api={api} scope="environment" environmentID={fields.id || undefined} stage={stage} id={`env-network-${stage}`} value={fields.networks[`${stage}_profile_id`]} onValueChange={value => update("networks", { ...fields.networks, [`${stage}_profile_id`]: value })} disabled={disabled || !fields.id} /></Field>)}
          </FieldGroup></FieldSet>
          <FieldSet><FieldLegend>Docker gateway</FieldLegend><FieldGroup className="grid gap-4 md:grid-cols-2">
            <Field id="env-gateway-mode" label="Mode" error={errors.dockerGatewayMode} disabled={disabled}><Select disabled={disabled} value={fields.dockerGatewayMode || "unset"} onValueChange={value => { if (value === "unset" || value === "detect" || value === "manual") { update("dockerGatewayMode", value === "unset" ? "" : value); update("dockerGateway", ""); } }}><SelectTrigger id="env-gateway-mode" className="w-full" aria-invalid={Boolean(errors.dockerGatewayMode)} aria-describedby={errors.dockerGatewayMode ? "env-gateway-mode-help" : undefined}><SelectValue>{fields.dockerGatewayMode === "detect" ? "Detect actual Docker gateway" : fields.dockerGatewayMode === "manual" ? "Manual host address" : "Unconfigured"}</SelectValue></SelectTrigger><SelectContent><SelectGroup><SelectItem value="unset">Unconfigured</SelectItem><SelectItem value="detect">Detect actual Docker gateway</SelectItem><SelectItem value="manual">Manual host address</SelectItem></SelectGroup></SelectContent></Select></Field>
            {fields.dockerGatewayMode === "manual" && <TextField id="env-gateway" label="Host address" value={fields.dockerGateway} onChange={value => update("dockerGateway", value)} error={errors.dockerGateway} disabled={disabled} aria-required="true" />}
          </FieldGroup></FieldSet>
          <LegacyNetworkDiagnostic environment={saved} />
        </FieldGroup></FieldSet>
        {operationError && <div className="mt-4"><ErrorNotice error={operationError} title="环境操作未完成" /></div>}
        {notice && <Alert role="status" className="mt-4"><HugeiconsIcon icon={CheckmarkCircle02Icon} aria-hidden="true" /><AlertTitle>{notice}</AlertTitle></Alert>}
      </form>
    </CardContent><CardFooter className="flex-wrap gap-3"><Button type="submit" form="environment-form" disabled={disabled}><HugeiconsIcon icon={FloppyDiskIcon} data-icon="inline-start" aria-hidden="true" />{busy === "save" ? "保存中…" : "保存环境"}</Button><Button type="button" variant="outline" onClick={() => void inspect()} disabled={!selectedID || dirty || disabled || Object.keys(sourceErrors).length > 0}><HugeiconsIcon icon={Search01Icon} data-icon="inline-start" aria-hidden="true" />{busy === "inspect" ? "检查中…" : "检查环境"}</Button>{dirty && selectedID && <FieldDescription>配置已修改，先保存再检查。</FieldDescription>}{!dirty && selectedID && sourceErrors.source && <FieldDescription>{sourceErrors.source}</FieldDescription>}</CardFooter></Card>
    <InspectionResults inspection={inspection.data} />
  </div>;
}
