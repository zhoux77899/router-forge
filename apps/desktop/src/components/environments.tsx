import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Environment, type Inspection } from "@router-forge/api";
import { Check, Search, Server, Save } from "lucide-react";
import { ErrorNotice, LoadingState, errorMessage, safeMessage } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@router-forge/ui/components/ui/table";
import { Field, TextField } from "./fields";
import { blankEnvironment, environmentFields, validateEnvironment, type EnvironmentFields, type FormErrors } from "../lib/forms";

export function InspectionResults({ inspection }: { inspection: Inspection | null | undefined }) {
  if (!inspection) return <p className="rounded-md border border-dashed p-5 text-sm text-muted-foreground">尚未检查此环境。保存配置不会安装、升级或重启宿主工具。</p>;
  return <div className="space-y-5">
    <Alert variant={inspection.ready ? "default" : "destructive"}><Server aria-hidden="true" /><AlertTitle>{inspection.ready ? "环境检查通过" : "环境尚未就绪"}</AlertTitle><AlertDescription><p>{inspection.os || "OS 未知"} / {inspection.arch || "架构未知"} · 工作目录：{inspection.work_dir || "未知"}</p>{inspection.issues?.length > 0 && <ul className="list-inside list-disc space-y-1">{inspection.issues.map((issue, index) => <li key={index}>{issue}</li>)}</ul>}</AlertDescription></Alert>
    <section className="space-y-2"><h3 className="font-semibold">宿主工具与真实诊断</h3><div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow><TableHead>工具</TableHead><TableHead>可执行性</TableHead><TableHead>版本 / 路径</TableHead><TableHead>诊断</TableHead></TableRow></TableHeader><TableBody>{(inspection.tools ?? []).map(tool => <TableRow key={tool.name}><TableCell>{tool.name}</TableCell><TableCell><Badge variant={tool.available ? "secondary" : "outline"}>{tool.available ? "可用" : "不可用"}</Badge></TableCell><TableCell><span className="block">{safeMessage(tool.version || (tool.version_output ? "未提取数值版本" : "版本未报告"))}</span><code className="block break-all text-xs text-muted-foreground">{tool.path || "路径未知"}</code>{tool.version_output && <pre className="mt-1 whitespace-pre-wrap break-words text-xs text-muted-foreground">{safeMessage(tool.version_output)}</pre>}</TableCell><TableCell className="max-w-96 whitespace-normal text-xs">{tool.detail || "—"}</TableCell></TableRow>)}</TableBody></Table></div></section>
    <section className="space-y-2"><h3 className="font-semibold">发现的 Agent 与 Harbor 能力</h3><p className="text-xs text-muted-foreground">宿主 CLI 路径与版本仅是提示，不代表容器版本。Harbor 使用默认容器安装；成功独立观测实际容器 CLI 版本并保存确认后才允许推理。</p>
      {(inspection.agents ?? []).length ? <div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow><TableHead>Agent</TableHead><TableHead>宿主安装 / 版本</TableHead><TableHead>Harbor / 原生协议</TableHead><TableHead>容器安装与限制</TableHead></TableRow></TableHeader><TableBody>{inspection.agents.map(agent => <TableRow key={agent.name}><TableCell className="font-medium">{agent.name}<code className="mt-1 block break-all text-xs text-muted-foreground">{agent.path || agent.command}</code></TableCell><TableCell>{agent.installed ? "已发现" : "宿主未安装"}<span className="block text-xs text-muted-foreground">{agent.version || "版本未报告"}</span></TableCell><TableCell><Badge variant="outline">{agent.harbor_supported ? "Adapter 支持" : "Adapter 不支持"}</Badge><p className="mt-1 text-xs">{agent.supported_protocols?.map(protocol => protocolLabels[protocol]).join(" / ") || "无已确认协议"}</p></TableCell><TableCell className="max-w-96 whitespace-normal text-xs">{agent.container_install || "未声明安装方式"}<p className="text-muted-foreground">{agent.command?.trim() ? `独立容器版本命令：${agent.command} --version` : "缺少独立容器版本命令，不能执行"}{agent.reason ? ` · ${agent.reason}` : ""}</p></TableCell></TableRow>)}</TableBody></Table></div> : <p className="rounded-md border border-dashed p-5 text-sm text-muted-foreground">没有发现可用 Agent Adapter。请查看宿主 Harbor 检查结果。</p>}
    </section>
    <p className="text-xs text-muted-foreground">Docker Gateway: {inspection.docker_gateway || "未就绪"}。首版 SWE 原生 Windows 执行能力由后端如实检查；当前建议使用已具备 Linux Docker/Harbor 的 WSL 环境。</p>
  </div>;
}

export function EnvironmentsPanel({ api, selectedID, onSelect }: { api: APIClient; selectedID: string; onSelect: (id: string) => void }) {
  const queryClient = useQueryClient();
  const environments = useQuery({ queryKey: ["environments"], queryFn: () => api.environments() });
  const [fields, setFields] = useState<EnvironmentFields>(() => blankEnvironment());
  const [errors, setErrors] = useState<FormErrors>({});
  const [busy, setBusy] = useState<"save" | "inspect" | null>(null);
  const [notice, setNotice] = useState("");
  const [operationError, setOperationError] = useState<Error | null>(null);
  const saved = environments.data?.find(environment => environment.id === selectedID);
  useEffect(() => { if (saved && fields.id !== saved.id) setFields(environmentFields(saved)); }, [saved, fields.id]);
  const inspection = useQuery<Inspection | null>({ queryKey: ["inspection", selectedID], queryFn: () => api.inspectEnvironment(selectedID), enabled: false });
  const dirty = !saved || JSON.stringify(fields) !== JSON.stringify(environmentFields(saved));
  function update<K extends keyof EnvironmentFields>(key: K, value: EnvironmentFields[K]) { setFields(current => ({ ...current, [key]: value })); setNotice(""); }
  function choose(id: string) {
    const environment = environments.data?.find(environment => environment.id === id);
    setFields(environment ? environmentFields(environment) : blankEnvironment()); onSelect(environment?.id ?? ""); setErrors({}); setOperationError(null); setNotice("");
  }
  async function save() {
    const result = validateEnvironment(fields);
    if (!result.ok) { setErrors(result.errors); return; }
    setErrors({}); setBusy("save"); setOperationError(null);
    try {
      const environment = await api.saveEnvironment(result.value);
      queryClient.setQueryData<Environment[]>(["environments"], current => [...(current ?? []).filter(item => item.id !== environment.id), environment]);
      queryClient.setQueryData(["inspection", environment.id], null);
      onSelect(environment.id); setFields(environmentFields(environment)); setNotice("环境已保存。请检查已保存配置，再创建运行草稿。");
    } catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  async function inspect() {
    if (!selectedID || dirty) return;
    setBusy("inspect"); setOperationError(null); setNotice("");
    try { const result = await api.inspectEnvironment(selectedID); queryClient.setQueryData(["inspection", selectedID], result); }
    catch (error) { setOperationError(new Error(errorMessage(error))); } finally { setBusy(null); }
  }
  return <div className="space-y-6">
    <div className="panel-heading"><div><h2 className="text-xl font-semibold">执行环境</h2><p className="mt-1 text-sm text-muted-foreground">连接本机、WSL 或 SSH，检查真实工具与 Agent 能力；不自动安装宿主软件。</p></div></div>
    {environments.isError && <ErrorNotice error={environments.error} retry={() => void environments.refetch()} />}
    {environments.isPending && <LoadingState />}
    <Card><CardHeader><CardTitle className="text-base">环境配置</CardTitle></CardHeader><CardContent>
      <form onSubmit={event => { event.preventDefault(); void save(); }} className="space-y-5">
        <fieldset disabled={Boolean(busy)} className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2"><Field id="env-profile" label="已保存环境" hint="选择已有配置，或新建；切换不会启动任务。"><Select value={selectedID || "__new__"} onValueChange={choose}><SelectTrigger id="env-profile" className="w-full"><SelectValue placeholder="新建环境" /></SelectTrigger><SelectContent><SelectItem value="__new__">新建环境</SelectItem>{environments.data?.map(environment => <SelectItem key={environment.id} value={environment.id}>{environment.name} · {environment.type.toUpperCase()}</SelectItem>)}</SelectContent></Select></Field><TextField id="env-name" label="环境名称" value={fields.name} onChange={value => update("name", value)} error={errors.name} placeholder="例如：开发 WSL" /></div>
          <div className="grid gap-4 md:grid-cols-2"><Field id="env-type" label="连接方式" error={errors.type}><Select value={fields.type} onValueChange={value => { if (value === "local" || value === "wsl" || value === "ssh") update("type", value); }}><SelectTrigger id="env-type" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="local">Local · 本机原生</SelectItem><SelectItem value="wsl">WSL · Linux 发行版</SelectItem><SelectItem value="ssh">SSH · Linux Server</SelectItem></SelectContent></Select></Field><TextField id="env-workdir" label="执行环境工作目录" value={fields.workDir} onChange={value => update("workDir", value)} hint="使用目标环境路径；Windows 与 Linux 路径不能混用。留空由后端解析。" /></div>
          {fields.type === "wsl" && <TextField id="env-distribution" label="WSL 发行版名称" value={fields.distribution} onChange={value => update("distribution", value)} hint="留空使用系统默认发行版；填写实际 wsl --list 返回的名称。" />}
          {fields.type === "ssh" && <div className="grid gap-4 md:grid-cols-2">
            <TextField id="env-host" label="SSH 主机" value={fields.host} onChange={value => update("host", value)} error={errors.host} /><TextField id="env-user" label="SSH 用户" value={fields.user} onChange={value => update("user", value)} error={errors.user} />
            <TextField id="env-port" label="SSH 端口" value={fields.port} onChange={value => update("port", value)} error={errors.port} inputMode="numeric" /><Field id="env-arch" label="远端 Linux Worker 架构" error={errors.workerArch}><Select value={fields.workerArch} onValueChange={value => update("workerArch", value)}><SelectTrigger id="env-arch" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="amd64">Linux amd64</SelectItem><SelectItem value="arm64">Linux arm64</SelectItem></SelectContent></Select></Field>
            <TextField id="env-identity" label="Identity 文件（Windows 宿主路径）" value={fields.identityFile} onChange={value => update("identityFile", value)} hint="仅引用用户已配置的 SSH 身份文件；不保存 SSH 密钥内容。" /><TextField id="env-known-hosts" label="known_hosts 文件（Windows 宿主路径）" value={fields.knownHostsFile} onChange={value => update("knownHostsFile", value)} hint="保留严格主机验证，不自动接受未知指纹。" />
          </div>}
          <details className="rounded-md border p-4" open><summary className="cursor-pointer font-medium">环境集成 · 路径、网络与 CA</summary><div className="mt-4 grid gap-4 md:grid-cols-2">
            <TextField id="env-harbor" label="Harbor 可执行路径" value={fields.harborPath} onChange={value => update("harborPath", value)} hint="目标环境原生路径；留空检查目标 PATH 中实际安装的 Harbor，不设置数值版本门槛。" /><TextField id="env-uv" label="uv 可执行路径" value={fields.uvPath} onChange={value => update("uvPath", value)} hint="目标环境原生路径；不安装宿主工具。" />
            <TextField id="env-docker" label="Docker 可执行路径" value={fields.dockerPath} onChange={value => update("dockerPath", value)} hint="检查 Docker daemon 可访问性，不启动或重启 daemon。" /><TextField id="env-gateway" label="Docker 宿主可达地址" value={fields.dockerGateway} onChange={value => update("dockerGateway", value)} hint="供容器访问受控网关；留空由检查解析并验证。" />
            <TextField id="env-proxy" label="HTTP(S) 代理 URL" value={fields.proxyURL} onChange={value => update("proxyURL", value)} error={errors.proxyURL} hint="目标环境代理，不在 URL 嵌入凭据。" /><TextField id="env-no-proxy" label="NO_PROXY" value={fields.noProxy} onChange={value => update("noProxy", value)} hint="本机、Gateway 与内网地址应按实际网络配置。" />
            <TextField id="env-ca" label="CA 证书路径" value={fields.caCertPath} onChange={value => update("caCertPath", value)} hint="目标环境中已存在的可信 CA 文件；不禁用 TLS。" />
          </div></details>
        </fieldset>
        {operationError && <ErrorNotice error={operationError} title="环境操作未完成" />}
        {notice && <p role="status" className="flex items-center gap-2 text-sm text-primary"><Check className="size-4" aria-hidden="true" />{notice}</p>}
        <div className="flex flex-wrap items-center gap-3"><Button type="submit" disabled={Boolean(busy)}><Save className="size-4" aria-hidden="true" />{busy === "save" ? "保存中…" : "保存环境"}</Button><Button type="button" variant="outline" onClick={() => void inspect()} disabled={!selectedID || dirty || Boolean(busy)}><Search className="size-4" aria-hidden="true" />{busy === "inspect" ? "实际检查中…" : "检查已保存环境"}</Button>{dirty && selectedID && <span className="text-xs text-muted-foreground">配置已修改，请先保存再检查。</span>}</div>
      </form>
    </CardContent></Card>
    <InspectionResults inspection={inspection.data} />
  </div>;
}
