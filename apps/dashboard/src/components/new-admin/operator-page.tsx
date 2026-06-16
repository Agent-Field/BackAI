// SPDX-License-Identifier: Apache-2.0

import {
  ArrowUpRight,
  Circle,
  Code2,
  Database,
  ExternalLink,
  Filter,
  GitBranch,
  PanelRight,
  Search,
  Server,
  Terminal,
  Webhook,
  Zap,
} from "lucide-react"

import { ActionDrawer } from "@/components/new-admin/action-drawer"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { LiveRefresh } from "@/components/new-admin/live-refresh"
import { cn } from "@/lib/utils"
import type { ConsoleRow, Kpi, StatusTone } from "@/lib/new-admin/data"
import type { PageCard, PageControl, PageModel } from "@/lib/new-admin/page-model"

const toneClass: Record<StatusTone, string> = {
  ok: "text-[var(--status-ok)]",
  fail: "text-[var(--status-fail)]",
  warn: "text-[var(--status-warn)]",
  running: "text-[var(--status-running)]",
  neutral: "text-muted-foreground",
}

export function OperatorPage({ model }: { model: PageModel }) {
  return (
    <div className="mx-auto flex w-full max-w-[1480px] flex-col gap-4 p-4 md:p-6">
      <LiveRefresh enabled={model.live} />
      <section className="flex min-h-10 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold leading-tight tracking-normal">{model.title}</h1>
            <Badge variant="outline" className="gap-1 rounded-full">
              <Circle className={cn("size-2 fill-current", model.live ? "text-[var(--status-ok)]" : "text-[var(--status-warn)]")} />
              {model.live ? "live" : "seeded"}
            </Badge>
            {model.adapter && (
              <Badge variant="secondary" className="gap-1 rounded-full" title="Open docs, adapter admin, or change adapter from Setup.">
                via {model.adapter}
                <ExternalLink className="size-3" />
              </Badge>
            )}
          </div>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{model.description}</p>
        </div>
        <ActionDrawer action={model.primaryAction} page={model.title} />
      </section>

      <section className="flex min-h-10 flex-wrap items-center gap-2 rounded-lg border bg-card/70 p-2">
        {model.controls.map((control) => (
          <Control key={`${control.kind}-${control.label}`} control={control} />
        ))}
        <div className="ml-auto hidden text-xs text-muted-foreground md:block">
          Platform · all tenants · {formatGeneratedAt(model.generatedAt)}
        </div>
      </section>

      <PageCanvas model={model} />
    </div>
  )
}

function formatGeneratedAt(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "updated now"
  return `updated ${new Intl.DateTimeFormat("en", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date)}`
}

function PageCanvas({ model }: { model: PageModel }) {
  if (model.path === "/") return <HomeCanvas model={model} />
  if (model.path === "/operate/cost") return <CostCanvas model={model} />
  if (["/operate/runs", "/operate/errors", "/operate/traces", "/operate/queue"].includes(model.path)) {
    return <DebugCanvas model={model} />
  }
  if (model.path.startsWith("/develop/")) return <DevelopCanvas model={model} />
  if (model.path === "/build/data/tables") return <TableExplorerCanvas model={model} />
  if (model.path.startsWith("/build/data/")) return <DataCanvas model={model} />
  if (model.path.startsWith("/setup/")) return <SetupCanvas model={model} />
  if (model.path === "/brand") return <BrandCanvas model={model} />
  return <EntityCanvas model={model} />
}

function HomeCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="xl:grid-cols-4 2xl:grid-cols-8" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(360px,0.9fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>Recent activity</CardTitle>
            <CardDescription>Runs, tenants, budgets, alerts, and configuration changes in one operator feed.</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <ActivityList rows={model.rows} />
          </CardContent>
        </Card>
        <div className="grid gap-4">
          <QuickActions />
          <SecondaryRegion model={model} />
        </div>
      </section>
    </>
  )
}

function CostCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(360px,0.8fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>Spend over time</CardTitle>
            <CardDescription>Live ledger events grouped by tenant, model, or agent. Bars are normalized to the selected window.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            <SpendBars rows={model.rows.slice(0, 12)} />
            <DenseTable columns={["Model / tenant", "Provider context", "Cost / tokens", "When"]} rows={model.rows.slice(0, 8)} />
          </CardContent>
        </Card>
        <div className="grid gap-4">
          <Card className="rounded-lg">
            <CardHeader>
              <CardTitle>Budget pressure</CardTitle>
              <CardDescription>Caps remain visible while cost events continue to update.</CardDescription>
            </CardHeader>
            <CardContent>
              <RankedRows rows={model.secondary.flatMap((card) => card.rows ?? []).slice(0, 5)} />
            </CardContent>
          </Card>
          <SecondaryRegion model={model} />
        </div>
      </section>
    </>
  )
}

function DebugCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid min-h-[520px] gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(380px,0.85fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
            <CardAction>
              <Button variant="outline" size="sm" className="gap-2">
                <Filter className="size-3.5" />
                Filters
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="p-0">
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle className="flex items-center gap-2">
              <PanelRight className="size-4" />
              Selected detail
            </CardTitle>
            <CardDescription>Split-pane inspection for fast row-to-row debugging.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            {selected ? (
              <>
                <KeyValue label="ID" value={selected.id} />
                <KeyValue label="Object" value={selected.primary} />
                <KeyValue label="Context" value={selected.secondary} />
                <KeyValue label="Status" value={selected.status} tone={selected.tone} />
                <Separator />
                <pre className="overflow-auto rounded-md border bg-background p-3 font-mono text-xs text-muted-foreground">
                  {JSON.stringify(selected, null, 2)}
                </pre>
                <div className="flex gap-2">
                  <Button size="sm" variant="outline">Open source</Button>
                  <Button size="sm" variant="ghost">Copy JSON</Button>
                </div>
              </>
            ) : (
              <EmptyInline label="No rows match the current filters." />
            )}
          </CardContent>
        </Card>
      </section>
    </>
  )
}

function DevelopCanvas({ model }: { model: PageModel }) {
  const isExplorer = model.path.includes("api-explorer")
  const isSchema = model.path.includes("schema")
  const isRecipes = model.path.includes("recipes")
  return (
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(360px,0.8fr)]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            {isExplorer ? <Zap className="size-4" /> : isSchema ? <GitBranch className="size-4" /> : isRecipes ? <Webhook className="size-4" /> : <Terminal className="size-4" />}
            {model.tableTitle}
          </CardTitle>
          <CardDescription>{model.tableDescription}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          {isExplorer ? <EndpointExplorer rows={model.rows} /> : isSchema ? <SchemaPanel rows={model.rows} /> : isRecipes ? <RecipeGrid rows={model.rows} /> : <SnippetWorkbench model={model} />}
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <KpiGrid model={model} columns="grid-cols-2" />
        <SecondaryRegion model={model} />
      </div>
    </section>
  )
}

function TableExplorerCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <section className="grid min-h-[560px] gap-4 xl:grid-cols-[320px_minmax(0,1fr)]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>Tables</CardTitle>
          <CardDescription>Postgres schema objects and tenant isolation posture.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <ActivityList rows={model.rows} compact />
        </CardContent>
      </Card>
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{selected?.primary ?? "No table selected"}</CardTitle>
          <CardDescription>{selected?.secondary ?? "Select a table to inspect columns, RLS, indexes, and preview rows."}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          <KpiGrid model={model} columns="lg:grid-cols-4" />
          <DenseTable columns={["Column", "Type / policy", "Rows / size", "State"]} rows={model.rows.slice(0, 8)} />
        </CardContent>
      </Card>
    </section>
  )
}

function DataCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            <SearchProbe path={model.path} />
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <SecondaryRegion model={model} />
      </section>
    </>
  )
}

function SetupCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(340px,0.75fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle className="flex items-center gap-2">
              <Server className="size-4" />
              {model.tableTitle}
            </CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            <ReadinessMatrix rows={model.rows} />
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <SecondaryRegion model={model} />
      </section>
    </>
  )
}

function BrandCanvas({ model }: { model: PageModel }) {
  return (
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>Theme tokens</CardTitle>
          <CardDescription>Centralized monochrome shell tokens used by the admin app.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {model.rows.slice(0, 4).map((row) => (
              <div key={row.id} className="rounded-lg border bg-background p-3">
                <div className="text-xs text-muted-foreground">{row.primary}</div>
                <div className="mt-2 font-mono text-xl">{row.metric}</div>
                <StatusBadge status={row.status} tone={row.tone} />
              </div>
            ))}
          </div>
          <DenseTable columns={model.tableColumns} rows={model.rows} />
        </CardContent>
      </Card>
      <SecondaryRegion model={model} />
    </section>
  )
}

function EntityCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(340px,0.75fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
            <CardAction>
              <Button variant="ghost" size="icon">
                <ArrowUpRight className="size-4" />
                <span className="sr-only">Open details</span>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="p-0">
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <SecondaryRegion model={model} />
      </section>
    </>
  )
}

function KpiGrid({ model, columns = "xl:grid-cols-4" }: { model: PageModel; columns?: string }) {
  return (
    <section className={cn("grid gap-3 md:grid-cols-2", columns)}>
      {model.kpis.map((kpi) => (
        <KpiTile key={kpi.label} kpi={kpi} />
      ))}
    </section>
  )
}

function SecondaryRegion({ model }: { model: PageModel }) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-[15px] font-semibold">{model.secondaryTitle}</h2>
        <Badge variant="outline" className="rounded-full font-mono text-[11px]">
          {model.secondary.length} panels
        </Badge>
      </div>
      {model.secondary.map((card) => (
        <SecondaryCard key={card.title} card={card} />
      ))}
    </div>
  )
}

function Control({ control }: { control: PageControl }) {
  if (control.kind === "input") {
    return (
      <Input
        aria-label={control.label}
        placeholder={control.placeholder}
        className="h-9 min-w-[220px] flex-1 md:max-w-sm"
      />
    )
  }
  if (control.kind === "tabs") {
    return (
      <Tabs defaultValue={control.value ?? control.options?.[0]} className="h-9">
        <TabsList className="h-9">
          {control.options?.map((option) => (
            <TabsTrigger key={option} value={option} className="h-8 px-3 text-xs">
              {option}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    )
  }
  if (control.kind === "switch") {
    return (
      <div className="flex h-9 items-center gap-2 rounded-md border px-2 text-xs text-muted-foreground">
        <span>{control.label}</span>
        <Switch defaultChecked={control.value === "on"} />
      </div>
    )
  }
  return (
    <Select defaultValue={control.value ?? control.options?.[0]}>
      <SelectTrigger className="h-9 min-w-[140px]">
        <SelectValue placeholder={control.label} />
      </SelectTrigger>
      <SelectContent>
        {control.options?.map((option) => (
          <SelectItem key={option} value={option}>
            {option}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function KpiTile({ kpi }: { kpi: Kpi }) {
  return (
    <Card size="sm" className="min-h-[108px] rounded-lg">
      <CardHeader>
        <CardTitle className="text-xs font-medium text-muted-foreground">{kpi.label}</CardTitle>
        <CardAction>
          <ArrowUpRight className="size-3.5 text-muted-foreground" />
        </CardAction>
      </CardHeader>
      <CardContent className="grid gap-2">
        <div className="flex items-end gap-2">
          <div className="font-mono text-2xl leading-none tracking-normal text-foreground">{kpi.value}</div>
          <div className={cn("pb-0.5 font-mono text-xs", kpi.tone ? toneClass[kpi.tone] : "text-muted-foreground")}>
            {kpi.trend}
          </div>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="truncate text-xs text-muted-foreground">{kpi.detail}</span>
          <MiniSparkline values={kpi.sparkline} />
        </div>
      </CardContent>
    </Card>
  )
}

function MiniSparkline({ values }: { values: number[] }) {
  const max = Math.max(...values, 1)
  return (
    <div className="flex h-7 w-24 items-end gap-1" aria-hidden>
      {values.map((value, index) => (
        <span
          key={`${value}-${index}`}
          className="w-1 flex-1 rounded-sm bg-foreground/25"
          style={{ height: `${Math.max(15, (value / max) * 100)}%` }}
        />
      ))}
    </div>
  )
}

function ActivityList({ rows, compact = false }: { rows: ConsoleRow[]; compact?: boolean }) {
  return (
    <div className="divide-y">
      {rows.map((row) => (
        <button
          key={row.id}
          type="button"
          className={cn(
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-4 text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            compact ? "min-h-11 py-2" : "min-h-14 py-3"
          )}
        >
          <Circle className={cn("size-2 fill-current", toneClass[row.tone])} />
          <span className="min-w-0">
            <span className="block truncate text-sm font-medium">{row.primary}</span>
            <span className="block truncate font-mono text-xs text-muted-foreground">{row.secondary}</span>
          </span>
          <span className="font-mono text-xs text-muted-foreground">{row.timestamp}</span>
        </button>
      ))}
    </div>
  )
}

function QuickActions() {
  const actions = [
    { icon: Terminal, label: "Issue API key", sub: "Scoped tenant key" },
    { icon: Database, label: "Add tenant", sub: "Customer workspace" },
    { icon: Zap, label: "Test agent", sub: "Create a live run" },
    { icon: Code2, label: "API explorer", sub: "Try an endpoint" },
  ]
  return (
    <Card className="rounded-lg">
      <CardHeader>
        <CardTitle>Quick actions</CardTitle>
        <CardDescription>Mutation entry points open in the right drawer.</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-2">
        {actions.map((action) => (
          <Button key={action.label} variant="outline" className="h-auto justify-start gap-3 px-3 py-2">
            <action.icon className="size-4" />
            <span className="grid text-left">
              <span>{action.label}</span>
              <span className="text-xs font-normal text-muted-foreground">{action.sub}</span>
            </span>
          </Button>
        ))}
      </CardContent>
    </Card>
  )
}

function SpendBars({ rows }: { rows: ConsoleRow[] }) {
  const values = rows.map((row) => moneyNumber(row.metric))
  const max = Math.max(...values, 1)
  return (
    <div className="grid h-56 grid-cols-12 items-end gap-2 rounded-lg border bg-background p-3">
      {rows.slice(0, 12).map((row, index) => {
        const value = values[index] ?? 0
        return (
          <div key={row.id} className="flex min-w-0 flex-col items-center gap-2">
            <div className="flex h-44 w-full items-end">
              <div
                className="w-full rounded-t-sm bg-foreground/70"
                style={{ height: `${Math.max(8, (value / max) * 100)}%` }}
              />
            </div>
            <span className="max-w-full truncate font-mono text-[10px] text-muted-foreground">
              {row.timestamp || index + 1}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function RankedRows({ rows }: { rows: Array<{ label: string; value: string; tone?: StatusTone }> }) {
  if (!rows.length) return <EmptyInline label="No budget rows returned for this scope." />
  return (
    <div className="grid gap-3">
      {rows.map((row) => {
        const progress = Number.parseInt(row.value, 10)
        return (
          <div key={`${row.label}-${row.value}`} className="grid gap-2">
            <div className="flex justify-between gap-3 text-sm">
              <span className="truncate text-muted-foreground">{row.label}</span>
              <span className={cn("font-mono", row.tone ? toneClass[row.tone] : "text-foreground")}>{row.value}</span>
            </div>
            <Progress value={Number.isFinite(progress) ? progress : 0} />
          </div>
        )
      })}
    </div>
  )
}

function KeyValue({ label, value, tone }: { label: string; value: string; tone?: StatusTone }) {
  return (
    <div className="grid grid-cols-[110px_minmax(0,1fr)] gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn("truncate font-mono", tone ? toneClass[tone] : "text-foreground")}>{value}</span>
    </div>
  )
}

function EmptyInline({ label }: { label: string }) {
  return (
    <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
      {label}
    </div>
  )
}

function EndpointExplorer({ rows }: { rows: ConsoleRow[] }) {
  const selected = rows[0]
  return (
    <div className="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
      <div className="rounded-lg border">
        <ActivityList rows={rows} compact />
      </div>
      <div className="grid gap-3 rounded-lg border bg-background p-3">
        <div className="flex items-center justify-between">
          <div>
            <div className="font-mono text-sm">{selected?.primary ?? "Select endpoint"}</div>
            <div className="text-xs text-muted-foreground">{selected?.secondary ?? "OpenAPI operation details"}</div>
          </div>
          <Button size="sm" className="gap-2">
            <Zap className="size-3.5" />
            Try it
          </Button>
        </div>
        <Textarea defaultValue={'{\n  "tenant": "acme",\n  "input": {}\n}'} className="min-h-40 font-mono text-xs" />
        <pre className="rounded-md border bg-card p-3 font-mono text-xs text-muted-foreground">
          {selected ? JSON.stringify({ operation: selected.id, status: selected.status, response: "linked back into Runs and Traces" }, null, 2) : "{}"}
        </pre>
      </div>
    </div>
  )
}

function SchemaPanel({ rows }: { rows: ConsoleRow[] }) {
  return (
    <div className="grid gap-4">
      <div className="grid gap-3 md:grid-cols-3">
        {rows.slice(0, 3).map((row) => (
          <Card key={row.id} size="sm" className="rounded-lg">
            <CardHeader>
              <CardTitle>{row.primary}</CardTitle>
              <CardDescription>{row.secondary}</CardDescription>
            </CardHeader>
            <CardContent className="flex items-center justify-between">
              <StatusBadge status={row.status} tone={row.tone} />
              <span className="font-mono text-sm">{row.metric}</span>
            </CardContent>
          </Card>
        ))}
      </div>
      <pre className="max-h-[360px] overflow-auto rounded-lg border bg-background p-4 font-mono text-xs text-muted-foreground">
        {JSON.stringify({ openapi: "3.1.0", info: { title: "BackAI Runtime API" }, paths: rows.map((row) => row.id) }, null, 2)}
      </pre>
    </div>
  )
}

function RecipeGrid({ rows }: { rows: ConsoleRow[] }) {
  return (
    <div className="grid gap-3 md:grid-cols-2">
      {rows.map((row) => (
        <Card key={row.id} size="sm" className="rounded-lg">
          <CardHeader>
            <CardTitle>{row.primary}</CardTitle>
            <CardDescription>{row.secondary}</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <Badge variant="outline" className="rounded-full">{row.status}</Badge>
            <span className="font-mono text-xs text-muted-foreground">{row.metric}</span>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function SnippetWorkbench({ model }: { model: PageModel }) {
  const snippets = model.secondary.filter((card) => card.code)
  return (
    <div className="grid gap-4">
      <DenseTable columns={model.tableColumns} rows={model.rows} />
      <Tabs defaultValue={snippets[0]?.title ?? "snippet"}>
        <TabsList>
          {snippets.map((snippet) => (
            <TabsTrigger key={snippet.title} value={snippet.title}>{snippet.title}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <pre className="overflow-auto rounded-lg border bg-background p-4 font-mono text-xs text-muted-foreground">
        {snippets[0]?.code ?? model.rows[0]?.secondary ?? "No snippet available."}
      </pre>
    </div>
  )
}

function SearchProbe({ path }: { path: string }) {
  const label = path.includes("sql")
    ? "select * from suite_runs order by started_at desc limit 20;"
    : path.includes("storage")
      ? "tenant/acme/"
      : path.includes("memory")
        ? "What does this tenant remember?"
        : "status:active tenant:acme"
  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-background p-3 md:flex-row md:items-center">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Search className="size-4 text-muted-foreground" />
        <Input defaultValue={label} className="font-mono text-xs" />
      </div>
      <Button variant="outline" size="sm">Run probe</Button>
    </div>
  )
}

function ReadinessMatrix({ rows }: { rows: ConsoleRow[] }) {
  const grouped = rows.slice(0, 8)
  return (
    <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-4">
      {grouped.map((row) => (
        <div key={row.id} className="rounded-lg border bg-background p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-sm font-medium">{row.primary}</span>
            <Circle className={cn("size-2 fill-current", toneClass[row.tone])} />
          </div>
          <div className="mt-1 truncate text-xs text-muted-foreground">{row.secondary}</div>
          <div className="mt-3 flex items-center justify-between gap-2">
            <StatusBadge status={row.status} tone={row.tone} />
            <span className="font-mono text-xs text-muted-foreground">{row.metric}</span>
          </div>
        </div>
      ))}
    </div>
  )
}

function moneyNumber(value: string) {
  const parsed = Number.parseFloat(value.replace(/[^0-9.-]/g, ""))
  return Number.isFinite(parsed) ? parsed : 0
}

function DenseTable({
  columns,
  rows,
}: {
  columns: [string, string, string, string]
  rows: ConsoleRow[]
}) {
  return (
    <Table className="table-fixed md:table-auto">
      <TableHeader>
        <TableRow className="h-9">
          {columns.map((column, index) => (
            <TableHead
              key={column}
              className={cn(
                "h-9 px-4 text-[11px] uppercase tracking-normal text-muted-foreground",
                index === 0 && "w-[38%] md:w-auto",
                index >= 2 && "hidden text-right md:table-cell"
              )}
            >
              {column}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, index) => (
          <TableRow key={row.id} data-state={index === 0 ? "selected" : undefined} className="h-9">
            <TableCell className="w-[38%] max-w-0 whitespace-normal px-4 py-1.5 md:w-auto md:max-w-[340px]">
              <div className="truncate font-medium">{row.primary}</div>
              <div className="truncate font-mono text-xs text-muted-foreground">{row.id}</div>
            </TableCell>
            <TableCell className="max-w-0 whitespace-normal px-4 py-1.5 md:max-w-[380px]">
              <div className="truncate text-sm text-muted-foreground">{row.secondary}</div>
              <div className="mt-1 flex items-center gap-2">
                <StatusBadge status={row.status} tone={row.tone} />
                <span className="font-mono text-xs text-muted-foreground md:hidden">{row.metric}</span>
              </div>
            </TableCell>
            <TableCell className="hidden px-4 py-1.5 text-right font-mono text-sm md:table-cell">{row.metric}</TableCell>
            <TableCell className="hidden px-4 py-1.5 text-right font-mono text-xs text-muted-foreground md:table-cell">{row.timestamp}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function StatusBadge({ status, tone }: { status: string; tone: StatusTone }) {
  return (
    <Badge variant="outline" className="mt-1 gap-1 rounded-full px-1.5 py-0 font-mono text-[11px]">
      <Circle className={cn("size-1.5 fill-current", toneClass[tone])} />
      {status}
    </Badge>
  )
}

function SecondaryCard({ card }: { card: PageCard }) {
  return (
    <Card size="sm" className="rounded-lg">
      <CardHeader>
        <CardTitle>{card.title}</CardTitle>
        <CardDescription>{card.description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3">
        {card.rows?.map((row) => (
          <div key={`${row.label}-${row.value}`} className="grid gap-2">
            <div className="flex items-center justify-between gap-3 text-sm">
              <span className="text-muted-foreground">{row.label}</span>
              <span className={cn("truncate text-right font-mono", row.tone ? toneClass[row.tone] : "text-foreground")}>
                {row.value}
              </span>
            </div>
            {row.value.includes("%") && <Progress value={Number.parseInt(row.value, 10) || 0} />}
          </div>
        ))}
        {card.code && (
          <>
            <Separator />
            <pre className="overflow-x-auto rounded-md border bg-background p-3 font-mono text-xs text-muted-foreground">
              {card.code}
            </pre>
          </>
        )}
        {!card.rows?.length && !card.code && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Database className="size-4" />
            Runtime data will populate this panel.
          </div>
        )}
      </CardContent>
    </Card>
  )
}
