import type { ReactNode } from "react";
import { HugeiconsIcon } from "@hugeicons/react";
import { AlertCircleIcon, ArrowDown01Icon, InboxIcon, Shield01Icon } from "@hugeicons/core-free-icons";
import { Alert, AlertDescription, AlertTitle } from "@router-forge/ui/components/ui/alert";
import { Button } from "@router-forge/ui/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@router-forge/ui/components/ui/collapsible";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@router-forge/ui/components/ui/empty";
import { Skeleton } from "@router-forge/ui/components/ui/skeleton";
import { errorMessage } from "../lib/format";

export function ErrorNotice({ error, retry, title = "无法读取数据" }: { error: unknown; retry?: () => void; title?: string }) {
  return <Alert variant="destructive" role="alert">
    <HugeiconsIcon icon={AlertCircleIcon} aria-hidden="true" />
    <AlertTitle>{title}</AlertTitle>
    <AlertDescription className="break-words">
      <p>{errorMessage(error)}</p>
      {retry && <Button size="sm" variant="outline" onClick={retry}>重新读取</Button>}
    </AlertDescription>
  </Alert>;
}

export function HarborTLSWarning() {
  return <Alert variant="destructive">
    <HugeiconsIcon icon={Shield01Icon} aria-hidden="true" />
    <AlertTitle>跳过 Harbor TLS 验证（不安全）</AlertTitle>
    <AlertDescription>
      <p>启用后不验证服务器身份，中间人（MITM）可能替换元数据、下载内容或安装脚本。仅在可信测试网络临时使用；默认关闭。</p>
      <Collapsible className="flex flex-col gap-2">
        <CollapsibleTrigger asChild>
          <Button size="sm" variant="ghost" className="group w-fit">
            影响范围
            <HugeiconsIcon icon={ArrowDown01Icon} data-icon="inline-end" className="transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent forceMount className="flex flex-col gap-2 data-[state=closed]:hidden">
          <p>仅影响受控 Harbor 进程与运行时任务容器的下载；不改变模型服务商 HTTPS、SSH 主机校验、操作系统信任库或 Docker daemon 的镜像拉取。</p>
          <p>不能解决代理 407 认证、Docker IPAM 地址池冲突或原始 Dockerfile 构建中的 TLS 错误。</p>
        </CollapsibleContent>
      </Collapsible>
    </AlertDescription>
  </Alert>;
}

export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return <Empty>
    <EmptyHeader>
      <EmptyMedia variant="icon"><HugeiconsIcon icon={InboxIcon} aria-hidden="true" /></EmptyMedia>
      <EmptyTitle>{title}</EmptyTitle>
      {children && <EmptyDescription>{children}</EmptyDescription>}
    </EmptyHeader>
    {action && <EmptyContent>{action}</EmptyContent>}
  </Empty>;
}

export function LoadingState({ label = "正在读取本机数据…" }: { label?: string }) {
  return <div role="status" aria-label={label} aria-busy="true" className="p-6">
    <span className="sr-only">{label}</span>
    <div aria-hidden="true" className="flex flex-col gap-3">
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-1/2" />
    </div>
  </div>;
}
