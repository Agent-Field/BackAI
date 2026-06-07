// SPDX-License-Identifier: Apache-2.0

import { requireCustomerContext } from "@/lib/session"
import { CodeHelperClient } from "./code-helper-client"

export const dynamic = "force-dynamic"

export default async function CodeHelperPage() {
  const { ctx } = await requireCustomerContext()
  return (
    <CodeHelperClient
      tenantId={ctx.tenantId}
      apiKeyPrefix={ctx.apiKeyPrefix}
    />
  )
}
