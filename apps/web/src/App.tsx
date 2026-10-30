import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { HugeiconsIcon } from "@hugeicons/react";
import { LockKeyIcon, Moon02Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { APIClient } from "@router-forge/api";
import { ErrorNotice, ResultsWorkspace, resultsClient } from "@router-forge/ui";
import { Badge } from "@router-forge/ui/components/ui/badge";
import { Button } from "@router-forge/ui/components/ui/button";
import { Separator } from "@router-forge/ui/components/ui/separator";
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

export function App() {
  const [dark, setDark] = useState(() => typeof window !== "undefined" && (window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ?? false));
  useEffect(() => { document.documentElement.classList.toggle("dark", dark); }, [dark]);
  const status = useQuery({ queryKey: ["status"], queryFn: () => client.status(), refetchInterval: 15000 });
  return <TooltipProvider><main data-theme={dark ? "dark" : "light"} className="mx-auto flex min-h-screen max-w-[1660px] flex-col gap-6 px-4 py-6 md:px-8">
    <header className="flex flex-wrap items-center justify-between gap-4">
      <h1 className="text-xl">评测结果</h1>
      <div className="flex items-center gap-2">
        <Badge variant="outline"><HugeiconsIcon icon={LockKeyIcon} aria-hidden="true" />只读 Web</Badge>
        <ThemeToggle dark={dark} onToggle={() => setDark(value => !value)} />
      </div>
    </header>
    <Separator />
    {status.isError && <ErrorNotice error={status.error} retry={() => void status.refetch()} title="无法连接本机后端" />}
    <ResultsWorkspace client={client} />
  </main></TooltipProvider>;
}
