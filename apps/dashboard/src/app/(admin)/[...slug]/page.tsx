// SPDX-License-Identifier: Apache-2.0

import { notFound } from "next/navigation"

import { OperatorPage } from "@/components/new-admin/operator-page"
import { getOperatorSnapshot } from "@/lib/new-admin/data"
import { buildPageModel } from "@/lib/new-admin/page-model"

export const dynamic = "force-dynamic"

type PageProps = {
  params: Promise<{ slug: string[] }>
}

export default async function Page({ params }: PageProps) {
  const { slug } = await params
  const snapshot = await getOperatorSnapshot()
  const model = buildPageModel(`/${slug.join("/")}`, snapshot)
  if (!model) notFound()
  return <OperatorPage model={model} />
}
