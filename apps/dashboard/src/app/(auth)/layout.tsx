// SPDX-License-Identifier: Apache-2.0

import { Boxes } from "lucide-react"
import Link from "next/link"

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col">
      <div className="border-b">
        <div className="mx-auto flex max-w-5xl items-center gap-2 px-4 py-4">
          <Link href="/" className="flex items-center gap-2 font-semibold">
            <div className="bg-primary text-primary-foreground flex aspect-square size-7 items-center justify-center rounded-md">
              <Boxes className="size-4" />
            </div>
            AF Stack
          </Link>
        </div>
      </div>
      <div className="flex flex-1 items-center justify-center px-4 py-12">
        {children}
      </div>
    </div>
  )
}