// Shared formatting helpers for the Cost page. Keeping these in one
// file makes the page consistent (every dollar value formats the same)
// and easy to tweak later.

// Currency formatter that adapts precision to magnitude. Sub-cent values
// (where 2-digit precision would round to $0.00) get extra decimals so the
// operator can actually see what they spent.
export function formatCurrency(usd: number): string {
  if (!Number.isFinite(usd)) return "$0.00"
  const abs = Math.abs(usd)
  const digits = abs === 0 ? 2 : abs < 0.0001 ? 6 : abs < 0.01 ? 4 : 2
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(usd)
}

export function formatCurrencyCompact(usd: number): string {
  if (!Number.isFinite(usd)) return "$0"
  const abs = Math.abs(usd)
  if (abs < 1000) {
    const digits = abs === 0 ? 2 : abs < 0.0001 ? 6 : abs < 0.01 ? 4 : 2
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: "USD",
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }).format(usd)
  }
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(usd)
}

export function formatPercentDelta(curr: number, prev: number): {
  text: string
  direction: "up" | "down" | "flat"
} {
  if (prev === 0 || !Number.isFinite(prev)) {
    if (curr === 0) return { text: "—", direction: "flat" }
    return { text: "new", direction: "up" }
  }
  const pct = ((curr - prev) / prev) * 100
  const direction: "up" | "down" | "flat" =
    Math.abs(pct) < 0.05 ? "flat" : pct > 0 ? "up" : "down"
  const sign = pct > 0 ? "+" : ""
  return { text: `${sign}${pct.toFixed(1)}%`, direction }
}
