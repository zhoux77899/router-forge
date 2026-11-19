/// <reference path="../svg.d.ts" />
import type { ComponentProps } from "react";
import appIconUrl from "../../../../assets/icon-mark.svg?url";
import { cn } from "@router-forge/ui/lib/utils";

export { appIconUrl };

export function AppIcon({ className, style, ...props }: Omit<ComponentProps<"span">, "children">) {
  const maskImage = `url("${appIconUrl}")`;
  return <span {...props} role="img" aria-label="Router Forge" draggable={false} className={cn("app-icon size-6 shrink-0", className)} style={{ ...style, maskImage, WebkitMaskImage: maskImage }} />;
}
