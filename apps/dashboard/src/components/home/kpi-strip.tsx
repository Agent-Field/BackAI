// SPDX-License-Identifier: Apache-2.0

import type { KpiTileModel } from "@/lib/home/types"
import { KPI_GOOD_DIRECTION } from "@/lib/home/derive"

import { KpiTile } from "./kpi-tile"

// Eight tiles in a single dense strip with hairline dividers. Every cell
// is `overflow-hidden` so the inner sparkline can never leak past its
// own boundary into the neighbour.

export function KpiStrip({ tiles }: { tiles: KpiTileModel[] }) {
  return (
    <section
      aria-label="Key performance indicators"
      className="overflow-hidden rounded-md border bg-card"
    >
      <ul
        role="list"
        className="grid grid-cols-2 md:grid-cols-4 xl:grid-cols-8"
      >
        {tiles.map((tile, idx) => (
          <li
            key={tile.id}
            className={cellClass(idx, tiles.length)}
          >
            <KpiTile
              tile={tile}
              goodDirection={KPI_GOOD_DIRECTION[tile.id]}
            />
          </li>
        ))}
      </ul>
    </section>
  )
}

// Hand-rolled dividers because divide-x / divide-y don't combine cleanly
// across a wrapping grid. Each cell decides whether to draw its top/left
// border based on its position; the parent's overflow-hidden hides any
// edge bleed.
function cellClass(idx: number, _total: number): string {
  const base = "relative min-w-0 overflow-hidden"
  // Top border on rows 2+ (md: rows after 4; xl: never, all in one row).
  // Left border on every cell except col-1 at each breakpoint.
  return [
    base,
    // 2-col layout (mobile): top border on row 2+
    idx >= 2 ? "border-t" : "",
    // 2-col: left border on the right-side column
    idx % 2 !== 0 ? "border-l" : "",
    // md: 4-col: top border for rows 2+ (idx >= 4)
    "md:border-t-0",
    idx >= 4 ? "md:border-t" : "",
    // md: left border for col 2/3/4 (idx % 4 !== 0)
    "md:border-l-0",
    idx % 4 !== 0 ? "md:border-l" : "",
    // xl: 8-col, single row: only left borders (col 2..8)
    "xl:border-t-0",
    idx % 8 !== 0 ? "xl:border-l" : "",
  ]
    .filter(Boolean)
    .join(" ")
}
