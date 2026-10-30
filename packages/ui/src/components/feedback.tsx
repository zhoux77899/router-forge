import type { ReactNode } from "react";
import { AlertCircle, Inbox, LoaderCircle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "./ui/alert";
import { Button } from "./ui/button";
import { errorMessage } from "../lib/format";

export function ErrorNotice({ error, retry, title = "无法读取数据" }: { error: unknown; retry?: () => void; title?: string }) {
  return <Alert variant="destructive" role="alert">
    <AlertCircle aria-hidden="true" />
    <AlertTitle>{title}</AlertTitle>
    <AlertDescription className="break-words">
      <p>{errorMessage(error)}</p>
      {retry && <Button size="sm" variant="outline" onClick={retry}>重新读取</Button>}
    </AlertDescription>
  </Alert>;
}

export function EmptyState({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
    <Inbox size={28} className="text-muted-foreground" aria-hidden="true" />
    <h3 className="font-semibold">{title}</h3>
    <div className="max-w-[60ch] text-sm text-muted-foreground">{children}</div>
    {action}
  </div>;
}

export function LoadingState({ label = "正在读取本机数据…" }: { label?: string }) {
  return <div role="status" className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
    <LoaderCircle size={16} className="animate-spin motion-reduce:animate-none" aria-hidden="true" />{label}
  </div>;
}
