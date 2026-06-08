// SPDX-License-Identifier: Apache-2.0

import { redirect } from "next/navigation"
import Link from "next/link"
import { Boxes } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { brand } from "@/lib/brand"
import { operatorCount } from "@/lib/session"

export default async function SetupPage() {
  const count = await operatorCount()
  if (count > 0) {
    redirect("/")
  }
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-12">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="bg-primary text-primary-foreground mb-2 flex aspect-square size-10 items-center justify-center rounded-md">
            {brand.logos.light ? (
              <img src={brand.logos.light} alt="" className="size-6 object-contain" />
            ) : (
              <Boxes className="size-5" />
            )}
          </div>
          <CardTitle>Welcome to {brand.displayName}</CardTitle>
          <CardDescription>
            Create the first operator account. This account governs the entire deployment.
            Subsequent users sign up via invitation.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-3">
            <Button render={<Link href="/signup">Create operator account</Link>} />
            <p className="text-muted-foreground text-center text-xs">
              Already configured?{" "}
              <Link className="underline-offset-4 hover:underline" href="/login">
                Sign in
              </Link>
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
