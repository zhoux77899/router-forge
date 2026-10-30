import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { HugeiconsIcon } from "@hugeicons/react";
import { Add01Icon, Alert02Icon, Globe02Icon, RefreshIcon } from "@hugeicons/core-free-icons";
import type { APIClient, NetworkCheck } from "@router-forge/api";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Checkbox } from "@router-forge/ui/components/ui/checkbox";
import { Input } from "@router-forge/ui/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Table, TableBody, TableCell, TableRow } from "@router-forge/ui/components/ui/table";
import { Textarea } from "@router-forge/ui/components/ui/textarea";
import { Skeleton } from "@router-forge/ui/components/ui/skeleton";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@router-forge/ui/components/ui/accordion";
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@router-forge/ui/components/ui/field";
import { blankNetwork, changeNetwork, mirrorTools, networkFromRecord, networkToRecord, validateNetwork, type MirrorTool, type NetworkForm, type NetworkProfileRecord, type ProxyTarget } from "../lib/network-forms";

const mirrorNames: Record<MirrorTool, string> = { apt: "apt", pypi: "PyPI", npm: "npm", node: "Node", uv: "uv 索引", uv_python: "uv Python 下载" };
const networkKey = ["network-profiles"];
export type NetworkCheckResult = NetworkCheck;

type ChoiceProps = { id: string; label: string; value: string; options: readonly (readonly [string, string])[]; onChange: (value: string) => void; disabled?: boolean; hideLabel?: boolean };
function Choice({ id, label, value, options, onChange, disabled, hideLabel }: ChoiceProps) {
  return <Field><FieldLabel htmlFor={id} className={hideLabel ? "sr-only" : undefined}>{label}</FieldLabel><Select value={value || undefined} onValueChange={onChange} disabled={disabled}><SelectTrigger id={id}><SelectValue placeholder="选择" /></SelectTrigger><SelectContent><SelectGroup>{options.map(([key, text]) => <SelectItem key={key} value={key}>{text}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>;
}

export function NetworkManager({ api }: { api: APIClient }) {
  const cache = useQueryClient();
  const profiles = useQuery({ queryKey: networkKey, queryFn: () => api.networkProfiles() });
  const environments = useQuery({ queryKey: ["environments"], queryFn: () => api.environments() });
  const [form, setForm] = useState(blankNetwork);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "check">("");
  const [target, setTarget] = useState("");
  const [check, setCheck] = useState<NetworkCheckResult | null>(null);
  const selected = profiles.data?.find((profile) => profile.id === form.id);
  const change = <T extends keyof NetworkForm>(key: T, value: NetworkForm[T]) => {
    setForm((current) => changeNetwork(current, key, value));
    if (key !== "confirmTLSRisk") { setDirty(true); setCheck(null); }
    setError("");
  };
  function choose(profile?: NetworkProfileRecord) {
    setForm(profile ? networkFromRecord(profile) : blankNetwork());
    setDirty(false); setTarget(""); setCheck(null); setError("");
  }
  async function save(event: FormEvent) {
    event.preventDefault(); setError("");
    try {
      const input = networkToRecord(form);
      setBusy("save");
      // Credential inputs never enter a mutation cache or a public profile.
      const saved = await api.saveNetworkProfile(input);
      setForm(networkFromRecord(saved)); setDirty(false); setCheck(null);
      await cache.invalidateQueries({ queryKey: networkKey });
    } catch (failure) { setError(failure instanceof Error ? failure.message : "网络配置保存失败"); }
    finally { setBusy(""); }
  }
  async function inspect() {
    if (!selected || dirty) return;
    setBusy("check"); setError(""); setCheck(null);
    try {
      const result = await api.checkNetworkProfile(selected.id, { profile_revision: selected.revision, target_url: target.trim(), confirm_tls_risk: form.confirmTLSRisk });
      setCheck(result);
    } catch (failure) { setError(failure instanceof Error ? failure.message : "网络检查失败"); }
    finally { setBusy(""); }
  }
  const environmentOptions = environments.data?.map((environment) => [environment.id, environment.name] as const) ?? [];
  const unavailable = busy !== "";
  return <div className="grid gap-6 xl:grid-cols-[16rem_minmax(0,1fr)]">
    <Card className="h-fit"><CardHeader className="flex-row items-center justify-between"><CardTitle>网络配置</CardTitle><Button variant="outline" size="icon" aria-label="新建网络配置" onClick={() => choose()} disabled={unavailable}><HugeiconsIcon icon={Add01Icon} /></Button></CardHeader><CardContent className="grid gap-1">
      {profiles.isPending ? <Skeleton className="h-8 w-full" aria-label="加载网络配置" /> : null}
      {profiles.error ? <FieldError>{profiles.error.message}</FieldError> : null}
      {profiles.data?.map((profile) => <Button key={profile.id} variant={form.id === profile.id ? "secondary" : "ghost"} className="h-auto justify-between gap-3 whitespace-normal py-3 text-left" onClick={() => choose(profile)} disabled={unavailable}><span>{profile.name}</span>{profile.tls_mode === "skip" ? <Badge variant="destructive">TLS 跳过</Badge> : null}</Button>)}
      {profiles.data?.length === 0 ? <Empty className="p-4"><EmptyHeader><EmptyMedia variant="icon"><HugeiconsIcon icon={Globe02Icon} /></EmptyMedia><EmptyTitle>尚无网络配置</EmptyTitle></EmptyHeader></Empty> : null}
    </CardContent></Card>
    <Card><CardHeader><CardTitle>{form.id ? form.name : "新建网络配置"}</CardTitle></CardHeader><CardContent>
      <form onSubmit={save} className="grid gap-6" autoComplete="off">
        <FieldGroup><Field><FieldLabel htmlFor="network-name">名称</FieldLabel><Input id="network-name" value={form.name} onChange={(event) => change("name", event.target.value)} required disabled={unavailable} /></Field>
          <Choice id="network-scope" label="宿主" value={form.scope} options={[["desktop", "Desktop（模型出口）"], ["environment", "执行环境"]]} onChange={(value) => change("scope", value as NetworkForm["scope"])} disabled={unavailable} />
          {form.scope === "environment" ? <Choice id="network-environment" label="环境" value={form.environmentID} options={environmentOptions} onChange={(value) => change("environmentID", value)} disabled={unavailable} /> : null}
          {environments.error ? <FieldError>{environments.error.message}</FieldError> : null}
        </FieldGroup>
        <FieldSet><FieldLegend>Proxy</FieldLegend><FieldGroup>
          <Choice id="network-route" label="连接方式" value={form.proxyMode} options={[["direct", "直连"], ["proxy", "代理"]]} onChange={(value) => change("proxyMode", value as NetworkForm["proxyMode"])} disabled={unavailable} />
          {form.proxyMode === "proxy" ? <>
            <Field><FieldLabel htmlFor="network-proxy">代理 URL</FieldLabel><Input id="network-proxy" type="url" value={form.proxyURL} onChange={(event) => change("proxyURL", event.target.value)} required disabled={unavailable} /></Field>
            <FieldSet><FieldLegend variant="label">适用协议</FieldLegend><div className="flex flex-wrap gap-5">{(["http", "https", "all"] as ProxyTarget[]).map((protocol) => <Field key={protocol} orientation="horizontal"><Checkbox id={`network-proxy-${protocol}`} checked={form.proxyTargets.includes(protocol)} onCheckedChange={(checked) => change("proxyTargets", checked === true ? [...form.proxyTargets, protocol] : form.proxyTargets.filter((value) => value !== protocol))} disabled={unavailable} /><FieldLabel htmlFor={`network-proxy-${protocol}`}>{protocol.toUpperCase()}</FieldLabel></Field>)}</div></FieldSet>
            <Choice id="network-auth" label="代理认证" value={form.proxyAuth} options={[["none", "无认证"], ["basic", "用户名与密码"]]} onChange={(value) => change("proxyAuth", value as NetworkForm["proxyAuth"])} disabled={unavailable} />
            {form.proxyAuth === "basic" ? <>
              <Choice id="network-relay" label="中继范围" value={form.relayMode} options={form.scope === "desktop" ? [["loopback", "仅本机"]] : [["loopback", "仅宿主操作"], ["docker", "所属容器与宿主操作"]]} onChange={(value) => change("relayMode", value as NetworkForm["relayMode"])} disabled={unavailable} />
              <Field><FieldLabel htmlFor="network-username">代理用户名</FieldLabel><Input id="network-username" type="password" autoComplete="off" value={form.username} onChange={(event) => change("username", event.target.value)} disabled={unavailable} /></Field>
              <Field><FieldLabel htmlFor="network-password">代理密码</FieldLabel><Input id="network-password" type="password" autoComplete="new-password" value={form.password} onChange={(event) => change("password", event.target.value)} disabled={unavailable} /><FieldDescription>仅写入，保存在本机明文数据库{form.hasCredentials ? "；留空保留" : ""}。</FieldDescription></Field>
            </> : null}
          </> : null}
          {form.hasCredentials ? <Field orientation="horizontal"><Checkbox id="network-clear-credentials" checked={form.clearCredentials} onCheckedChange={(checked) => change("clearCredentials", checked === true)} disabled={unavailable} /><FieldLabel htmlFor="network-clear-credentials">清除所有已保存代理凭据</FieldLabel></Field> : null}
          <Field><FieldLabel htmlFor="network-no-proxy">NO_PROXY</FieldLabel><Input id="network-no-proxy" value={form.noProxy} onChange={(event) => change("noProxy", event.target.value)} disabled={unavailable} /></Field>
        </FieldGroup></FieldSet>
        {form.scope === "environment" ? <FieldSet><FieldLegend>Mirror</FieldLegend><Accordion type="multiple">{mirrorTools.map((tool) => <AccordionItem key={tool} value={tool}><AccordionTrigger>{mirrorNames[tool]}</AccordionTrigger><AccordionContent><FieldGroup>
          <Choice id={`network-${tool}-mode`} label="下载源" hideLabel value={form.mirrors[tool].mode} options={[["native", "原生源"], ["custom", "自定义源"]]} onChange={(value) => change("mirrors", { ...form.mirrors, [tool]: { ...form.mirrors[tool], mode: value as "native" | "custom" } })} disabled={unavailable} />
          {form.mirrors[tool].mode === "custom" ? <Field><FieldLabel htmlFor={`network-${tool}-value`}>{tool === "apt" ? "apt sources（.list）" : "镜像 URL"}</FieldLabel>{tool === "apt" ? <Textarea id={`network-${tool}-value`} value={form.mirrors[tool].sources} onChange={(event) => change("mirrors", { ...form.mirrors, [tool]: { ...form.mirrors[tool], sources: event.target.value } })} disabled={unavailable} /> : <Input id={`network-${tool}-value`} type="url" value={form.mirrors[tool].url} onChange={(event) => change("mirrors", { ...form.mirrors, [tool]: { ...form.mirrors[tool], url: event.target.value } })} disabled={unavailable} />}</Field> : null}
        </FieldGroup></AccordionContent></AccordionItem>)}</Accordion></FieldSet> : null}
        <FieldSet><FieldLegend>CA / TLS</FieldLegend><FieldGroup>
          <Choice id="network-tls" label="TLS 验证" value={form.tlsMode} options={[["verify", "验证证书"], ["skip", "跳过验证"]]} onChange={(value) => change("tlsMode", value as NetworkForm["tlsMode"])} disabled={unavailable} />
          <Choice id="network-trust" label="证书信任" value={form.trustMode} options={[["system", "目标宿主系统 CA"], ["custom", "系统 CA + 自定义 CA"]]} onChange={(value) => change("trustMode", value as NetworkForm["trustMode"])} disabled={unavailable} />
          {form.trustMode === "custom" ? <Field><FieldLabel htmlFor="network-ca">CA 文件{form.tlsMode === "skip" ? "（保留但忽略）" : ""}</FieldLabel><Input id="network-ca" value={form.caCertPath} onChange={(event) => change("caCertPath", event.target.value)} disabled={unavailable || form.tlsMode === "skip"} /></Field> : null}
          {form.tlsMode === "skip" ? <><Alert variant="destructive"><HugeiconsIcon icon={Alert02Icon} /><AlertTitle>不再验证服务器证书</AlertTitle><AlertDescription>{form.scope === "desktop" ? "作用于绑定模型的 HTTPS 出口。" : "作用于绑定阶段的 Harbor 下载与容器客户端，不放宽 Docker daemon 或 SSH。"}</AlertDescription></Alert><Field orientation="horizontal"><Checkbox id="network-tls-risk" checked={form.confirmTLSRisk} onCheckedChange={(checked) => change("confirmTLSRisk", checked === true)} disabled={unavailable} /><FieldLabel htmlFor="network-tls-risk">我确认该配置与检查目标的 TLS 风险</FieldLabel></Field></> : null}
        </FieldGroup></FieldSet>
        {error ? <FieldError>{error}</FieldError> : null}
        <div className="flex justify-end"><Button type="submit" disabled={unavailable || Boolean(validateNetwork(form))}>{busy === "save" ? "保存中" : "保存"}</Button></div>
      </form>
      {selected ? <Accordion type="single" collapsible className="mt-6"><AccordionItem value="check"><AccordionTrigger>连接检查</AccordionTrigger><AccordionContent className="grid gap-4"><Field><FieldLabel htmlFor="network-check-target">检查 URL</FieldLabel><Input id="network-check-target" type="url" value={target} onChange={(event) => { setTarget(event.target.value); setCheck(null); setForm((current) => ({ ...current, confirmTLSRisk: false })); }} disabled={unavailable || dirty} /></Field><Button variant="outline" onClick={inspect} disabled={unavailable || dirty || !target.trim() || selected.tls_mode === "skip" && !form.confirmTLSRisk}><HugeiconsIcon icon={RefreshIcon} data-icon="inline-start" />{busy === "check" ? "检查中" : "检查"}</Button>
        {check ? <NetworkCheckReadout check={check} /> : null}
      </AccordionContent></AccordionItem></Accordion> : null}
    </CardContent></Card>
  </div>;
}

export function NetworkCheckReadout({ check }: { check: NetworkCheckResult }) {
  return <div className="grid gap-4">
    <Badge variant={check.reachable ? "secondary" : "destructive"} className="w-fit">{check.reachable ? "收到网络响应" : "网络连接失败"}</Badge>
    <Table><TableBody>
      <TableRow><TableCell>HTTP</TableCell><TableCell>{check.http_status ?? "—"}</TableCell></TableRow>
      <TableRow><TableCell>TLS 证书</TableCell><TableCell>{check.tls_verified == null ? "—" : check.tls_verified ? "已验证" : "未验证"}</TableCell></TableRow>
      {check.ca_sha256 ? <TableRow><TableCell>CA SHA-256</TableCell><TableCell><code className="break-all">{check.ca_sha256}</code></TableCell></TableRow> : null}
      <TableRow><TableCell>观察时间</TableCell><TableCell>{check.observed_at}</TableCell></TableRow>
    </TableBody></Table>
    {check.issues.map((issue, index) => <FieldError key={index}>{issue}</FieldError>)}
  </div>;
}

export function NetworkProfileSelect({ api, scope, environmentID, stage, id, value, onValueChange, disabled }: { api: APIClient; scope: "desktop" | "environment"; environmentID?: string; stage?: "source" | "build" | "agent" | "verifier"; id: string; value: string; onValueChange: (value: string) => void; disabled?: boolean }) {
  const profiles = useQuery({ queryKey: networkKey, queryFn: () => api.networkProfiles() });
  const available = profiles.data?.filter((profile) => {
    if (profile.scope !== scope || scope === "environment" && profile.environment_id !== environmentID) return false;
    if (stage === "verifier" && profile.tls_mode !== "verify") return false;
    if ((stage === "agent" || stage === "verifier") && profile.proxy_auth === "basic" && profile.relay_mode !== "docker") return false;
    if (stage === "build" && (profile.proxy_auth === "basic" || profile.tls_mode !== "verify" || profile.trust_mode !== "system" || mirrorTools.some((tool) => profile.mirrors?.[tool]?.mode !== "native"))) return false;
    return true;
  }) ?? [];
  return <><Select value={value || "unconfigured"} onValueChange={(next) => onValueChange(next === "unconfigured" ? "" : next)} disabled={disabled || !environmentID && scope === "environment" || profiles.isPending}><SelectTrigger id={id}><SelectValue placeholder="未配置">{value ? available.find((profile) => profile.id === value)?.name ?? "配置失效" : "未配置"}</SelectValue></SelectTrigger><SelectContent><SelectGroup><SelectItem value="unconfigured">未配置</SelectItem>{available.map((profile) => <SelectItem key={profile.id} value={profile.id}>{profile.name}</SelectItem>)}</SelectGroup></SelectContent></Select>{profiles.error ? <FieldError>{profiles.error.message}</FieldError> : null}</>;
}
