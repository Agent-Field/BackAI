// SPDX-License-Identifier: Apache-2.0

import { requireCustomerContext } from "@/lib/session"
import { SupportChatClient } from "./support-chat-client"

export const dynamic = "force-dynamic"

export default async function SupportPage() {
  const { ctx } = await requireCustomerContext()
  return <SupportChatClient tenantId={ctx.tenantId} />
}
