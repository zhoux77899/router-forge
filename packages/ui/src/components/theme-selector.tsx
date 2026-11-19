import { HugeiconsIcon } from "@hugeicons/react";
import { ComputerIcon, Moon02Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@router-forge/ui/components/ui/tabs";
import { isThemePreference, type ThemePreference } from "@router-forge/ui/lib/appearance";

const themes = [
  { value: "light", label: "浅色", icon: Sun03Icon },
  { value: "dark", label: "深色", icon: Moon02Icon },
  { value: "system", label: "跟随系统", icon: ComputerIcon },
] as const;

export function ThemeSelector({ value, onValueChange }: { value: ThemePreference; onValueChange: (value: ThemePreference) => void }) {
  return <Tabs value={value} onValueChange={next => { if (isThemePreference(next)) onValueChange(next); }}>
    <TabsList aria-label="主题模式" className="max-w-full">
      {themes.map(theme => <TabsTrigger key={theme.value} value={theme.value}>
        <HugeiconsIcon icon={theme.icon} data-icon="inline-start" aria-hidden="true" />{theme.label}
      </TabsTrigger>)}
    </TabsList>
    {themes.map(theme => <TabsContent key={theme.value} value={theme.value} className="sr-only">{theme.label}主题模式</TabsContent>)}
  </Tabs>;
}
