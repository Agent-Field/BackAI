// SPDX-License-Identifier: Apache-2.0

"use client"

import Link from "next/link"
import { Eye, KeyRound } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

type Props = {
  prefix: string | null
}

export function ApiKeyPanel({ prefix }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" />
          Your API key
        </CardTitle>
        <CardDescription>
          Send this token as a Bearer header to the LLM gateway. The full key
          was shown once at signup.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="rounded-md border bg-muted p-3 font-mono text-xs break-all">
          {prefix ? `af_${prefix}_•••••••••••••••••••••••••••••••••••••••••••` : "—"}
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            className="flex-1"
            nativeButton={false}
            render={
              <Link href="/api-key">
                <Eye data-icon="inline-start" />
                Manage keys
              </Link>
            }
          />
        </div>
      </CardContent>
    </Card>
  )
}
