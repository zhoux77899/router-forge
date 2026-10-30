import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Model, type Provider } from "@router-forge/api";
import { Check, KeyRound, Save } from "lucide-react";
import { ErrorNotice, errorMessage } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Checkbox } from "@router-forge/ui/components/ui/checkbox";
import { Label } from "@router-forge/ui/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Field, TextField } from "./fields";
import { blankModel, modelFields, validateModel, validateProvider, type FormErrors, type ModelFields, type ProviderFields } from "../lib/forms";

export function ServicesPanel({ api }: { api: APIClient }) {
  const queryClient = useQueryClient();
  const providers = useQuery({ queryKey: ["providers"], queryFn: () => api.providers() });
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models() });
  const [provider, setProvider] = useState<ProviderFields>({ id: "", name: "", baseURL: "" });
  const [newKey, setNewKey] = useState("");
  const [model, setModel] = useState<ModelFields>(blankModel);
  const [providerErrors, setProviderErrors] = useState<FormErrors>({});
  const [modelErrors, setModelErrors] = useState<FormErrors>({});
  const [savingProvider, setSavingProvider] = useState(false);
  const [savingModel, setSavingModel] = useState(false);
  const [providerError, setProviderError] = useState<Error | null>(null);
  const [modelError, setModelError] = useState<Error | null>(null);
  const [notice, setNotice] = useState("");
  const currentProvider = providers.data?.find(item => item.id === provider.id);
  function chooseProvider(id: string) {
    const item = providers.data?.find(item => item.id === id);
    setProvider(item ? { id: item.id, name: item.name, baseURL: item.base_url } : { id: "", name: "", baseURL: "" }); setNewKey(""); setProviderErrors({}); setProviderError(null); setNotice("");
  }
  function updateProvider(key: keyof ProviderFields, value: string) { setProvider(fields => ({ ...fields, [key]: value })); setNotice(""); }
  function updateModel<K extends keyof ModelFields>(key: K, value: ModelFields[K]) { setModel(fields => ({ ...fields, [key]: value })); setNotice(""); }
  async function saveProvider() {
    const submittedKey = newKey;
    const validated = validateProvider(provider, submittedKey);
    if (!validated.ok) { setProviderErrors(validated.errors); return; }
    setNewKey(""); setProviderErrors({}); setSavingProvider(true); setProviderError(null); setNotice("");
    // A direct write avoids retaining full key values as TanStack mutation variables.
    try {
      const item = await api.saveProvider(validated.value);
      queryClient.setQueryData<Provider[]>(["providers"], current => [...(current ?? []).filter(value => value.id !== item.id), item]);
      setProvider({ id: item.id, name: item.name, baseURL: item.base_url });
      setNotice("服务商已保存。密钥已清空输入框；保存不会调用模型。");
    } catch (error) { setProviderError(new Error(errorMessage(error, [submittedKey]))); }
    finally { setSavingProvider(false); }
  }
  async function saveModel() {
    const validated = validateModel(model, providers.data ?? []);
    if (!validated.ok) { setModelErrors(validated.errors); return; }
    setModelErrors({}); setSavingModel(true); setModelError(null); setNotice("");
    try {
      const item = await api.saveModel(validated.value);
      queryClient.setQueryData<Model[]>(["models"], current => [...(current ?? []).filter(value => value.id !== item.id), item]);
      setModel(modelFields(item)); setNotice("固定模型配置已保存。既有运行快照不会被改写。");
    } catch (error) { setModelError(new Error(errorMessage(error))); } finally { setSavingModel(false); }
  }
  return <div className="space-y-6">
    <div><h2 className="text-xl font-semibold">模型服务</h2><p className="mt-1 text-sm text-muted-foreground">保存原生协议与固定模型配置，不自动探测或调用付费模型服务。</p></div>
    <Alert><KeyRound aria-hidden="true" /><AlertTitle>密钥仅写入 · 明文 SQLite</AlertTitle><AlertDescription>API Key 保存在 Windows 宿主的权限保护 SQLite 文件中，不加密。保存后只返回密钥提示与引用，不读取完整密钥，不写入浏览器存储、查询缓存或日志。</AlertDescription></Alert>
    {providers.isError && <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />}
    <Card><CardHeader><CardTitle className="text-base">服务商与写入凭据</CardTitle></CardHeader><CardContent><form onSubmit={event => { event.preventDefault(); void saveProvider(); }} className="space-y-4">
      <fieldset disabled={savingProvider} className="grid gap-4 md:grid-cols-2">
        <Field id="provider-profile" label="已保存服务商"><Select value={provider.id || "__new__"} onValueChange={chooseProvider}><SelectTrigger id="provider-profile" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="__new__">新建服务商</SelectItem>{providers.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.name} · {item.has_key ? "已有密钥" : "无密钥"}</SelectItem>)}</SelectContent></Select></Field>
        <TextField id="provider-name" label="服务商名称" value={provider.name} onChange={value => updateProvider("name", value)} error={providerErrors.name} />
        <TextField id="provider-base-url" label="服务 Base URL" value={provider.baseURL} onChange={value => updateProvider("baseURL", value)} error={providerErrors.baseURL} hint="例如原生 /v1 根地址；不在 URL 中拼接密钥或授权参数。" />
        <TextField id="provider-key-write" label={currentProvider?.has_key ? "替换 API Key（可留空）" : "写入 API Key"} type="password" autoComplete="new-password" value={newKey} onChange={setNewKey} error={providerErrors.apiKey} hint={currentProvider?.has_key ? `已保存：${currentProvider.key_hint || "存在密钥"}。留空保留已有密钥；完整值不可读取。` : "仅在保存时发送到本机原生桥接；不会验证或调用模型。"} />
      </fieldset>
      {providerError && <ErrorNotice error={providerError} title="服务商保存失败" />}
      <Button type="submit" disabled={savingProvider || providers.isPending}><Save className="size-4" aria-hidden="true" />{savingProvider ? "写入中…" : "保存服务商"}</Button>
    </form></CardContent></Card>
    {models.isError && <ErrorNotice error={models.error} retry={() => void models.refetch()} />}
    <Card><CardHeader><CardTitle className="text-base">固定模型与可选价格</CardTitle></CardHeader><CardContent><form onSubmit={event => { event.preventDefault(); void saveModel(); }} className="space-y-4">
      <fieldset disabled={savingModel} className="space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <Field id="model-profile" label="已保存模型"><Select value={model.id || "__new__"} onValueChange={id => { const item = models.data?.find(item => item.id === id); setModel(item ? modelFields(item) : blankModel()); setModelErrors({}); setModelError(null); }}><SelectTrigger id="model-profile" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="__new__">新建固定模型</SelectItem>{models.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.display_name || item.name} · {protocolLabels[item.protocol]}</SelectItem>)}</SelectContent></Select></Field>
          <Field id="model-provider" label="服务商" error={modelErrors.providerID}><Select value={model.providerID || undefined} onValueChange={value => updateModel("providerID", value)}><SelectTrigger id="model-provider" className="w-full" aria-invalid={Boolean(modelErrors.providerID)} aria-describedby="model-provider-help"><SelectValue placeholder="选择已保存服务商" /></SelectTrigger><SelectContent>{providers.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}</SelectContent></Select></Field>
          <TextField id="model-upstream" label="上游模型名称" value={model.name} onChange={value => updateModel("name", value)} error={modelErrors.name} hint="服务商实际接受的名称；运行时不静默切换其他模型。" />
          <TextField id="model-display" label="显示名称" value={model.displayName} onChange={value => updateModel("displayName", value)} error={modelErrors.displayName} />
          <Field id="model-protocol" label="原生 API 协议" error={modelErrors.protocol}><Select value={model.protocol} onValueChange={value => { if (value === "chat_completions" || value === "responses" || value === "anthropic_messages") updateModel("protocol", value); }}><SelectTrigger id="model-protocol" className="w-full"><SelectValue /></SelectTrigger><SelectContent>{Object.entries(protocolLabels).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectContent></Select></Field>
        </div>
        <div className="flex items-center gap-2"><Checkbox id="model-pricing-enabled" checked={model.priceEnabled} onCheckedChange={value => updateModel("priceEnabled", value === true)} /><Label htmlFor="model-pricing-enabled">配置精确十进制价格（可选）</Label></div>
        {model.priceEnabled ? <div className="grid gap-4 rounded-md border bg-muted/30 p-4 md:grid-cols-2">
          <TextField id="model-currency" label="币种代码" value={model.currency} onChange={value => updateModel("currency", value)} error={modelErrors.currency} />
          <TextField id="model-price-source" label="价格来源" value={model.priceSource} onChange={value => updateModel("priceSource", value)} error={modelErrors.priceSource} hint="填写文档链接或定价依据；账本保留此来源。" />
          <TextField id="model-price-input" label="每百万输入 Token 价格" inputMode="decimal" value={model.inputPrice} onChange={value => updateModel("inputPrice", value)} error={modelErrors.inputPrice} />
          <TextField id="model-price-output" label="每百万输出 Token 价格" inputMode="decimal" value={model.outputPrice} onChange={value => updateModel("outputPrice", value)} error={modelErrors.outputPrice} />
          <TextField id="model-price-cache-read" label="每百万缓存读取价格（可空）" inputMode="decimal" value={model.cacheReadPrice} onChange={value => updateModel("cacheReadPrice", value)} error={modelErrors.cacheReadPrice} hint="未配置就是未知，不按普通输入价格伪造。" />
          <TextField id="model-price-cache-write" label="每百万缓存写入价格（可空）" inputMode="decimal" value={model.cacheWritePrice} onChange={value => updateModel("cacheWritePrice", value)} error={modelErrors.cacheWritePrice} />
        </div> : <p className="text-xs text-muted-foreground">未配置价格时，成本可能未知；不显示伪造的零费用。</p>}
      </fieldset>
      {modelError && <ErrorNotice error={modelError} title="模型保存失败" />}
      <Button type="submit" disabled={savingModel || !providers.data?.length}><Save className="size-4" aria-hidden="true" />{savingModel ? "保存中…" : "保存固定模型"}</Button>
    </form></CardContent></Card>
    {notice && <p role="status" className="flex items-center gap-2 text-sm text-primary"><Check className="size-4" aria-hidden="true" />{notice}</p>}
  </div>;
}
