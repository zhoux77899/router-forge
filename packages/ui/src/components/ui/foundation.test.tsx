import { expect, test } from "bun:test"
import { readFileSync, readdirSync } from "node:fs"
import { join } from "node:path"
import { renderToStaticMarkup } from "react-dom/server"
import { Button, buttonVariants } from "./button"
import { Badge, badgeVariants } from "./badge"
import { Card } from "./card"
import { Checkbox } from "./checkbox"
import { Input } from "./input"
import { Textarea } from "./textarea"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "./select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs"
import { Field, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "./field"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "./accordion"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "./collapsible"
import { Empty, EmptyHeader, EmptyTitle } from "./empty"
import { Skeleton } from "./skeleton"
import { Dialog, DialogTrigger } from "./dialog"
import { Sheet, SheetTrigger } from "./sheet"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./tooltip"
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "./sidebar"

const effect = /\b(?:ring|shadow|drop-shadow)(?:-|\b)|box-shadow/
const styles = readFileSync(join(import.meta.dir, "..", "..", "styles.css"), "utf8")
const primitiveFiles = readdirSync(import.meta.dir).filter((name) => name.endsWith(".tsx") && !name.endsWith(".test.tsx"))

function themeVariables(selector: ":root" | ".dark") {
  const block = styles.match(selector === ":root" ? /:root\s*\{([^}]+)\}/ : /\.dark\s*\{([^}]+)\}/)
  expect(block).not.toBeNull()
  return Object.fromEntries(Array.from(block![1].matchAll(/--([\w-]+):\s*([^;]+);/g), ([, token, value]) => [token, value]))
}

function expectKeyboardFocus(classes: string) {
  expect(classes).toContain("focus-visible:outline-2")
  expect(classes).toContain("focus-visible:outline-offset-2")
  expect(classes).toContain("focus-visible:outline-foreground")
  expect(classes).not.toMatch(effect)
}

test("all primitive states are free of ring, shadow, Lucide, and hard-coded dark colors", () => {
  for (const name of primitiveFiles) {
    const source = readFileSync(join(import.meta.dir, name), "utf8")
    expect(source).not.toMatch(effect)
    expect(source).not.toContain("lucide-react")
    expect(source).not.toMatch(/dark:[^\s\"]*(?:bg-|text-|border-)/)
  }
  expect(styles).not.toMatch(/box-shadow|drop-shadow|#[\da-f]{3,8}\b/i)
})

test("shadcn config retains the Radix style and aliases with Neutral and HugeIcons", () => {
  const config = JSON.parse(readFileSync(join(import.meta.dir, "..", "..", "..", "components.json"), "utf8"))
  expect(config.style).toBe("new-york")
  expect(config.rsc).toBe(false)
  expect(config.tailwind.baseColor).toBe("neutral")
  expect(config.tailwind.cssVariables).toBe(true)
  expect(config.iconLibrary).toBe("hugeicons")
  expect(config.aliases.ui).toBe("@router-forge/ui/components/ui")
  expect(config.aliases.utils).toBe("@router-forge/ui/lib/utils")
})

test("light and dark use official Neutral semantic values and monochrome sidebar tokens", () => {
  const light = themeVariables(":root")
  const dark = themeVariables(".dark")
  expect(light.background).toBe("oklch(1 0 0)")
  expect(light.foreground).toBe("oklch(0.145 0 0)")
  expect(light.primary).toBe("oklch(0.205 0 0)")
  expect(light.border).toBe("oklch(0.922 0 0)")
  expect(light["muted-foreground"]).toBe("oklch(0.556 0 0)")
  expect(dark.background).toBe("oklch(0.145 0 0)")
  expect(dark.foreground).toBe("oklch(0.985 0 0)")
  expect(dark.primary).toBe("oklch(0.922 0 0)")
  expect(dark.border).toBe("oklch(1 0 0 / 10%)")
  expect(dark.input).toBe("oklch(1 0 0 / 15%)")
  expect(dark["muted-foreground"]).toBe("oklch(0.708 0 0)")
  for (const theme of [light, dark]) {
    for (const token of ["background", "foreground", "card", "card-foreground", "popover", "popover-foreground", "primary", "primary-foreground", "secondary", "secondary-foreground", "muted", "muted-foreground", "accent", "accent-foreground", "border", "input", "sidebar", "sidebar-foreground", "sidebar-accent", "sidebar-accent-foreground", "sidebar-border"]) {
      expect(theme[token]).toMatch(/^oklch\([\d.]+ 0 0(?: \/ [\d.]+%)?\)$/)
      expect(styles).toContain(`--color-${token}: var(--${token});`)
    }
    expect(theme["sidebar-primary"]).toBe("var(--primary)")
    expect(theme["sidebar-primary-foreground"]).toBe("var(--primary-foreground)")
  }
  expect(styles).toContain("@custom-variant dark (&:is(.dark *))")
  expect(styles).toMatch(/:focus-visible\s*\{\s*outline: 2px solid var\(--foreground\); outline-offset: 2px;/)
  expect(styles).toContain("@media (prefers-reduced-motion: reduce)")
})

test("application typography uses text-xs without shrinking input and button hit targets", () => {
  for (const element of [<Button>Action</Button>, <Input />, <Textarea />]) {
    const html = renderToStaticMarkup(element);
    expect(html).toContain("text-xs");
    expect(html).not.toMatch(/\btext-(?:sm|base|lg|xl)\b/);
  }
  expect(renderToStaticMarkup(<Button>Action</Button>)).toContain("h-9");
  expect(styles).toContain("@apply text-xs");
  for (const name of primitiveFiles) {
    expect(readFileSync(join(import.meta.dir, name), "utf8")).not.toMatch(/\btext-(?:sm|base|lg|xl|[2-9]xl)\b/);
  }
});

test("button sizes and variants preserve visible focus without introducing effects", () => {
  for (const variant of ["default", "destructive", "outline", "secondary", "ghost", "link"] as const) {
    for (const size of ["default", "xs", "sm", "lg", "icon", "icon-xs", "icon-sm", "icon-lg"] as const) {
      expectKeyboardFocus(buttonVariants({ variant, size }))
      const html = renderToStaticMarkup(<Button variant={variant} size={size} aria-invalid disabled>Action</Button>)
      expect(html).toContain(`data-variant="${variant}"`)
      expect(html).toContain(`data-size="${size}"`)
      expect(html).toContain('aria-invalid="true"')
      expect(html).toContain("disabled=")
      expect(html).not.toMatch(effect)
    }
  }
  const link = renderToStaticMarkup(<Button asChild variant="outline"><a href="#settings">Settings</a></Button>)
  expect(link).toStartWith("<a ")
  expect(link).not.toContain("<button")
  expectKeyboardFocus(link)
})

test("badge variants, controls, and card surfaces are effect-free", () => {
  for (const variant of ["default", "secondary", "destructive", "outline", "ghost", "link"] as const) {
    expectKeyboardFocus(badgeVariants({ variant }))
    expectKeyboardFocus(renderToStaticMarkup(<Badge asChild variant={variant}><a href="#report">Report</a></Badge>))
  }
  for (const control of [<Input aria-label="Name" aria-invalid />, <Textarea aria-label="Notes" aria-invalid />]) {
    const html = renderToStaticMarkup(control)
    expectKeyboardFocus(html)
    expect(html).toContain("aria-invalid:border-destructive")
  }
  expect(renderToStaticMarkup(<Card>Content</Card>)).not.toMatch(effect)
})

test("Radix checkbox preserves checked, unchecked, indeterminate, and disabled semantics with HugeIcons", () => {
  for (const checked of [true, false, "indeterminate"] as const) {
    const html = renderToStaticMarkup(<Checkbox checked={checked} disabled aria-invalid aria-label="Choice" />)
    expect(html).toContain('role="checkbox"')
    expect(html).toContain(`aria-checked="${checked === "indeterminate" ? "mixed" : checked}"`)
    expect(html).toContain('aria-invalid="true"')
    expectKeyboardFocus(html)
    if (checked !== false) {
      expect(html).toContain("<svg")
      expect(html).toContain('aria-hidden="true"')
    }
  }
})

test("Radix select keeps grouped options, sizing, focus, and combobox semantics", () => {
  for (const size of ["default", "sm"] as const) {
    const html = renderToStaticMarkup(
      <Select defaultValue="neutral">
        <SelectTrigger size={size} aria-label="Theme"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup><SelectItem value="neutral">Neutral</SelectItem></SelectGroup></SelectContent>
      </Select>,
    )
    expect(html).toContain('role="combobox"')
    expect(html).toContain('aria-expanded="false"')
    expect(html).toContain(`data-size="${size}"`)
    expectKeyboardFocus(html)
  }
})

test("line/default and horizontal/vertical tabs retain active and keyboard-focus states", () => {
  for (const variant of ["default", "line"] as const) for (const orientation of ["horizontal", "vertical"] as const) {
    const html = renderToStaticMarkup(
      <Tabs defaultValue="one" orientation={orientation}>
        <TabsList variant={variant}><TabsTrigger value="one">One</TabsTrigger><TabsTrigger value="two">Two</TabsTrigger></TabsList>
        <TabsContent value="one">Content</TabsContent>
      </Tabs>,
    )
    expect(html).toContain('role="tablist"')
    expect(html).toContain('role="tabpanel"')
    expect(html).toContain('aria-selected="true"')
    expect(html).toContain(`data-variant="${variant}"`)
    expect(html).toContain(`data-orientation="${orientation}"`)
    expectKeyboardFocus(html)
  }
})

test("Field family provides semantic grouping, label association, and deduplicated errors", () => {
  const html = renderToStaticMarkup(
    <FieldSet>
      <FieldLegend>Settings</FieldLegend>
      <FieldGroup>
        <Field data-invalid>
          <FieldLabel htmlFor="profile-name">Name</FieldLabel>
          <Input id="profile-name" aria-invalid aria-describedby="profile-error" />
          <FieldError id="profile-error" errors={[{ message: "Required" }, { message: "Required" }]} />
        </Field>
      </FieldGroup>
    </FieldSet>,
  )
  expect(html).toContain("<fieldset")
  expect(html).toContain("<legend")
  expect(html).toContain('role="group"')
  expect(html).toContain('for="profile-name"')
  expect(html).toContain('aria-describedby="profile-error"')
  expect(html).toContain('role="alert"')
  expect(html.split("Required")).toHaveLength(2)
  expect(renderToStaticMarkup(<FieldError />)).toBe("")
  expect(renderToStaticMarkup(<FieldError errors={[{ message: "Ignored" }]}>Override</FieldError>)).toContain("Override")
  for (const orientation of ["vertical", "horizontal", "responsive"] as const) {
    expect(renderToStaticMarkup(<Field orientation={orientation} />)).toContain(`data-orientation="${orientation}"`)
  }
})

test("official Radix disclosures retain open/closed content and accessible trigger state", () => {
  for (const open of [false, true]) {
    const accordion = renderToStaticMarkup(
      <Accordion type="single" collapsible defaultValue={open ? "details" : undefined}>
        <AccordionItem value="details"><AccordionTrigger>Details</AccordionTrigger><AccordionContent>Accordion body</AccordionContent></AccordionItem>
      </Accordion>,
    )
    const collapsible = renderToStaticMarkup(
      <Collapsible defaultOpen={open}>
        <CollapsibleTrigger asChild><Button variant="ghost">Details</Button></CollapsibleTrigger>
        <CollapsibleContent>Collapsible body</CollapsibleContent>
      </Collapsible>,
    )
    for (const html of [accordion, collapsible]) {
      expect(html).toContain(`aria-expanded="${open}"`)
      expect(html).toContain(`data-state="${open ? "open" : "closed"}"`)
      expectKeyboardFocus(html)
    }
    if (open) {
      expect(accordion).toContain("Accordion body")
      expect(collapsible).toContain("Collapsible body")
    } else {
      expect(accordion).not.toContain("Accordion body")
      expect(collapsible).not.toContain("Collapsible body")
    }
  }
  expect(styles).toContain("--animate-accordion-down: accordion-down 0.2s ease-out")
  expect(styles).toContain("--radix-accordion-content-height")
})

test("official Empty and Skeleton slots add no forced secondary description", () => {
  const empty = renderToStaticMarkup(<Empty><EmptyHeader><EmptyTitle>No runs</EmptyTitle></EmptyHeader></Empty>)
  expect(empty).toContain('data-slot="empty"')
  expect(empty).toContain("No runs")
  expect(empty).not.toContain("empty-description")
  const skeleton = renderToStaticMarkup(<Skeleton className="h-4 w-20" aria-hidden />)
  expect(skeleton).toContain('data-slot="skeleton"')
  expect(skeleton).toContain("animate-pulse")
  expect(skeleton).not.toMatch(effect)
})

test("Dialog, Sheet, and Tooltip keep Radix asChild triggers instead of nested buttons", () => {
  const dialog = renderToStaticMarkup(<Dialog><DialogTrigger asChild><Button>Open</Button></DialogTrigger></Dialog>)
  const sheet = renderToStaticMarkup(<Sheet><SheetTrigger asChild><Button>Open</Button></SheetTrigger></Sheet>)
  const tooltip = renderToStaticMarkup(<TooltipProvider><Tooltip><TooltipTrigger asChild><Button>Help</Button></TooltipTrigger><TooltipContent>Help</TooltipContent></Tooltip></TooltipProvider>)
  for (const html of [dialog, sheet, tooltip]) {
    expect(html.match(/<button\b/g)).toHaveLength(1)
    expectKeyboardFocus(html)
  }
  for (const html of [dialog, sheet]) {
    expect(html).toContain('aria-haspopup="dialog"')
    expect(html).toContain('aria-expanded="false"')
  }
  for (const file of ["dialog.tsx", "sheet.tsx"]) {
    const source = readFileSync(join(import.meta.dir, file), "utf8")
    expect(source).toContain("showCloseButton = true")
    expect(source).toContain('className="sr-only">Close')
    expect(source).toContain('icon={Cancel01Icon} aria-hidden="true"')
  }
})

test("official Sidebar exposes controlled state, active items, tooltip composition, and outline variants", () => {
  for (const variant of ["default", "outline"] as const) for (const size of ["default", "sm", "lg"] as const) {
    const html = renderToStaticMarkup(
      <SidebarProvider open={false}>
        <Sidebar collapsible="icon">
          <SidebarContent><SidebarGroup><SidebarMenu><SidebarMenuItem>
            <SidebarMenuButton asChild isActive variant={variant} size={size} tooltip="Runs"><a href="#runs">Runs</a></SidebarMenuButton>
          </SidebarMenuItem></SidebarMenu></SidebarGroup></SidebarContent>
        </Sidebar>
        <SidebarTrigger />
      </SidebarProvider>,
    )
    expect(html).toContain('data-state="collapsed"')
    expect(html).toContain('data-collapsible="icon"')
    expect(html).toContain('data-active="true"')
    expect(html).toContain(`data-size="${size}"`)
    expect(html).toContain("focus-visible:outline-sidebar-foreground")
    expect(html).toContain("Toggle Sidebar")
    expect(html).not.toMatch(effect)
    if (variant === "outline") expect(html).toContain("border-sidebar-border")
  }
})
