import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { HugeiconsIcon } from "@hugeicons/react";
import { AnalyticsUpIcon, LockKeyIcon, Moon02Icon, Settings01Icon, SidebarLeft01Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { APIClient } from "@router-forge/api";
import { ErrorNotice, ResultsWorkspace, resultsClient } from "@router-forge/ui";
import { Button } from "@router-forge/ui/components/ui/button";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { RetainedView } from "@router-forge/ui/components/retained-view";
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarMenu, SidebarMenuItem, SidebarMenuButton, SidebarProvider, SidebarInset, useSidebar } from "@router-forge/ui/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@router-forge/ui/components/ui/tooltip";

const client = resultsClient(new APIClient(""));

export function ThemeToggle({ dark, onToggle }: { dark: boolean; onToggle: () => void }) {
  return <Tooltip>
    <TooltipTrigger asChild><Button type="button" size="icon" variant="ghost" aria-label="深色主题" aria-pressed={dark} onClick={onToggle}>
      <HugeiconsIcon icon={dark ? Sun03Icon : Moon02Icon} data-icon="inline-start" aria-hidden="true" />
    </Button></TooltipTrigger>
    <TooltipContent>{dark ? "切换为浅色主题" : "切换为深色主题"}</TooltipContent>
  </Tooltip>;
}

function WebSidebar({ settings, onSettings }: { settings: boolean; onSettings: (value: boolean) => void }) {
  const { toggleSidebar, setOpenMobile } = useSidebar();
  function navigate(value: boolean) { onSettings(value); setOpenMobile(false); }
  return <Sidebar collapsible="icon">
    <SidebarContent><SidebarGroup><SidebarGroupContent><nav aria-label="Web 导航"><SidebarMenu><SidebarMenuItem><SidebarMenuButton type="button" aria-label="结果" isActive={!settings} aria-current={!settings ? "page" : undefined} tooltip="结果" onClick={() => navigate(false)}><HugeiconsIcon icon={AnalyticsUpIcon} aria-hidden="true" /><span>结果</span></SidebarMenuButton></SidebarMenuItem></SidebarMenu></nav></SidebarGroupContent></SidebarGroup></SidebarContent>
    <SidebarFooter><SidebarMenu>
      <SidebarMenuItem><Tooltip><TooltipTrigger asChild><Badge variant="outline" tabIndex={0} aria-label="只读 Web"><HugeiconsIcon icon={LockKeyIcon} aria-hidden="true" /><span className="group-data-[collapsible=icon]:sr-only">只读 Web</span></Badge></TooltipTrigger><TooltipContent>只读 Web</TooltipContent></Tooltip></SidebarMenuItem>
      <SidebarMenuItem><SidebarMenuButton type="button" aria-label="设置" isActive={settings} aria-current={settings ? "page" : undefined} tooltip="设置" onClick={() => navigate(true)}><HugeiconsIcon icon={Settings01Icon} aria-hidden="true" /><span>设置</span></SidebarMenuButton></SidebarMenuItem>
      <SidebarMenuItem><SidebarMenuButton type="button" aria-label="切换导航栏" tooltip="切换导航栏" onClick={toggleSidebar}><HugeiconsIcon icon={SidebarLeft01Icon} aria-hidden="true" /><span>切换导航栏</span></SidebarMenuButton></SidebarMenuItem>
    </SidebarMenu></SidebarFooter>
  </Sidebar>;
}

export function App() {
  const [dark, setDark] = useState(() => typeof window !== "undefined" && (window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ?? false));
  const [settings, setSettings] = useState(false);
  useEffect(() => { document.documentElement.classList.toggle("dark", dark); document.documentElement.style.colorScheme = dark ? "dark" : "light"; }, [dark]);
  const status = useQuery({ queryKey: ["status"], queryFn: () => client.status(), refetchInterval: 15000 });
  return <TooltipProvider><SidebarProvider data-theme={dark ? "dark" : "light"}>
    <WebSidebar settings={settings} onSettings={setSettings} />
    <SidebarInset className="min-w-0"><div className="mx-auto flex w-full max-w-[1660px] flex-col gap-6 px-4 py-6 md:px-8">
      <h1 className="font-semibold">{settings ? "设置" : "评测结果"}</h1>
      {status.isError && <ErrorNotice error={status.error} retry={() => void status.refetch()} title="无法连接本机后端" />}
      <RetainedView active={!settings}><ResultsWorkspace client={client} /></RetainedView>
      {settings && <Card><CardHeader><CardTitle>外观</CardTitle></CardHeader><CardContent className="flex items-center justify-between gap-4"><span>深色主题</span><ThemeToggle dark={dark} onToggle={() => setDark(value => !value)} /></CardContent></Card>}
    </div></SidebarInset>
  </SidebarProvider></TooltipProvider>;
}
