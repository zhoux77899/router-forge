import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { APIClient, protocolLabels, type Model, type Provider } from "@router-forge/api";
import { HugeiconsIcon } from "@hugeicons/react";
import { CheckmarkCircle02Icon, FloppyDiskIcon, Key01Icon } from "@hugeicons/core-free-icons";
import { ErrorNotice, errorMessage } from "@router-forge/ui";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Checkbox } from "@router-forge/ui/components/ui/checkbox";
import { FieldDescription, FieldGroup, FieldLegend, FieldSet } from "@router-forge/ui/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@router-forge/ui/components/ui/select";
import { Separator } from "@router-forge/ui/components/ui/separator";
import { Field, TextField } from "./fields";
import { blankModel, blankProvider, modelFields, providerFields, validateModel, validateProvider, type FormErrors, type ModelFields, type ProviderFields } from "../lib/forms";
import { NetworkProfileSelect } from "./networks";

export function ServicesPanel({ api }: { api: APIClient }) {
  const queryClient = useQueryClient();
  const providers = useQuery({ queryKey: ["providers"], queryFn: () => api.providers() });
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models() });
  const networkProfiles = useQuery({ queryKey: ["network-profiles"], queryFn: () => api.networkProfiles() });
  const [provider, setProvider] = useState<ProviderFields>(blankProvider);
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
    setProvider(item ? providerFields(item) : blankProvider()); setNewKey(""); setProviderErrors({}); setProviderError(null); setNotice("");
  }
  function updateProvider(key: keyof ProviderFields, value: string) { setProvider(fields => ({ ...fields, [key]: value })); setNotice(""); }
  function updateModel<K extends keyof ModelFields>(key: K, value: ModelFields[K]) { setModel(fields => ({ ...fields, [key]: value })); setNotice(""); }
  async function saveProvider() {
    const submittedKey = newKey;
    const validated = validateProvider(provider, submittedKey, networkProfiles.data ?? []);
    if (!validated.ok) { setProviderErrors(validated.errors); return; }
    setNewKey(""); setProviderErrors({}); setSavingProvider(true); setProviderError(null); setNotice("");
    // A direct write avoids retaining full key values as TanStack mutation variables.
    try {
      const item = await api.saveProvider(validated.value);
      queryClient.setQueryData<Provider[]>(["providers"], current => [...(current ?? []).filter(value => value.id !== item.id), item]);
      setProvider(providerFields(item));
      setNotice("服务商已保存，密钥输入已清空。");
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
      setModel(modelFields(item)); setNotice("模型已保存；既有快照不变。");
    } catch (error) { setModelError(new Error(errorMessage(error))); } finally { setSavingModel(false); }
  }
  return <div className="flex flex-col gap-6">
    <Alert><HugeiconsIcon icon={Key01Icon} aria-hidden="true" /><AlertTitle>密钥仅写入 · 明文 SQLite</AlertTitle><AlertDescription>API Key 保存在 Windows 宿主的权限保护 SQLite 文件中，不加密。只返回密钥提示与引用，不读取完整密钥，不写入浏览器存储、查询缓存或日志。保存不会调用模型。</AlertDescription></Alert>
    {providers.isError && <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />}
    <Card><CardHeader><CardTitle role="heading" aria-level={2}>服务商</CardTitle></CardHeader><CardContent><form id="provider-form" onSubmit={event => { event.preventDefault(); void saveProvider(); }}>
      <FieldSet disabled={savingProvider}><FieldLegend className="sr-only">服务商与凭据</FieldLegend><FieldGroup className="grid gap-4 md:grid-cols-2">
        <Field id="provider-profile" label="已保存服务商" disabled={savingProvider}><Select disabled={savingProvider} value={provider.id || "__new__"} onValueChange={chooseProvider}><SelectTrigger id="provider-profile" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="__new__">新建服务商</SelectItem>{providers.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.name} · {item.has_key ? "已有密钥" : "无密钥"}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
        <TextField id="provider-name" label="名称" value={provider.name} onChange={value => updateProvider("name", value)} error={providerErrors.name} disabled={savingProvider} />
        <TextField id="provider-base-url" label="Base URL" value={provider.baseURL} onChange={value => updateProvider("baseURL", value)} error={providerErrors.baseURL} disabled={savingProvider} />
        <TextField id="provider-key-write" label={currentProvider?.has_key ? "替换 API Key" : "API Key"} type="password" autoComplete="new-password" value={newKey} onChange={setNewKey} error={providerErrors.apiKey} disabled={savingProvider} hint={currentProvider?.has_key ? `已保存：${currentProvider.key_hint || "存在密钥"}。留空保留；完整值不可读取。` : "仅写入，不验证或调用模型。"} />
        <Field id="provider-network" label="Model network (Desktop)" error={providerErrors.networkProfileID} disabled={savingProvider}><NetworkProfileSelect api={api} scope="desktop" id="provider-network" value={provider.networkProfileID ?? ""} onValueChange={value => updateProvider("networkProfileID", value)} disabled={savingProvider} /></Field>
      </FieldGroup></FieldSet>
      {providerError && <div className="mt-4"><ErrorNotice error={providerError} title="服务商保存失败" /></div>}
    </form></CardContent><CardFooter><Button type="submit" form="provider-form" disabled={savingProvider || providers.isPending}><HugeiconsIcon icon={FloppyDiskIcon} data-icon="inline-start" aria-hidden="true" />{savingProvider ? "写入中…" : "保存服务商"}</Button></CardFooter></Card>
    {models.isError && <ErrorNotice error={models.error} retry={() => void models.refetch()} />}
    <Card><CardHeader><CardTitle role="heading" aria-level={2}>固定模型</CardTitle></CardHeader><CardContent><form id="model-form" onSubmit={event => { event.preventDefault(); void saveModel(); }}>
      <FieldSet disabled={savingModel}><FieldLegend className="sr-only">固定模型配置</FieldLegend><FieldGroup>
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field id="model-profile" label="已保存模型" disabled={savingModel}><Select disabled={savingModel} value={model.id || "__new__"} onValueChange={id => { const item = models.data?.find(item => item.id === id); setModel(item ? modelFields(item) : blankModel()); setModelErrors({}); setModelError(null); setNotice(""); }}><SelectTrigger id="model-profile" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="__new__">新建固定模型</SelectItem>{models.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.display_name || item.name} · {protocolLabels[item.protocol]}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
          <Field id="model-provider" label="服务商" error={modelErrors.providerID} disabled={savingModel}><Select disabled={savingModel} value={model.providerID || undefined} onValueChange={value => updateModel("providerID", value)}><SelectTrigger id="model-provider" className="w-full" aria-invalid={Boolean(modelErrors.providerID)} aria-describedby={modelErrors.providerID ? "model-provider-help" : undefined}><SelectValue placeholder="选择服务商" /></SelectTrigger><SelectContent><SelectGroup>{providers.data?.map(item => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
          <TextField id="model-upstream" label="上游模型名称" value={model.name} onChange={value => updateModel("name", value)} error={modelErrors.name} disabled={savingModel} />
          <TextField id="model-display" label="显示名称" value={model.displayName} onChange={value => updateModel("displayName", value)} error={modelErrors.displayName} disabled={savingModel} />
          <Field id="model-protocol" label="原生协议" error={modelErrors.protocol} disabled={savingModel}><Select disabled={savingModel} value={model.protocol} onValueChange={value => { if (value === "chat_completions" || value === "responses" || value === "anthropic_messages") updateModel("protocol", value); }}><SelectTrigger id="model-protocol" className="w-full" aria-invalid={Boolean(modelErrors.protocol)} aria-describedby={modelErrors.protocol ? "model-protocol-help" : undefined}><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{Object.entries(protocolLabels).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
        </FieldGroup>
        <Separator />
        <FieldSet><FieldLegend>价格</FieldLegend><FieldGroup>
          <Field id="model-pricing-enabled" label="配置价格" orientation="horizontal" disabled={savingModel}><Checkbox id="model-pricing-enabled" checked={model.priceEnabled} onCheckedChange={value => updateModel("priceEnabled", value === true)} disabled={savingModel} /></Field>
          {model.priceEnabled ? <><FieldDescription>每百万 Token；缓存价格留空表示未知，不按普通输入价格替代。</FieldDescription><FieldGroup className="grid gap-4 md:grid-cols-2">
            <TextField id="model-currency" label="币种" value={model.currency} onChange={value => updateModel("currency", value)} error={modelErrors.currency} disabled={savingModel} />
            <TextField id="model-price-source" label="价格来源" value={model.priceSource} onChange={value => updateModel("priceSource", value)} error={modelErrors.priceSource} disabled={savingModel} />
            <TextField id="model-price-input" label="输入" inputMode="decimal" value={model.inputPrice} onChange={value => updateModel("inputPrice", value)} error={modelErrors.inputPrice} disabled={savingModel} />
            <TextField id="model-price-output" label="输出" inputMode="decimal" value={model.outputPrice} onChange={value => updateModel("outputPrice", value)} error={modelErrors.outputPrice} disabled={savingModel} />
            <TextField id="model-price-cache-read" label="缓存读取" inputMode="decimal" value={model.cacheReadPrice} onChange={value => updateModel("cacheReadPrice", value)} error={modelErrors.cacheReadPrice} disabled={savingModel} />
            <TextField id="model-price-cache-write" label="缓存写入" inputMode="decimal" value={model.cacheWritePrice} onChange={value => updateModel("cacheWritePrice", value)} error={modelErrors.cacheWritePrice} disabled={savingModel} />
          </FieldGroup></> : <FieldDescription>未配置价格，成本可能未知；不按零费用显示。</FieldDescription>}
        </FieldGroup></FieldSet>
      </FieldGroup></FieldSet>
      {modelError && <div className="mt-4"><ErrorNotice error={modelError} title="模型保存失败" /></div>}
    </form></CardContent><CardFooter><Button type="submit" form="model-form" disabled={savingModel || !providers.data?.length}><HugeiconsIcon icon={FloppyDiskIcon} data-icon="inline-start" aria-hidden="true" />{savingModel ? "保存中…" : "保存固定模型"}</Button></CardFooter></Card>
    {notice && <Alert role="status"><HugeiconsIcon icon={CheckmarkCircle02Icon} aria-hidden="true" /><AlertTitle>{notice}</AlertTitle></Alert>}
  </div>;
}
