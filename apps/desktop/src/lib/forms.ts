import type { Agent, Environment, EnvironmentInput, Inspection, Model, ModelInput, Protocol, Provider, ProviderInput, ResolvedBenchmark, RunSpec, Suite } from "@router-forge/api";

export type FormErrors = Record<string, string>;
export type Validation<T> = { ok: true; value: T } | { ok: false; errors: FormErrors };
const finish = <T>(value: T, errors: FormErrors): Validation<T> => Object.keys(errors).length ? { ok: false, errors } : { ok: true, value };
const clean = (value: string) => value.trim();

export type EnvironmentFields = {
  id: string; name: string; type: "local" | "wsl" | "ssh"; distribution: string; host: string; user: string; port: string;
  identityFile: string; knownHostsFile: string; workDir: string; workerOS: string; workerArch: string;
  harborPath: string; uvPath: string; dockerPath: string; proxyURL: string; noProxy: string; caCertPath: string; dockerGateway: string;
};

export const blankEnvironment = (): EnvironmentFields => ({
  id: "", name: "", type: "wsl", distribution: "", host: "", user: "", port: "22", identityFile: "", knownHostsFile: "", workDir: "", workerOS: "linux", workerArch: "amd64",
  harborPath: "", uvPath: "", dockerPath: "", proxyURL: "", noProxy: "", caCertPath: "", dockerGateway: "",
});

export function environmentFields(environment: Environment): EnvironmentFields {
  return { ...blankEnvironment(), id: environment.id, name: environment.name, type: environment.type === "ssh" ? "ssh" : environment.type === "local" ? "local" : "wsl",
    distribution: environment.distribution ?? "", host: environment.host ?? "", user: environment.user ?? "", port: String(environment.port || 22),
    identityFile: environment.identity_file ?? "", knownHostsFile: environment.known_hosts_file ?? "", workDir: environment.work_dir ?? "", workerOS: environment.worker_os ?? "linux", workerArch: environment.worker_arch ?? "amd64",
    harborPath: environment.tools?.harbor ?? "", uvPath: environment.tools?.uv ?? "", dockerPath: environment.tools?.docker ?? "", proxyURL: environment.proxy_url ?? "", noProxy: environment.no_proxy ?? "", caCertPath: environment.ca_cert_path ?? "", dockerGateway: environment.docker_host_gateway ?? "",
  };
}

function validHTTPURL(value: string): boolean {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password && !url.hash && ![...url.searchParams.keys()].some(key => /key|token|auth|password/i.test(key));
  } catch { return false; }
}

export function validateEnvironment(fields: EnvironmentFields): Validation<EnvironmentInput> {
  const errors: FormErrors = {};
  if (!clean(fields.name)) errors.name = "请为环境填写可识别的名称。";
  if (!["local", "wsl", "ssh"].includes(fields.type)) errors.type = "请选择 Local、WSL 或 SSH。";
  let port: number | undefined;
  if (fields.type === "ssh") {
    if (!clean(fields.host) || /[\s/@]/.test(fields.host)) errors.host = "请填写 SSH 主机名或 IP，不含 URL、用户名或空白。";
    if (!clean(fields.user) || /\s/.test(fields.user)) errors.user = "请填写 SSH 用户名。";
    port = Number(fields.port);
    if (!/^\d+$/.test(fields.port) || !Number.isInteger(port) || port < 1 || port > 65535) errors.port = "SSH 端口必须为 1–65535 的整数。";
    if (fields.workerOS !== "linux") errors.workerOS = "首版 SSH Harbor 执行环境需要 Linux。";
    if (!["amd64", "arm64"].includes(fields.workerArch)) errors.workerArch = "请选择实际 worker 架构。";
  }
  if (fields.proxyURL && !validHTTPURL(fields.proxyURL)) errors.proxyURL = "代理应为 HTTP(S) 地址，不能在 URL 内嵌入凭据。";
  const tools: Record<string, string> = {};
  for (const [key, path] of [["harbor", fields.harborPath], ["uv", fields.uvPath], ["docker", fields.dockerPath]]) if (path?.trim()) tools[key ?? ""] = path.trim();
  return finish({
    ...(fields.id ? { id: fields.id } : {}), name: clean(fields.name), type: fields.type,
    ...(fields.type === "wsl" && clean(fields.distribution) ? { distribution: clean(fields.distribution) } : {}),
    ...(fields.type === "ssh" ? { host: clean(fields.host), user: clean(fields.user), port, worker_os: fields.workerOS, worker_arch: fields.workerArch, identity_file: clean(fields.identityFile), known_hosts_file: clean(fields.knownHostsFile) } : {}),
    work_dir: clean(fields.workDir), tools, proxy_url: clean(fields.proxyURL), no_proxy: clean(fields.noProxy), ca_cert_path: clean(fields.caCertPath), docker_host_gateway: clean(fields.dockerGateway),
  }, errors);
}

export type ProviderFields = { id: string; name: string; baseURL: string };
export function validateProvider(fields: ProviderFields, newKey: string): Validation<ProviderInput> {
  const errors: FormErrors = {};
  if (!clean(fields.name)) errors.name = "请填写模型服务商名称。";
  if (!validHTTPURL(fields.baseURL)) errors.baseURL = "请填写 HTTP(S) 服务根地址，密钥必须在单独字段写入。";
  if (!fields.id && !newKey.trim()) errors.apiKey = "新服务商需要写入密钥；不进行模型调用验证。";
  if (newKey && /\s/.test(newKey)) errors.apiKey = "密钥不能含空白或换行；不会自动截取或记录输入值。";
  return finish({ ...(fields.id ? { id: fields.id } : {}), name: clean(fields.name), base_url: clean(fields.baseURL), ...(newKey ? { api_key: newKey } : {}) }, errors);
}

export type ModelFields = {
  id: string; providerID: string; name: string; displayName: string; protocol: Protocol; priceEnabled: boolean;
  currency: string; inputPrice: string; outputPrice: string; cacheReadPrice: string; cacheWritePrice: string; priceSource: string;
};
export const blankModel = (): ModelFields => ({ id: "", providerID: "", name: "", displayName: "", protocol: "chat_completions", priceEnabled: false, currency: "USD", inputPrice: "", outputPrice: "", cacheReadPrice: "", cacheWritePrice: "", priceSource: "" });
export function modelFields(model: Model): ModelFields {
  return { ...blankModel(), id: model.id, providerID: model.provider_id, name: model.name, displayName: model.display_name, protocol: model.protocol, priceEnabled: Boolean(model.pricing), currency: model.pricing?.currency ?? "USD", inputPrice: model.pricing?.input_per_million ?? "", outputPrice: model.pricing?.output_per_million ?? "", cacheReadPrice: model.pricing?.cache_read_per_million ?? "", cacheWritePrice: model.pricing?.cache_write_per_million ?? "", priceSource: model.pricing?.source ?? "" };
}
export function exactPrice(value: string): boolean { return value.length <= 128 && /^\d+(?:\.\d+)?$/.test(value); }
export function validateModel(fields: ModelFields, providers: readonly Provider[]): Validation<ModelInput> {
  const errors: FormErrors = {};
  if (!providers.some(provider => provider.id === fields.providerID)) errors.providerID = "请选择已保存的服务商。";
  if (!clean(fields.name)) errors.name = "请填写服务商实际接受的上游模型名称。";
  if (!clean(fields.displayName)) errors.displayName = "请填写模型显示名称。";
  if (!["chat_completions", "responses", "anthropic_messages"].includes(fields.protocol)) errors.protocol = "请选择模型的原生协议。";
  if (fields.priceEnabled) {
    if (!/^[A-Z]{3,12}$/.test(clean(fields.currency))) errors.currency = "请填写大写币种代码，如 USD 或 CNY。";
    for (const field of ["inputPrice", "outputPrice", "cacheReadPrice", "cacheWritePrice"] as const) if ((field === "inputPrice" || field === "outputPrice" || fields[field]) && !exactPrice(fields[field])) errors[field] = "价格需为非负十进制字符串，不使用浮点科学计数或负数。";
    if (!clean(fields.priceSource)) errors.priceSource = "请填写此价格的来源或依据。";
  }
  return finish({ ...(fields.id ? { id: fields.id } : {}), provider_id: fields.providerID, name: clean(fields.name), display_name: clean(fields.displayName), protocol: fields.protocol,
    ...(fields.priceEnabled ? { pricing: { currency: clean(fields.currency), input_per_million: fields.inputPrice, output_per_million: fields.outputPrice, ...(fields.cacheReadPrice ? { cache_read_per_million: fields.cacheReadPrice } : {}), ...(fields.cacheWritePrice ? { cache_write_per_million: fields.cacheWritePrice } : {}), source: clean(fields.priceSource) } } : {}),
  }, errors);
}

export function agentReasons(agent: Agent, protocol: Protocol | undefined): string[] {
  const reasons: string[] = [];
  if (!agent.harbor_supported) reasons.push("当前 Harbor 不支持此 Adapter");
  if (!agent.command?.trim()) reasons.push("缺少可独立观测实际容器 CLI 版本的运行命令");
  if (protocol && !agent.supported_protocols?.includes(protocol)) reasons.push("不支持选中模型的原生协议");
  return reasons;
}

export type RunFields = { environmentID: string; modelID: string; taskName: string; agentName: string; kwargsText: string; timeout: string; setupTimeout: string; maxRequests: string; maxOutputTokens: string };
export const blankRun = (): RunFields => ({ environmentID: "", modelID: "", taskName: "", agentName: "", kwargsText: "{}", timeout: "1800", setupTimeout: "900", maxRequests: "100", maxOutputTokens: "4096" });
export type RunChoices = { environments: readonly Environment[]; models: readonly Model[]; providers: readonly Provider[]; suites: readonly Suite[]; resolution?: ResolvedBenchmark; inspection?: Inspection };

function boundedInteger(value: string, max: number): number | undefined {
  const number = Number(value);
  return /^\d+$/.test(value) && Number.isSafeInteger(number) && number >= 1 && number <= max ? number : undefined;
}
function object(value: unknown): value is Record<string, unknown> { return Boolean(value) && typeof value === "object" && !Array.isArray(value); }

function validateKwargs(text: string, agent: Agent | undefined): Validation<NonNullable<RunSpec["agent_kwargs"]>> {
  let value: unknown;
  try { value = JSON.parse(text); } catch { return { ok: false, errors: { kwargsText: "Agent 参数必须是有效 JSON 对象。" } }; }
  if (!object(value)) return { ok: false, errors: { kwargsText: "请使用 JSON 对象，不使用数组、null 或单值。" } };
  const schema = object(agent?.schema) ? agent.schema : undefined;
  const properties = object(schema?.properties) ? schema.properties : undefined;
  for (const [key, option] of Object.entries(value)) {
    const normalized = key.replace(/([a-z])([A-Z])/g, "$1_$2").toLowerCase();
    if (/(^|[_-])(version|model|base|url|api|auth|env|config|prompt|mcp|skills|token|tokens|password|secret|path|cwd|dir|directory|workdir)([_-]|$)/.test(normalized)) return { ok: false, errors: { kwargsText: `参数 ${key} 会覆盖受控模型、凭据、版本或执行配置，不允许使用。` } };
    if (option == null || !["string", "number", "boolean"].includes(typeof option) || typeof option === "number" && !Number.isFinite(option)) return { ok: false, errors: { kwargsText: `参数 ${key} 只允许字符串、有限数值或布尔值。` } };
    if (!properties || !Object.hasOwn(properties, key)) return { ok: false, errors: { kwargsText: `所检查的 Agent 未声明标量参数 ${key}，请参考实际 schema。` } };
    const property = properties[key];
    if (object(property)) {
      const type = property.type;
      if ((type === "string" && typeof option !== "string") || (type === "boolean" && typeof option !== "boolean") || ((type === "number" || type === "integer") && typeof option !== "number") || type === "integer" && !Number.isInteger(option)) return { ok: false, errors: { kwargsText: `参数 ${key} 与 Agent schema 类型不匹配。` } };
      if (Array.isArray(property.enum) && !property.enum.includes(option)) return { ok: false, errors: { kwargsText: `参数 ${key} 不在 Agent schema 的可选值内。` } };
    }
  }
  return { ok: true, value };
}

export function validateRun(fields: RunFields, choices: RunChoices): Validation<RunSpec> {
  const errors: FormErrors = {};
  if (!choices.environments.some(environment => environment.id === fields.environmentID)) errors.environmentID = "请选择已保存的真实执行环境。";
  if (!choices.inspection || choices.inspection.environment_id !== fields.environmentID || !choices.inspection.ready) errors.environmentID = "此环境尚未通过实际检查，请先检查并修复诊断问题。";
  const model = choices.models.find(model => model.id === fields.modelID);
  if (!model) errors.modelID = "请选择已保存的固定模型。";
  else if (!choices.providers.find(provider => provider.id === model.provider_id)?.has_key) errors.modelID = "此模型的服务商尚未保存密钥。";
  const dataset = "swe-bench/swe-bench-verified";
  if (!choices.suites.some(suite => suite.id === "swebench-verified" && suite.dataset === dataset)
    || choices.resolution?.suite.id !== "swebench-verified" || choices.resolution.suite.dataset !== dataset) errors.taskName = `请在所选环境解析唯一支持的数据集 ${dataset}。`;
  else if (!fields.taskName || !choices.resolution.tasks.some(task => task.name === fields.taskName)) errors.taskName = "请从该环境实际返回的任务预览中选择完整、精确的任务名称，不能替换命名空间。";
  const agent = choices.inspection?.agents?.find(agent => agent.name === fields.agentName);
  if (!agent) errors.agentName = "请从所选环境检查到的 Agent 中选择。";
  else if (agentReasons(agent, model?.protocol).length) errors.agentName = agentReasons(agent, model?.protocol).join("；");
  const kwargs = validateKwargs(fields.kwargsText, agent);
  if (!kwargs.ok) Object.assign(errors, kwargs.errors);
  const timeout = boundedInteger(fields.timeout, 86400), setup = boundedInteger(fields.setupTimeout, 86400), requests = boundedInteger(fields.maxRequests, 10000), output = boundedInteger(fields.maxOutputTokens, 1000000);
  if (!timeout) errors.timeout = "执行超时必须为 1–86400 秒整数。";
  if (!setup) errors.setupTimeout = "Setup 超时必须为 1–86400 秒整数。";
  if (!requests) errors.maxRequests = "请求上限必须为 1–10000 的整数。";
  if (!output) errors.maxOutputTokens = "输出上限必须为 1–1000000 Token 的整数。";
  return finish({ environment_id: fields.environmentID, model_id: fields.modelID, suite_id: "swebench-verified", task_names: [fields.taskName], agent: fields.agentName, agent_kwargs: kwargs.ok ? kwargs.value : {}, timeout_sec: timeout ?? 0, setup_timeout_sec: setup ?? 0, max_requests: requests ?? 0, max_output_tokens: output ?? 0, concurrency: 1, attempts: 1, confirm_paid: false }, errors);
}
