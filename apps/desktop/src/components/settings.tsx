import { useState } from "react";
import { HugeiconsIcon } from "@hugeicons/react";
import { ArrowReloadHorizontalIcon } from "@hugeicons/core-free-icons";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@router-forge/ui/components/ui/tabs";
import { RetainedView } from "@router-forge/ui/components/retained-view";
import { EnvironmentsPanel } from "./environments";
import { ServicesPanel } from "./services";
import { NetworkManager } from "./networks";
import { ThemeToggle, type Theme } from "../App";
import type { DesktopConnection } from "../lib/native-client";

const categories = [
  ["appearance", "外观"], ["environments", "执行环境"], ["models", "模型"], ["network", "网络"], ["diagnostics", "诊断"],
] as const;

export function DesktopSettings({ desktop, environmentID, onEnvironment, theme, onTheme, reconnect, statusError, backendRunning, version, pid }: {
  desktop: DesktopConnection; environmentID: string; onEnvironment: (id: string) => void;
  theme: Theme; onTheme: () => void; reconnect: () => void; statusError: boolean;
  backendRunning?: boolean; version?: string; pid?: number;
}) {
  const [category, setCategory] = useState("appearance");
  const { api, connection } = desktop;
  return <Tabs value={category} onValueChange={setCategory} className="gap-6">
    <TabsList aria-label="设置分类" className="h-auto max-w-full flex-wrap justify-start">
      {categories.map(([id, label]) => <TabsTrigger key={id} value={id}>{label}</TabsTrigger>)}
    </TabsList>
    {categories.map(([id]) => <TabsContent key={id} value={id} forceMount className="data-[state=inactive]:hidden">
      <RetainedView active={category === id}>
        {id === "appearance" && <Card><CardHeader><CardTitle>外观</CardTitle></CardHeader><CardContent className="flex items-center justify-between gap-4"><span>深色模式</span><ThemeToggle theme={theme} onToggle={onTheme} /></CardContent></Card>}
        {id === "environments" && <EnvironmentsPanel api={api} selectedID={environmentID} onSelect={onEnvironment} />}
        {id === "models" && <ServicesPanel api={api} />}
        {id === "network" && <NetworkManager api={api} />}
        {id === "diagnostics" && <Card><CardHeader><CardTitle>后端诊断</CardTitle></CardHeader><CardContent className="flex flex-col gap-4">
          <Badge variant={statusError || backendRunning === false ? "destructive" : "outline"}>{statusError ? "后端不可达" : backendRunning == null ? "读取状态…" : backendRunning ? "MyGo 已附着" : "后端未运行"}</Badge>
          <dl className="flex flex-col gap-3"><div><dt className="text-muted-foreground">版本 / PID</dt><dd>{version || connection.version} / {pid || connection.pid}</dd></div><div><dt className="text-muted-foreground">后端 URL</dt><dd className="break-all">{connection.url}</dd></div><div><dt className="text-muted-foreground">数据目录</dt><dd className="break-all">{connection.data_dir || "由本机后端管理"}</dd></div></dl>
          <div className="flex flex-wrap gap-3"><Button type="button" size="sm" variant="outline" onClick={reconnect}><HugeiconsIcon icon={ArrowReloadHorizontalIcon} data-icon="inline-start" aria-hidden="true" />重新附着</Button><Button size="sm" variant="outline" asChild><a href={connection.url} target="_blank" rel="noopener noreferrer">打开只读结果 Web</a></Button></div>
        </CardContent></Card>}
      </RetainedView>
    </TabsContent>)}
  </Tabs>;
}
