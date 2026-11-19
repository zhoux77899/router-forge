import { useEffect, useRef, type ReactNode } from "react";
import { ScrollArea } from "@router-forge/ui/components/ui/scroll-area";
import { cn } from "@router-forge/ui/lib/utils";

export function PageFrame({ title, leading, children, widthClassName }: {
  title: string; leading?: ReactNode; children: ReactNode; widthClassName?: string;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  useEffect(() => { if (viewport.current) viewport.current.scrollTop = 0; }, [title]);

  return <div data-slot="page-frame" className="flex min-h-0 min-w-0 flex-1 flex-col">
    <header data-slot="page-header" className="shrink-0 bg-background">
      <div className={cn("mx-auto flex w-full min-w-0 items-center gap-3 px-4 py-5 md:px-6", widthClassName)}>
        {leading}<h1 className="min-w-0 font-semibold">{title}</h1>
      </div>
    </header>
    <ScrollArea type="auto" className="app-page-scroll min-h-0 min-w-0 flex-1" viewportProps={{ ref: viewport, role: "region", "aria-label": `${title}内容` }}>
      <div data-slot="page-content" className={cn("@container/workspace app-page-content mx-auto flex w-full min-w-0 flex-col gap-6 px-4 pb-6 md:px-6", widthClassName)}>{children}</div>
    </ScrollArea>
  </div>;
}
