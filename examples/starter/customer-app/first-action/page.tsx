// SPDX-License-Identifier: Apache-2.0

"use client"

import * as React from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"

export default function FirstActionPage() {
  const [text, setText] = React.useState("Hello from my AF Stack fork.")
  const [result, setResult] = React.useState<string>("")
  const [pending, setPending] = React.useState(false)

  async function runFirstAction() {
    setPending(true)
    setResult("")
    try {
      const response = await fetch("/api/v1/agents/starter.echo", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ input: { message: text } }),
      })
      const body = await response.json()
      setResult(JSON.stringify(body, null, 2))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">First action</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          A logged-in customer action that calls your starter agent.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Call starter.echo</CardTitle>
          <CardDescription>
            Replace this with the first useful workflow in your product.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Textarea value={text} onChange={(event) => setText(event.target.value)} />
          <Button onClick={runFirstAction} disabled={pending}>
            {pending ? "Running..." : "Run"}
          </Button>
          {result ? (
            <pre className="bg-muted overflow-auto rounded-md p-4 text-xs">{result}</pre>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
