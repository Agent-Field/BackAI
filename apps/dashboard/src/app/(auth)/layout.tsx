// SPDX-License-Identifier: Apache-2.0

import { Boxes } from "lucide-react"
import Link from "next/link"

import { brand } from "@/lib/brand"

// Brand-only zone: change operator-console name/logo/colors through root brand.yaml.
// Auth behavior itself is platform wiring.
export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col">
      <div className="border-b">
        <div className="mx-auto flex max-w-5xl items-center gap-2 px-4 py-4">
          <Link href="/" className="flex items-center gap-2 font-semibold">
            <div className="bg-primary text-primary-foreground flex aspect-square size-7 items-center justify-center rounded-md">
              {brand.logos.light ? (
                <img src={brand.logos.light} alt="" className="size-4 object-contain" />
              ) : (
                <Boxes className="size-4" />
              )}
            </div>
            {brand.displayName}
          </Link>
        </div>
      </div>
      <div className="flex flex-1 items-center justify-center px-4 py-12">{children}</div>
    </div>
  )
}
