// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import { Copy } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

type Props = {
  prefix: string | null
}

function getGatewayUrl(): string {
  if (typeof window !== "undefined") {
    return (
      process.env.NEXT_PUBLIC_RUNTIME_URL ??
      window.location.origin.replace(/:34000$/, ":38080").replace(/:34001$/, ":38080")
    )
  }
  return "http://localhost:38080"
}

export function QuickstartPanel({ prefix }: Props) {
  const [tab, setTab] = useState("curl")
  const gateway = getGatewayUrl()
  const keyDisplay = prefix
    ? `af_${prefix}_<your-secret>`
    : "af_<your-key-prefix>_<your-secret>"

  const curl = `curl ${gateway}/api/v1/llm/chat/completions \\
  -H "Authorization: Bearer ${keyDisplay}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "qwen/qwen-2.5-72b-instruct",
    "messages": [
      {"role": "user", "content": "Draft a helpful reply for a customer asking about a refund."}
    ]
  }'`

  const python = `from openai import OpenAI

client = OpenAI(
    base_url="${gateway}/api/v1/llm",
    api_key="${keyDisplay}",
)

resp = client.chat.completions.create(
    model="qwen/qwen-2.5-72b-instruct",
    messages=[{"role": "user", "content": "Draft a helpful reply for a customer asking about a refund."}],
)
print(resp.choices[0].message.content)`

  const handleCopy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    toast.success("Copied.")
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>API quickstart</CardTitle>
        <CardDescription>
          OpenAI-compatible. Point your existing client at BackAI and usage
          shows up in the admin dashboard.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="curl">curl</TabsTrigger>
            <TabsTrigger value="python">Python</TabsTrigger>
          </TabsList>
          <TabsContent value="curl" className="mt-3">
            <div className="relative">
              <pre className="overflow-x-auto rounded-md border bg-muted p-3 text-xs">
                <code>{curl}</code>
              </pre>
              <Button
                variant="ghost"
                size="icon-sm"
                className="absolute top-2 right-2"
                onClick={() => handleCopy(curl)}
                aria-label="Copy curl"
              >
                <Copy />
              </Button>
            </div>
          </TabsContent>
          <TabsContent value="python" className="mt-3">
            <div className="relative">
              <pre className="overflow-x-auto rounded-md border bg-muted p-3 text-xs">
                <code>{python}</code>
              </pre>
              <Button
                variant="ghost"
                size="icon-sm"
                className="absolute top-2 right-2"
                onClick={() => handleCopy(python)}
                aria-label="Copy python"
              >
                <Copy />
              </Button>
            </div>
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  )
}
