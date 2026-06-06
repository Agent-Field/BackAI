"use client"

// Active sessions. Fetched client-side via `authClient.listSessions()`.
// "Sign out everywhere" calls `revokeOtherSessions` so the current session
// stays alive — operators almost never want to lock themselves out.

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Globe, Monitor } from "lucide-react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { authClient } from "@/lib/auth-client"

type SessionSummary = {
  id: string
  current: boolean
  ipAddress: string | null
  userAgent: string | null
  createdAt: string | null
  expiresAt: string | null
}

function formatDate(value: string | null): string {
  if (!value) return "—"
  try {
    return new Date(value).toLocaleString()
  } catch {
    return value
  }
}

function summarizeUserAgent(ua: string | null): string {
  if (!ua) return "Unknown device"
  // Cheap heuristic — keep tiny, no real UA parser dep.
  const platform = /Mac OS X/i.test(ua)
    ? "macOS"
    : /Windows/i.test(ua)
      ? "Windows"
      : /Linux/i.test(ua)
        ? "Linux"
        : /Android/i.test(ua)
          ? "Android"
          : /iPhone|iPad/i.test(ua)
            ? "iOS"
            : "Unknown"
  const browser = /Edg\//i.test(ua)
    ? "Edge"
    : /Chrome\//i.test(ua)
      ? "Chrome"
      : /Safari\//i.test(ua)
        ? "Safari"
        : /Firefox\//i.test(ua)
          ? "Firefox"
          : "Browser"
  return `${browser} on ${platform}`
}

export function SessionsPanel() {
  const router = useRouter()
  const [loading, setLoading] = useState(true)
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [revoking, setRevoking] = useState(false)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const currentResult = await authClient.getSession()
        const currentId =
          (currentResult?.data as { session?: { id?: string } } | null)?.session
            ?.id ?? null

        const listResult = await authClient.listSessions()
        const list =
          (listResult?.data as
            | Array<{
                id?: string
                ipAddress?: string | null
                userAgent?: string | null
                createdAt?: string | Date | null
                expiresAt?: string | Date | null
              }>
            | null
            | undefined) ?? []
        const summaries: SessionSummary[] = list.map((s) => ({
          id: s.id ?? "",
          current: !!currentId && s.id === currentId,
          ipAddress: s.ipAddress ?? null,
          userAgent: s.userAgent ?? null,
          createdAt:
            s.createdAt instanceof Date
              ? s.createdAt.toISOString()
              : (s.createdAt ?? null),
          expiresAt:
            s.expiresAt instanceof Date
              ? s.expiresAt.toISOString()
              : (s.expiresAt ?? null),
        }))
        // Current session first, then most recent.
        summaries.sort((a, b) => {
          if (a.current !== b.current) return a.current ? -1 : 1
          return (b.createdAt ?? "").localeCompare(a.createdAt ?? "")
        })
        if (!cancelled) setSessions(summaries)
      } catch {
        if (!cancelled) setSessions([])
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [])

  const handleRevokeOthers = async () => {
    setRevoking(true)
    try {
      const result = await authClient.revokeOtherSessions()
      if (result?.error) {
        toast.error(result.error.message ?? "Could not sign out other sessions.")
        return
      }
      toast.success("Signed out of every other session.")
      setSessions((prev) => prev.filter((s) => s.current))
      router.refresh()
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Could not sign out other sessions.",
      )
    } finally {
      setRevoking(false)
    }
  }

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }

  if (sessions.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No active sessions to show.
      </p>
    )
  }

  const otherCount = sessions.filter((s) => !s.current).length

  return (
    <div className="flex flex-col gap-4">
      <ul className="flex flex-col gap-3">
        {sessions.map((s) => (
          <li
            key={s.id}
            className="border-border/60 flex items-start gap-3 rounded-md border p-3"
          >
            <div className="bg-muted text-foreground/80 flex aspect-square size-9 shrink-0 items-center justify-center rounded-md">
              <Monitor className="size-4" />
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex items-center gap-2">
                <span className="truncate text-sm font-medium">
                  {summarizeUserAgent(s.userAgent)}
                </span>
                {s.current ? (
                  <Badge variant="secondary" className="text-[10px]">
                    Current
                  </Badge>
                ) : null}
              </div>
              <div className="text-muted-foreground flex flex-wrap gap-x-3 gap-y-0.5 text-xs">
                <span className="inline-flex items-center gap-1">
                  <Globe className="size-3" />
                  {s.ipAddress ?? "Unknown IP"}
                </span>
                <span>Signed in {formatDate(s.createdAt)}</span>
                <span>Expires {formatDate(s.expiresAt)}</span>
              </div>
            </div>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-muted-foreground text-xs">
          {otherCount === 0
            ? "Only this session is active."
            : `${otherCount} other ${otherCount === 1 ? "session" : "sessions"} active.`}
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRevokeOthers}
          disabled={revoking || otherCount === 0}
        >
          {revoking ? "Signing out…" : "Sign out everywhere else"}
        </Button>
      </div>
    </div>
  )
}
