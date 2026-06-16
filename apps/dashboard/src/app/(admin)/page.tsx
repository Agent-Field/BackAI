// SPDX-License-Identifier: Apache-2.0

import { notFound } from "next/navigation"

import { OperatorPage } from "@/components/new-admin/operator-page"
import { getOperatorSnapshot } from "@/lib/new-admin/data"
import { buildPageModel } from "@/lib/new-admin/page-model"

export const dynamic = "force-dynamic"

export default async function Page() {
  const snapshot = await getOperatorSnapshot()
  const model = buildPageModel("/", snapshot)
  if (!model) notFound()
  return <OperatorPage model={model} />
}
