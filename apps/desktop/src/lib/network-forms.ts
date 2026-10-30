import type { NetworkProfile, NetworkProfileInput } from "@router-forge/api";

export type NetworkScope = "" | "desktop" | "environment";
export type NetworkRoute = "" | "direct" | "proxy";
export type ProxyTarget = "http" | "https" | "all";
export type MirrorTool = "apt" | "pypi" | "npm" | "node" | "uv" | "uv_python";
export type MirrorChoice = { mode: "" | "native" | "custom"; url: string; sources: string };

export type NetworkForm = {
  id: string;
  revision: string;
  name: string;
  scope: NetworkScope;
  environmentID: string;
  proxyMode: NetworkRoute;
  proxyURL: string;
  proxyTargets: ProxyTarget[];
  proxyAuth: "" | "none" | "basic";
  noProxy: string;
  relayMode: "" | "loopback" | "docker";
  tlsMode: "" | "verify" | "skip";
  trustMode: "" | "system" | "custom";
  caCertPath: string;
  mirrors: Record<MirrorTool, MirrorChoice>;
  hasCredentials: boolean;
  credentialRevision: string;
  createdAt: string;
  username: string;
  password: string;
  clearCredentials: boolean;
  confirmTLSRisk: boolean;
};

export const mirrorTools: MirrorTool[] = ["apt", "pypi", "npm", "node", "uv", "uv_python"];

export type NetworkProfileRecord = NetworkProfile;
export type NetworkProfileInputRecord = NetworkProfileInput;

export function networkFromRecord(profile: NetworkProfileRecord): NetworkForm {
  const form = blankNetwork();
  Object.assign(form, {
    id: profile.id, revision: profile.revision, name: profile.name, scope: profile.scope,
    environmentID: profile.environment_id ?? "", proxyMode: profile.proxy_mode,
    proxyURL: profile.proxy_url ?? "", proxyTargets: [...profile.proxy_targets ?? []],
    proxyAuth: profile.proxy_auth ?? "", noProxy: profile.no_proxy ?? "", relayMode: profile.relay_mode ?? "",
    tlsMode: profile.tls_mode, trustMode: profile.trust_mode, caCertPath: profile.ca_cert_path ?? "",
    hasCredentials: profile.has_credentials, credentialRevision: profile.credential_revision ?? "", createdAt: profile.created_at,
  });
  for (const tool of mirrorTools) {
    const choice = profile.mirrors?.[tool];
    if (choice) form.mirrors[tool] = { mode: choice.mode, url: choice.url ?? "", sources: choice.sources ?? "" };
  }
  return form;
}

export function networkToRecord(form: NetworkForm): NetworkProfileInputRecord {
  const error = validateNetwork(form);
  if (error) throw new Error(error);
  const mirrors = form.scope === "environment" ? Object.fromEntries(mirrorTools.map((tool) => {
    const choice = form.mirrors[tool];
    return [tool, { mode: choice.mode, ...(choice.mode === "custom" ? tool === "apt" ? { sources: choice.sources } : { url: choice.url.trim() } : {}) }];
  })) as NetworkProfileRecord["mirrors"] : undefined;
  return {
    profile: {
      id: form.id, revision: form.revision, name: form.name.trim(), scope: form.scope as "desktop" | "environment",
      ...(form.scope === "environment" ? { environment_id: form.environmentID } : {}),
      proxy_mode: form.proxyMode as "direct" | "proxy",
      ...(form.proxyMode === "proxy" ? { proxy_url: form.proxyURL.trim(), proxy_targets: [...form.proxyTargets], proxy_auth: form.proxyAuth as "none" | "basic", ...(form.proxyAuth === "basic" ? { relay_mode: form.relayMode as "loopback" | "docker" } : {}) } : {}),
      no_proxy: form.noProxy.trim(), tls_mode: form.tlsMode as "verify" | "skip", trust_mode: form.trustMode as "system" | "custom",
      ca_cert_path: form.caCertPath.trim(), ...(mirrors ? { mirrors } : {}),
      has_credentials: form.hasCredentials, credential_revision: form.credentialRevision, tls_risk_accepted: false, created_at: form.createdAt,
    },
    ...(form.proxyMode === "proxy" && form.proxyAuth === "basic" ? {
      ...(form.username ? { proxy_username: form.username } : {}),
      ...(form.password ? { proxy_password: form.password } : {}),
    } : {}),
    clear_credentials: form.clearCredentials,
    confirm_tls_risk: form.tlsMode === "skip" && form.confirmTLSRisk,
  };
}

export function blankNetwork(): NetworkForm {
  return {
    id: "", revision: "", name: "", scope: "", environmentID: "",
    proxyMode: "", proxyURL: "", proxyTargets: [], proxyAuth: "", noProxy: "", relayMode: "",
    tlsMode: "", trustMode: "", caCertPath: "",
    mirrors: Object.fromEntries(mirrorTools.map((tool) => [tool, { mode: "", url: "", sources: "" }])) as Record<MirrorTool, MirrorChoice>,
    hasCredentials: false, credentialRevision: "", createdAt: "",
    username: "", password: "", clearCredentials: false, confirmTLSRisk: false,
  };
}

function plainURL(value: string, proxy = false): boolean {
  try {
    const url = new URL(value);
    if (!["http:", "https:"].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash || /[\s\u0000-\u001f]/.test(value)) return false;
    return !proxy || url.pathname === "/";
  } catch { return false; }
}

export function validateNetwork(form: NetworkForm): string | undefined {
  if (!form.name.trim()) return "填写网络配置名称";
  if (!form.scope) return "选择配置宿主";
  if (form.scope === "environment" && !form.environmentID) return "选择执行环境";
  if (!form.proxyMode) return "选择直连或代理";
  if (form.proxyMode === "proxy") {
    if (!plainURL(form.proxyURL, true)) return "填写不含凭据的 HTTP(S) 代理地址";
    if (!form.proxyTargets.length) return "选择代理适用协议";
    if (!form.proxyAuth) return "选择代理认证方式";
    if (form.proxyAuth === "basic") {
      if (!form.hasCredentials || form.clearCredentials) {
        if (!form.username || !form.password) return "填写代理用户名和密码";
      }
      if (/[\r\n\u0000:]/.test(form.username) || /[\r\n\u0000]/.test(form.password)) return "代理凭据包含无效字符";
      if (!form.relayMode || form.scope === "desktop" && form.relayMode !== "loopback") return "选择适用的代理中继范围";
    }
  }
  if (/[\r\n\u0000]/.test(form.noProxy + form.caCertPath)) return "网络字段包含无效字符";
  if (!form.tlsMode) return "选择 TLS 验证模式";
  if (!form.trustMode) return "选择证书信任方式";
  if (form.tlsMode === "skip" && !form.confirmTLSRisk) return "确认跳过 TLS 验证的风险";
  if (form.tlsMode === "verify" && form.trustMode === "custom" && !form.caCertPath.trim()) return "填写目标宿主上的 CA 文件路径";
  if (form.scope === "environment") {
    for (const tool of mirrorTools) {
      const choice = form.mirrors[tool];
      if (!choice.mode) return `选择 ${tool} 下载源`;
      if (choice.mode === "custom" && tool === "apt" && !choice.sources.trim()) return "填写 apt sources";
      if (choice.mode === "custom" && tool !== "apt" && !plainURL(choice.url)) return `填写不含凭据的 ${tool} 镜像地址`;
    }
  }
  return undefined;
}

export function changeNetwork<T extends keyof NetworkForm>(form: NetworkForm, key: T, value: NetworkForm[T]): NetworkForm {
  const next = { ...form, [key]: value };
  if (key !== "confirmTLSRisk") next.confirmTLSRisk = false;
  if (key === "scope") {
    next.environmentID = "";
    next.relayMode = "";
  }
  if (key === "proxyAuth" && value === "none") {
    next.relayMode = "";
    next.username = "";
    next.password = "";
  }
  if (key === "proxyMode" && value === "direct") {
    next.proxyURL = "";
    next.proxyTargets = [];
    next.proxyAuth = "";
    next.relayMode = "";
    next.username = "";
    next.password = "";
  }
  return next;
}
