// SPDX-License-Identifier: Apache-2.0

import {
  ArrowUpRight,
  CheckCircle2,
  Circle,
  Code2,
  Copy,
  Database,
  ExternalLink,
  Filter,
  PanelRight,
  Pause,
  Server,
  Terminal,
  Zap,
} from "lucide-react"

import { ActionDrawer } from "@/components/new-admin/action-drawer"
import Link from "next/link"
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

function truthTone(value: PageModel["dataTruth"]) {
  if (value === "backed") return "border-[var(--status-ok)]/40 text-[var(--status-ok)]"
  if (value === "missing") return "border-[var(--status-fail)]/40 text-[var(--status-fail)]"
  if (value === "degraded" || value === "derived") return "border-[var(--status-warn)]/40 text-[var(--status-warn)]"
  return "text-muted-foreground"
}

function sourceBadge(model: PageModel) {
  if (model.live) return { label: "live", tone: "text-[var(--status-ok)]" }
  if (model.source === "live") return { label: "runtime", tone: "text-[var(--status-ok)]" }
  return { label: "seeded", tone: "text-[var(--status-warn)]" }
}

export function OperatorPage({ model }: { model: PageModel }) {
  const source = sourceBadge(model)

  return (
    <div className="mx-auto flex w-full max-w-[1480px] flex-col gap-4 p-4 md:p-6">
      <LiveRefresh enabled={model.live} />
      <section className="flex min-h-10 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold leading-tight tracking-normal">{model.title}</h1>
            <Badge variant="outline" className="gap-1 rounded-full">
              <Circle className={cn("size-2 fill-current", source.tone)} />
              {source.label}
            </Badge>
            <Badge variant="outline" className={cn("rounded-full font-mono", truthTone(model.dataTruth))}>
              {model.dataTruth}
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

      {model.apiGap && (
        <section className="rounded-lg border border-[var(--status-warn)]/35 bg-[var(--status-warn)]/5 px-3 py-2 text-sm text-muted-foreground">
          <span className="font-medium text-foreground">API caveat:</span> {model.apiGap}
        </section>
      )}

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
  if (model.archetype === "analytics") return <AnalyticsCanvas model={model} />
  if (model.archetype === "split-debugger") return <DebugCanvas model={model} />
  if (model.archetype === "delivery-inbox") return <DeliveryInboxCanvas model={model} />
  if (model.archetype === "approval-queue") return <ApprovalQueueCanvas model={model} />
  if (model.archetype === "timeline") return <TimelineCanvas model={model} />
  if (model.archetype === "topology") return <TopologyCanvas model={model} />
  if (model.archetype === "log-console") return <LogConsoleCanvas model={model} />
  if (model.archetype === "registry-detail") return <RegistryCanvas model={model} />
  if (model.archetype === "workbench") return <WorkbenchCanvas model={model} />
  if (model.archetype === "data-explorer") return <TableExplorerCanvas model={model} />
  if (model.archetype === "customer-drilldown") return <CustomerCanvas model={model} />
  if (model.archetype === "config-inventory") return <SetupCanvas model={model} />
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

function AnalyticsCanvas({ model }: { model: PageModel }) {
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
            <BulkActionBar rows={model.rows} />
            <DenseTable columns={model.path.includes("cost") ? ["Model / tenant", "Provider context", "Cost / tokens", "When"] : model.tableColumns} rows={model.rows.slice(0, 8)} />
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

function DeliveryInboxCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
            <CardAction>
              <Button variant="outline" size="sm" className="gap-2">
                <Filter className="size-3.5" />
                Delivery filters
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="p-0">
            <BulkActionBar rows={model.rows} />
            <DenseTable columns={["Event", "Destination / recipient", "Attempts", "When"]} rows={model.rows} />
          </CardContent>
        </Card>
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>Payload and provider response</CardTitle>
            <CardDescription>Delivery rows keep payload, response, retry history, and share links together.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            {selected ? <RowInspector row={selected} /> : <EmptyInline label="Select a delivery to inspect provider context." />}
            <div className="grid grid-cols-2 gap-2">
              <Button variant="outline" size="sm">Retry</Button>
              <Button variant="ghost" size="sm">Copy link</Button>
            </div>
          </CardContent>
        </Card>
      </section>
    </>
  )
}

function ApprovalQueueCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <section className="grid gap-4 xl:grid-cols-[360px_minmax(0,1fr)_360px]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>Decision queue</CardTitle>
          <CardDescription>Pending first, with approved and denied history below.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <ActivityList rows={model.rows} compact />
        </CardContent>
      </Card>
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{selected?.primary ?? "No approval selected"}</CardTitle>
          <CardDescription>{selected?.secondary ?? "Select an approval to compare payload and source run."}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          {selected ? <RowInspector row={selected} /> : <EmptyInline label="No approval selected." />}
          <pre className="min-h-48 overflow-auto rounded-lg border bg-background p-3 font-mono text-xs text-muted-foreground">
            {selected?.detail ?? JSON.stringify({ payload: "Approval payload appears here", related_run: "run id" }, null, 2)}
          </pre>
        </CardContent>
      </Card>
      <Card className="rounded-lg">
        <CardHeader>
          <CardTitle>Decision</CardTitle>
          <CardDescription>Every action requires a note and writes audit context.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-2">
          <Button className="gap-2"><CheckCircle2 className="size-4" />Approve</Button>
          <Button variant="outline">Deny</Button>
          <Button variant="ghost">Cancel request</Button>
          <Textarea placeholder="Decision note" className="min-h-28" />
        </CardContent>
      </Card>
    </section>
  )
}

function TimelineCanvas({ model }: { model: PageModel }) {
  return (
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{model.tableTitle}</CardTitle>
          <CardDescription>{model.tableDescription}</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <TimelineRows rows={model.rows} />
        </CardContent>
      </Card>
      <SecondaryRegion model={model} />
    </section>
  )
}

function TopologyCanvas({ model }: { model: PageModel }) {
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
            <ReadinessMatrix rows={model.rows} />
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <SecondaryRegion model={model} />
      </section>
    </>
  )
}

function LogConsoleCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_400px]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            <Terminal className="size-4" />
            {model.tableTitle}
          </CardTitle>
          <CardDescription>{model.tableDescription}</CardDescription>
          <CardAction className="flex gap-2">
            <Button variant="outline" size="sm" className="gap-2">
              <Pause className="size-3.5" />
              Pause
            </Button>
            <Button variant="ghost" size="sm">Export</Button>
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          <LogRows rows={model.rows} />
        </CardContent>
      </Card>
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>Structured fields</CardTitle>
          <CardDescription>Copyable details for the selected stream row.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          {selected ? <RowInspector row={selected} /> : <EmptyInline label="No stream rows returned." />}
          <pre className="min-h-56 overflow-auto rounded-lg border bg-background p-3 font-mono text-xs text-muted-foreground">
            {selected?.detail ?? JSON.stringify(selected ?? {}, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </section>
  )
}

function RegistryCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(420px,0.9fr)]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <BulkActionBar rows={model.rows} />
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{selected?.primary ?? "Select item"}</CardTitle>
            <CardDescription>{selected?.secondary ?? "Detail, schema, capability, and action context."}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 p-4">
            {selected ? <RowInspector row={selected} /> : <EmptyInline label="No registry rows returned." />}
            <SecondaryRegion model={model} />
          </CardContent>
        </Card>
      </section>
    </>
  )
}

function WorkbenchCanvas({ model }: { model: PageModel }) {
  const isSql = model.path.includes("/sql")
  const isApi = model.path.includes("api-explorer")
  return (
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{model.tableTitle}</CardTitle>
          <CardDescription>{model.tableDescription}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          <div className="grid gap-3 rounded-lg border bg-background p-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-sm font-medium">{isSql ? "SQL editor" : isApi ? "Request body" : "Workbench input"}</div>
              <Button size="sm">{isSql ? "Run query" : isApi ? "Send" : "Run"}</Button>
            </div>
            <Textarea
              className="min-h-44 font-mono text-xs"
              defaultValue={isSql ? "select * from suite_runs order by started_at desc limit 20;" : isApi ? '{\n  "input": {},\n  "tenant": "platform"\n}' : "python -c 'print(42)'"}
            />
          </div>
          <DenseTable columns={model.tableColumns} rows={model.rows} />
        </CardContent>
      </Card>
      <SecondaryRegion model={model} />
    </section>
  )
}

function CustomerCanvas({ model }: { model: PageModel }) {
  return (
    <>
      <KpiGrid model={model} columns="lg:grid-cols-4" />
      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_380px]">
        <Card className="rounded-lg">
          <CardHeader className="border-b">
            <CardTitle>{model.tableTitle}</CardTitle>
            <CardDescription>{model.tableDescription}</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <BulkActionBar rows={model.rows} />
            <DenseTable columns={model.tableColumns} rows={model.rows} />
          </CardContent>
        </Card>
        <SecondaryRegion model={model} />
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

function TableExplorerCanvas({ model }: { model: PageModel }) {
  const selected = model.rows[0]
  const objectTitle = model.title.replace(/^Data \/ /, "")
  return (
    <section className="grid min-h-[560px] gap-4 xl:grid-cols-[320px_minmax(0,1fr)]">
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{objectTitle}</CardTitle>
          <CardDescription>{model.tableDescription}</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <ActivityList rows={model.rows} compact />
        </CardContent>
      </Card>
      <Card className="rounded-lg">
        <CardHeader className="border-b">
          <CardTitle>{selected?.primary ?? `No ${objectTitle.toLowerCase()} selected`}</CardTitle>
          <CardDescription>{selected?.secondary ?? "Select an object to inspect metadata, preview data, and copy a shareable link."}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 p-4">
          <KpiGrid model={model} columns="lg:grid-cols-4" />
          <DenseTable columns={model.tableColumns} rows={model.rows.slice(0, 8)} />
        </CardContent>
      </Card>
    </section>
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
  if (!rows.length) {
    return (
      <div className="p-4">
        <EmptyInline label={compact ? "No objects returned for this scope." : "No activity returned for this scope yet."} />
      </div>
    )
  }

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
  if (!rows.length) {
    return (
      <div className="grid h-56 place-items-center rounded-lg border bg-background p-3">
        <EmptyInline label="No cost events returned for this range." />
      </div>
    )
  }

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

function BulkActionBar({ rows }: { rows: ConsoleRow[] }) {
  return (
    <div className="flex min-h-10 flex-wrap items-center gap-2 border-b px-3 py-2 text-xs text-muted-foreground">
      <Badge variant="outline" className="rounded-full font-mono">
        {rows.length} rows
      </Badge>
      <span>Bulk actions appear after selecting rows.</span>
      <div className="ml-auto flex gap-2">
        <Button variant="ghost" size="sm" className="h-7 gap-1.5">
          <Copy className="size-3.5" />
          Copy links
        </Button>
        <Button variant="ghost" size="sm" className="h-7">
          Export
        </Button>
      </div>
    </div>
  )
}

function RowInspector({ row }: { row: ConsoleRow }) {
  return (
    <div className="grid gap-3">
      <KeyValue label="ID" value={row.id} />
      <KeyValue label="Object" value={row.primary} />
      <KeyValue label="Context" value={row.secondary} />
      <KeyValue label="Status" value={row.status} tone={row.tone} />
      <KeyValue label="Metric" value={row.metric} />
      <KeyValue label="Updated" value={row.timestamp} />
      {row.href && (
        <Button
          variant="outline"
          size="sm"
          className="w-fit gap-2"
          render={
          <Link href={row.href}>
            <ArrowUpRight className="size-3.5" />
            Open share link
          </Link>
          }
        />
      )}
    </div>
  )
}

function TimelineRows({ rows }: { rows: ConsoleRow[] }) {
  if (!rows.length) return <EmptyInline label="No timeline entries returned for this scope." />
  return (
    <div className="divide-y">
      {rows.map((row) => (
        <Link
          key={row.id}
          href={row.href ?? `?item=${encodeURIComponent(row.id)}`}
          className="grid min-h-14 grid-cols-[120px_auto_minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="font-mono text-xs text-muted-foreground">{row.timestamp}</span>
          <Circle className={cn("size-2 fill-current", toneClass[row.tone])} />
          <span className="min-w-0">
            <span className="block truncate text-sm font-medium">{row.primary}</span>
            <span className="block truncate text-xs text-muted-foreground">{row.secondary}</span>
          </span>
          <StatusBadge status={row.status} tone={row.tone} />
        </Link>
      ))}
    </div>
  )
}

function LogRows({ rows }: { rows: ConsoleRow[] }) {
  if (!rows.length) return <EmptyInline label="No log lines returned for the current filters." />
  return (
    <div className="max-h-[680px] overflow-auto font-mono text-xs">
      {rows.map((row) => (
        <Link
          key={row.id}
          href={row.href ?? `?log=${encodeURIComponent(row.id)}`}
          className="grid min-h-8 grid-cols-[92px_72px_minmax(0,1fr)_96px] items-center gap-3 border-b px-3 py-1.5 transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="text-muted-foreground">{row.timestamp}</span>
          <span className={cn("uppercase", toneClass[row.tone])}>{row.status}</span>
          <span className="truncate text-foreground">{row.primary}</span>
          <span className="truncate text-right text-muted-foreground">{row.metric}</span>
        </Link>
      ))}
    </div>
  )
}

function ReadinessMatrix({ rows }: { rows: ConsoleRow[] }) {
  const grouped = rows.slice(0, 8)
  if (!grouped.length) {
    return <EmptyInline label="No service readiness rows returned." />
  }

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
  if (!rows.length) return <EmptyInline label="No rows returned for this page and filter set." />
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
              <RowLink row={row} className="font-medium" value={row.primary} />
              <div className="flex items-center gap-1 truncate font-mono text-xs text-muted-foreground">
                <span className="truncate">{row.id}</span>
                {row.href && <ExternalLink className="size-3 shrink-0" />}
              </div>
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

function RowLink({ row, value, className }: { row: ConsoleRow; value: string; className?: string }) {
  if (!row.href) return <div className={cn("truncate", className)}>{value}</div>
  return (
    <Link
      href={row.href}
      className={cn("block truncate underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
    >
      {value}
    </Link>
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
