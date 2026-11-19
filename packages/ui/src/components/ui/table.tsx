import * as React from "react"
import { cn } from "@router-forge/ui/lib/utils"

const responsiveLayouts = {
  stacked: {
    table: "block @3xl/table:table",
    header: "sr-only @3xl/table:not-sr-only @3xl/table:table-header-group",
    body: "block @3xl/table:table-row-group",
    footer: "block @3xl/table:table-footer-group",
    row: "block @3xl/table:table-row",
    cell: "block @3xl/table:table-cell",
    label: "@3xl/table:hidden",
    caption: "block @3xl/table:table-caption",
  },
  "stacked-wide": {
    table: "block @5xl/table:table",
    header: "sr-only @5xl/table:not-sr-only @5xl/table:table-header-group",
    body: "block @5xl/table:table-row-group",
    footer: "block @5xl/table:table-footer-group",
    row: "block @5xl/table:table-row",
    cell: "block @5xl/table:table-cell",
    label: "@5xl/table:hidden",
    caption: "block @5xl/table:table-caption",
  },
} as const

type ResponsiveLayout = keyof typeof responsiveLayouts
const TableLayoutContext = React.createContext<ResponsiveLayout | undefined>(undefined)

function useTableLayout() {
  const responsive = React.useContext(TableLayoutContext)
  return responsive ? responsiveLayouts[responsive] : undefined
}

function Table({
  className,
  responsive,
  ...props
}: React.ComponentProps<"table"> & { responsive?: ResponsiveLayout }) {
  return (
    <div
      data-slot="table-container"
      className="@container/table relative w-full min-w-0"
    >
      <TableLayoutContext.Provider value={responsive}>
        <table
          data-slot="table"
          data-responsive={responsive}
          role={responsive ? "table" : undefined}
          className={cn(
            "w-full min-w-0 table-fixed caption-bottom text-xs",
            responsive && responsiveLayouts[responsive].table,
            className
          )}
          {...props}
        />
      </TableLayoutContext.Provider>
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  const layout = useTableLayout()
  return (
    <thead
      data-slot="table-header"
      role={layout ? "rowgroup" : undefined}
      className={cn("[&_tr]:border-b", layout?.header, className)}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  const layout = useTableLayout()
  return (
    <tbody
      data-slot="table-body"
      role={layout ? "rowgroup" : undefined}
      className={cn("[&_tr:last-child]:border-0", layout?.body, className)}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<"tfoot">) {
  const layout = useTableLayout()
  return (
    <tfoot
      data-slot="table-footer"
      role={layout ? "rowgroup" : undefined}
      className={cn(
        "border-t bg-muted/50 font-medium [&>tr]:last:border-b-0",
        layout?.footer,
        className
      )}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<"tr">) {
  const layout = useTableLayout()
  return (
    <tr
      data-slot="table-row"
      role={layout ? "row" : undefined}
      className={cn(
        "min-w-0 border-b transition-colors hover:bg-muted/50 has-aria-expanded:bg-muted/50 data-[state=selected]:bg-muted",
        layout?.row,
        className
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  const layout = useTableLayout()
  return (
    <th
      data-slot="table-head"
      role={layout ? "columnheader" : undefined}
      scope="col"
      className={cn(
        "h-10 px-2 text-left align-middle font-medium whitespace-normal text-foreground [overflow-wrap:anywhere] [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className
      )}
      {...props}
    />
  )
}

function TableCell({
  className,
  label,
  children,
  ...props
}: React.ComponentProps<"td"> & { label?: string }) {
  const layout = useTableLayout()
  return (
    <td
      data-slot="table-cell"
      data-label={label}
      role={layout ? "cell" : undefined}
      className={cn(
        "min-w-0 p-2 align-middle whitespace-normal [overflow-wrap:anywhere] [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        layout?.cell,
        className
      )}
      {...props}
    >
      {layout && label && (
        <span
          data-slot="table-cell-label"
          aria-hidden="true"
          className={cn("mb-1 block font-medium text-muted-foreground", layout.label)}
        >
          {label}
        </span>
      )}
      {children}
    </td>
  )
}

function TableCaption({
  className,
  ...props
}: React.ComponentProps<"caption">) {
  const layout = useTableLayout()
  return (
    <caption
      data-slot="table-caption"
      className={cn("mt-4 text-xs text-muted-foreground", layout?.caption, className)}
      {...props}
    />
  )
}

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
