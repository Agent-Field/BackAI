// SPDX-License-Identifier: Apache-2.0

// Standard empty-state stub used by every dashboard tab that doesn't have
// a real implementation yet. Built on shadcn `Empty`.
//
// Each unimplemented tab still gets a header so the breadcrumb path,
// command-palette navigation, and screen-reader landmarks all work.

import type { ComponentType, ReactNode } from "react"

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Button } from "@/components/ui/button"
import { PageHeader } from "@/components/layout/page-header"
import type { LucideIcon } from "lucide-react"
import Link from "next/link"

type TabStubProps = {
  title: string
  description?: string
  icon: LucideIcon | ComponentType
  body: ReactNode
}

export function TabStub({ title, description, icon: Icon, body }: TabStubProps) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={title} description={description} />
      <Empty className="border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Icon />
          </EmptyMedia>
          {body}
        </EmptyHeader>
      </Empty>
    </div>
  )
}

type ComingSoonProps = {
  title: string
  description?: string
  icon: LucideIcon
  /**
   * Path under `docs/` to a relevant document the operator can read.
   * Optional.
   */
  docHref?: string
  bodyTitle: string
  bodyDescription: string
}

export function ComingSoon({
  title,
  description,
  icon: Icon,
  docHref,
  bodyTitle,
  bodyDescription,
}: ComingSoonProps) {
  return (
    <TabStub
      title={title}
      description={description}
      icon={Icon}
      body={
        <>
          <EmptyTitle>{bodyTitle}</EmptyTitle>
          <EmptyDescription>{bodyDescription}</EmptyDescription>
          {docHref ? (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                size="sm"
                nativeButton={false}
                render={<Link href={docHref}>Read the docs →</Link>}
              />
            </div>
          ) : null}
        </>
      }
    />
  )
}

type MultiTenancyRequiredProps = {
  title: string
  description?: string
  icon: LucideIcon
}

/** Empty state shown for `Customers/*` tabs when the MT module is disabled. */
export function MultiTenancyRequired({
  title,
  description,
  icon,
}: MultiTenancyRequiredProps) {
  return (
    <ComingSoon
      title={title}
      description={description}
      icon={icon}
      bodyTitle="Enable multi-tenancy to use this section"
      bodyDescription="Multi-tenancy lets you isolate data, secrets, and quotas per customer. It's a module you can turn on in config.yaml."
      docHref="https://github.com/Agent-Field/backai/blob/main/docs/dashboard-ia.md"
    />
  )
}
