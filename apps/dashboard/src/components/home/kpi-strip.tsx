// SPDX-License-Identifier: Apache-2.0

import type { KpiTileModel } from "@/lib/home/types"
import { KpiTile } from "./kpi-tile"

// Eight tiles arranged in a responsive grid. Mobile: 1 column. sm: 2.
// md: 4 (two rows). xl: 8 (single row). Gap and any padding come from
// theme tokens — no hardcoded numbers.

export function KpiStrip({ tiles }: { tiles: KpiTileModel[] }) {
  return (
    <div
      role="list"
      aria-label="Key performance indicators"
      className="grid grid-cols-1 gap-stack sm:grid-cols-2 md:grid-cols-4 xl:grid-cols-8"
    >
      {tiles.map((tile) => (
        <div key={tile.id} role="listitem">
          <KpiTile tile={tile} />
        </div>
      ))}
    </div>
  )
}
