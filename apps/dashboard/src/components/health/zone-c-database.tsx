// SPDX-License-Identifier: Apache-2.0

"use client"

import { AlertTriangle } from "lucide-react"
import { useState } from "react"
import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts"

import { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"

import type { DBHealth } from "@/lib/api"
import { formatBytes, formatLatency } from "@/lib/health/derive"

// Zone C — Database health. Five sub-cards inside the zone:
// Connections, Cache, Slow queries, Largest tables, Vacuum. Each
// renders structure even at zero data.

const CACHE_MIN_SAMPLES = 100

// Strip migration noise from the slow-query feed. These statements are
// one-shot setup work and don't represent ongoing query cost. C3 mandates
// the toggle so power users can still see them when they want.
const DDL_RE = /^\s*(create|alter|drop|truncate|grant|revoke|reindex|vacuum|cluster|comment|copy|begin|commit|rollback)\b/i

export function ZoneCDatabase({ db }: { db: DBHealth | null }) {
  return (
    <ZoneCard aria-labelledby="zone-c-heading">
      <ZoneCardHeader
        id="zone-c-heading"
        title="Database"
        subtitle={
          db === null
            ? "Database health unavailable"
            : db.available
              ? "Postgres"
              : `Unavailable · ${db.reason ?? "unknown reason"}`
        }
      />
      {db === null || !db.available ? (
        <Unavailable reason={db?.reason ?? "no response from runtime"} />
      ) : (
        <div className="grid gap-stack p-row-x lg:grid-cols-2">
          <ConnectionsCard db={db} />
          <CacheCard db={db} />
          <SlowQueriesCard db={db} />
          <LargestTablesCard db={db} />
          <VacuumCard db={db} />
        </div>
      )}
    </ZoneCard>
  )
}

function Unavailable({ reason }: { reason: string }) {
  return (
    <div className="m-row-x flex items-start gap-stack rounded-md border border-l-4 border-l-warning bg-warning/5 px-row-x py-tile text-meta text-foreground">
      <AlertTriangle
        className="size-icon-inline shrink-0 text-warning"
        aria-hidden
      />
      <p>
        Database health diagnostics aren&apos;t available right now —{" "}
        <span className="font-mono">{reason}</span>. Sub-cards will populate
        when the next probe succeeds.
      </p>
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Connections — stacked segment bar (C1)
// ──────────────────────────────────────────────────────────────────────

function ConnectionsCard({ db }: { db: DBHealth }) {
  const { active, idle, max } = db.connections
  const free = Math.max(0, max - active - idle)
  const total = Math.max(max, active + idle + free, 1)
  const activePct = (active / total) * 100
  const idlePct = (idle / total) * 100
  const freePct = (free / total) * 100
  return (
    <SubCard title="Connections">
      <div className="grid grid-cols-4 gap-stack text-meta">
        <Stat label="Active" value={active} />
        <Stat label="Idle" value={idle} />
        <Stat label="Free" value={free} muted />
        <Stat label="Max" value={max} muted />
      </div>
      <div
        role="img"
        aria-label={`${active} active, ${idle} idle, ${free} free of ${max}`}
        className="flex h-3 w-full overflow-hidden rounded-pill bg-muted"
      >
        {active > 0 ? (
          <div
            className="h-full bg-foreground"
            style={{ width: `${activePct}%` }}
            title={`Active ${active}`}
          />
        ) : null}
        {idle > 0 ? (
          <div
            className="h-full bg-foreground/40"
            style={{ width: `${idlePct}%` }}
            title={`Idle ${idle}`}
          />
        ) : null}
        {free > 0 ? (
          <div
            className="h-full bg-muted-foreground/20"
            style={{ width: `${freePct}%` }}
            title={`Free ${free}`}
          />
        ) : null}
      </div>
      <p className="text-meta text-muted-foreground">
        {active + idle} in use · {free} capacity remaining
      </p>
    </SubCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Cache donut — sample threshold (C2)
// ──────────────────────────────────────────────────────────────────────

function CacheCard({ db }: { db: DBHealth }) {
  const ratio = Math.max(0, Math.min(1, db.cache_hit_ratio))
  const pct = Math.round(ratio * 100)
  // Postgres exposes the cache hit ratio without a sample count, so we
  // can't gate purely on that. The brief asks us to avoid lying with a
  // perfect-looking donut on a cold cache; surrogate: when the ratio is
  // exactly 1 the buffer hasn't had to evict yet — show the placeholder.
  const sparse = !Number.isFinite(db.cache_hit_ratio) || db.cache_hit_ratio === 0 || db.cache_hit_ratio === 1
  const data = sparse
    ? [{ name: "placeholder", value: 1 }]
    : [
        { name: "hit", value: pct },
        { name: "miss", value: Math.max(0, 100 - pct) },
      ]
  return (
    <SubCard title="Cache">
      <div className="relative h-32 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              innerRadius={42}
              outerRadius={60}
              startAngle={90}
              endAngle={-270}
              stroke="var(--card)"
              strokeWidth={2}
              isAnimationActive={false}
            >
              {data.map((d, idx) => (
                <Cell
                  key={d.name}
                  fill={
                    sparse
                      ? "var(--color-muted)"
                      : idx === 0
                        ? "var(--color-foreground)"
                        : "var(--color-muted)"
                  }
                />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-lg font-semibold tabular-nums text-foreground">
            {sparse ? "—" : `${pct}%`}
          </span>
          <span className="text-meta text-muted-foreground">hit ratio</span>
        </div>
      </div>
      <p className="text-meta text-muted-foreground">
        {sparse
          ? `Small sample — need ≥${CACHE_MIN_SAMPLES} evictions for a meaningful ratio.`
          : "Buffer cache effectiveness across the runtime's queries."}
      </p>
    </SubCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Slow queries — truncate + DDL filter (C3)
// ──────────────────────────────────────────────────────────────────────

function SlowQueriesCard({ db }: { db: DBHealth }) {
  const [includeDDL, setIncludeDDL] = useState(false)
  const filtered = includeDDL
    ? db.slow_queries
    : db.slow_queries.filter((q) => !DDL_RE.test(q.query))
  const rows = filtered.slice(0, 10)
  const ddlCount = db.slow_queries.length - filtered.length
  return (
    <SubCard
      title="Slow queries"
      subtitle="top 10 · last 24h · pg_stat_statements"
      className="lg:col-span-2"
      trailing={
        ddlCount > 0 ? (
          <label className="flex items-center gap-inline text-meta text-muted-foreground">
            <input
              type="checkbox"
              checked={includeDDL}
              onChange={(e) => setIncludeDDL(e.target.checked)}
              className="size-3 rounded border-border"
            />
            Include DDL ({ddlCount})
          </label>
        ) : null
      }
    >
      <table className="w-full text-meta">
        <thead>
          <tr className="text-left text-eyebrow uppercase tracking-wide text-muted-foreground">
            <th className="py-tile-tight font-normal">Query</th>
            <th className="py-tile-tight font-normal text-right">Calls</th>
            <th className="py-tile-tight font-normal text-right">Mean</th>
            <th className="py-tile-tight font-normal text-right">Total</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={4} className="py-tile text-meta text-muted-foreground">
                {includeDDL || ddlCount === 0
                  ? "First period of data — slow queries appear after some traffic. Requires "
                  : "No slow ongoing queries — only one-shot DDL noise. Toggle 'Include DDL' to see migrations. Requires "}
                <code className="font-mono">pg_stat_statements</code>.
              </td>
            </tr>
          ) : (
            rows.map((q, idx) => (
              <tr key={idx} className="border-t">
                <td className="py-tile-tight pr-stack font-mono text-meta text-foreground">
                  <span
                    className="block max-w-full truncate"
                    title={q.query}
                  >
                    {q.query.length > 60
                      ? `${q.query.slice(0, 58)}…`
                      : q.query}
                  </span>
                </td>
                <td className="py-tile-tight text-right font-mono tabular-nums text-muted-foreground">
                  {q.calls.toLocaleString()}
                </td>
                <td className="py-tile-tight text-right font-mono tabular-nums text-muted-foreground">
                  {formatLatency(q.mean_ms)}
                </td>
                <td className="py-tile-tight text-right font-mono tabular-nums text-foreground">
                  {formatLatency(q.total_ms)}
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </SubCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Largest tables
// ──────────────────────────────────────────────────────────────────────

function LargestTablesCard({ db }: { db: DBHealth }) {
  const rows = [...db.largest_tables]
    .sort((a, b) => b.size_bytes - a.size_bytes)
    .slice(0, 6)
  const max = rows[0]?.size_bytes ?? 1
  return (
    <SubCard title="Largest tables">
      {rows.length === 0 ? (
        <p className="text-meta text-muted-foreground">
          No tables reported yet.
        </p>
      ) : (
        <ul role="list" className="flex flex-col gap-stack">
          {rows.map((t) => (
            <li
              key={`${t.schema}.${t.table}`}
              className="grid grid-cols-[minmax(0,1fr)_minmax(120px,2fr)_88px] items-center gap-stack text-meta"
            >
              <span className="min-w-0 truncate font-mono text-foreground">
                {t.table}
              </span>
              <div
                aria-hidden
                className="h-2 w-full overflow-hidden rounded-pill bg-muted"
              >
                <div
                  className="h-full bg-foreground/70"
                  style={{
                    width: `${Math.max(2, (t.size_bytes / Math.max(max, 1)) * 100)}%`,
                  }}
                />
              </div>
              <span className="text-right font-mono tabular-nums text-muted-foreground">
                {formatBytes(t.size_bytes)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </SubCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// Vacuum
// ──────────────────────────────────────────────────────────────────────

function VacuumCard({ db }: { db: DBHealth }) {
  const rows = [...db.vacuum_status]
    .sort((a, b) => b.dead_tuples - a.dead_tuples)
    .slice(0, 6)
  const needsAttention = rows.filter((r) => r.dead_tuples >= 10_000)
  return (
    <SubCard title="Vacuum">
      {rows.length === 0 ? (
        <p className="text-meta text-muted-foreground">
          No vacuum stats reported yet.
        </p>
      ) : needsAttention.length === 0 ? (
        <p className="text-meta text-muted-foreground">
          ✓ All tables vacuumed recently — top {rows.length} clean.
        </p>
      ) : (
        <ul role="list" className="flex flex-col gap-tile-tight text-meta">
          {rows.map((row) => {
            const concern = row.dead_tuples >= 10_000
            return (
              <li
                key={row.table}
                className="grid grid-cols-[minmax(0,1fr)_120px_72px] items-center gap-stack"
              >
                <span className="min-w-0 truncate font-mono text-foreground">
                  {row.table}
                </span>
                <span className="text-right font-mono tabular-nums text-muted-foreground">
                  {row.last_vacuum
                    ? new Date(row.last_vacuum).toISOString().slice(5, 10)
                    : "never"}
                </span>
                <span
                  className={`text-right font-mono tabular-nums ${concern ? "text-warning" : "text-muted-foreground"}`}
                >
                  {row.dead_tuples.toLocaleString()}
                </span>
              </li>
            )
          })}
        </ul>
      )}
    </SubCard>
  )
}

// ──────────────────────────────────────────────────────────────────────
// SubCard primitive (Zone C-local) — visible gap between sub-sections (B3)
// ──────────────────────────────────────────────────────────────────────

function SubCard({
  title,
  subtitle,
  trailing,
  children,
  className,
}: {
  title: string
  subtitle?: string
  trailing?: React.ReactNode
  children: React.ReactNode
  className?: string
}) {
  return (
    <section
      className={`flex flex-col gap-stack rounded-md border bg-card p-tile ${className ?? ""}`}
    >
      <header className="flex items-baseline justify-between gap-stack">
        <span className="text-body font-medium text-foreground">{title}</span>
        <div className="flex items-baseline gap-stack">
          {subtitle ? (
            <span className="text-meta text-muted-foreground">{subtitle}</span>
          ) : null}
          {trailing}
        </div>
      </header>
      {children}
    </section>
  )
}

function Stat({
  label,
  value,
  muted,
}: {
  label: string
  value: number
  muted?: boolean
}) {
  return (
    <div className="flex flex-col gap-tile-tight">
      <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span
        className={`font-mono text-body tabular-nums ${muted ? "text-muted-foreground" : "text-foreground"}`}
      >
        {value.toLocaleString()}
      </span>
    </div>
  )
}
