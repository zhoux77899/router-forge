import { useQuery } from "@tanstack/react-query";
import { LockKeyhole, Router } from "lucide-react";
import { APIClient } from "@router-forge/api";
import { ResultsWorkspace, resultsClient } from "@router-forge/ui";
import { Badge } from "@router-forge/ui/components/ui/badge";

const client = resultsClient(new APIClient(""));

export function App() {
  const status = useQuery({ queryKey: ["status"], queryFn: () => client.status(), refetchInterval: 15000 });
  return <main className="mx-auto min-h-screen max-w-[1660px] px-4 py-6 md:px-8">
    <header className="mb-7 flex flex-wrap items-start justify-between gap-4 border-b pb-5">
      <div className="flex items-start gap-3"><div className="mt-1 rounded-md bg-primary p-2 text-primary-foreground"><Router className="size-5" aria-hidden="true" /></div><div><h1 className="text-2xl">评测结果</h1><p className="mt-1 text-sm text-muted-foreground">Router Forge · 本机评测的用量、成本与执行证据</p></div></div>
      <div className="flex flex-col items-end gap-2"><Badge variant="outline" className="gap-1.5 bg-card"><LockKeyhole className="size-3" aria-hidden="true" />只读 Web</Badge><span role="status" className="text-xs text-muted-foreground">{status.isError ? "后端不可达 · 请检查桌面连接" : status.data ? "本机后端已连接" : "正在连接本机后端…"}</span></div>
    </header>
    <ResultsWorkspace client={client} />
    <footer className="mt-8 border-t pt-4 text-xs text-muted-foreground">配置、创建草稿、启动与取消仅在桌面控制台提供。网页不接收服务商密钥，不展示模型原始提示或输出。</footer>
  </main>;
}
