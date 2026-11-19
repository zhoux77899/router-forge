import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { HugeiconsIcon } from "@hugeicons/react";
import { AnalyticsUpIcon, LockKeyIcon, Settings01Icon } from "@hugeicons/core-free-icons";
import { APIClient } from "@router-forge/api";
import { ErrorNotice, ResultsWorkspace, resultsClient } from "@router-forge/ui";
import { PageFrame } from "@router-forge/ui/components/page-frame";
import { ThemeSelector } from "@router-forge/ui/components/theme-selector";
import { Field, FieldGroup, FieldTitle } from "@router-forge/ui/components/ui/field";
import { useAppearance } from "@router-forge/ui/lib/use-appearance";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@router-forge/ui/components/ui/card";
import { RetainedView } from "@router-forge/ui/components/retained-view";
import { Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarMenu, SidebarMenuItem, SidebarMenuButton, SidebarProvider, SidebarInset, SidebarTrigger, useSidebar } from "@router-forge/ui/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@router-forge/ui/components/ui/tooltip";

const client = resultsClient(new APIClient(""));

function WebSidebar({ settings, onSettings }: { settings: boolean; onSettings: (value: boolean) => void }) {
  const { setOpenMobile } = useSidebar();
  function navigate(value: boolean) { onSettings(value); setOpenMobile(false); }
  return <Sidebar collapsible="icon">
    <SidebarHeader className="items-start px-2.5"><SidebarTrigger aria-label="切换导航栏" /></SidebarHeader>
    <SidebarContent><SidebarGroup><SidebarGroupContent><nav aria-label="Web 导航"><SidebarMenu><SidebarMenuItem><SidebarMenuButton type="button" aria-label="结果" isActive={!settings} aria-current={!settings ? "page" : undefined} tooltip="结果" onClick={() => navigate(false)}><HugeiconsIcon icon={AnalyticsUpIcon} aria-hidden="true" /><span>结果</span></SidebarMenuButton></SidebarMenuItem></SidebarMenu></nav></SidebarGroupContent></SidebarGroup></SidebarContent>
    <SidebarFooter><SidebarMenu>
      <SidebarMenuItem><Tooltip><TooltipTrigger asChild><Badge variant="outline" tabIndex={0} aria-label="只读 Web"><HugeiconsIcon icon={LockKeyIcon} aria-hidden="true" /><span className="group-data-[collapsible=icon]:sr-only">只读 Web</span></Badge></TooltipTrigger><TooltipContent>只读 Web</TooltipContent></Tooltip></SidebarMenuItem>
      <SidebarMenuItem><SidebarMenuButton type="button" aria-label="设置" isActive={settings} aria-current={settings ? "page" : undefined} tooltip="设置" onClick={() => navigate(true)}><HugeiconsIcon icon={Settings01Icon} aria-hidden="true" /><span>设置</span></SidebarMenuButton></SidebarMenuItem>
    </SidebarMenu></SidebarFooter>
  </Sidebar>;
}

export function App() {
  const appearance = useAppearance();
  const [settings, setSettings] = useState(false);
  const status = useQuery({ queryKey: ["status"], queryFn: () => client.status(), refetchInterval: 15000 });
  return <TooltipProvider><SidebarProvider className="app-workspace" data-theme={appearance.theme} data-theme-preference={appearance.preference}>
    <WebSidebar settings={settings} onSettings={setSettings} />
    <SidebarInset className="min-h-0 min-w-0"><PageFrame title={settings ? "设置" : "评测结果"} widthClassName="max-w-[1660px]">
      {status.isError && <ErrorNotice error={status.error} retry={() => void status.refetch()} title="无法连接本机后端" />}
      <RetainedView active={!settings}><ResultsWorkspace client={client} /></RetainedView>
      {settings && <Card><CardHeader><CardTitle>外观</CardTitle></CardHeader><CardContent><FieldGroup><Field><FieldTitle>主题模式</FieldTitle><ThemeSelector value={appearance.preference} onValueChange={appearance.setPreference} /></Field></FieldGroup></CardContent></Card>}
    </PageFrame></SidebarInset>
  </SidebarProvider></TooltipProvider>;
}
