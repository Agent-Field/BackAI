// SPDX-License-Identifier: Apache-2.0

import type { KpiTileModel } from "@/lib/home/types"
import { KPI_GOOD_DIRECTION } from "@/lib/home/derive"

import { KpiTile } from "./kpi-tile"

// Eight tiles arranged in a single dense strip. One row at xl, wraps at
// narrow screens. Hairline vertical dividers between tiles, no card
// chrome — the page-level surface frames the strip.

export function KpiStrip({ tiles }: { tiles: KpiTileModel[] }) {
  return (
    <section
      aria-label="Key performance indicators"
      className="rounded-md border bg-card"
    >
      <ul
        role="list"
        className="grid grid-cols-2 divide-y divide-x md:grid-cols-4 xl:grid-cols-8 xl:divide-y-0"
      >
        {tiles.map((tile) => (
          <li key={tile.id} className="min-w-0">
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
