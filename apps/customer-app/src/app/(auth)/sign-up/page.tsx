// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Copy, KeyRound } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { signUp } from "@/lib/auth-client"
import { brand } from "@/lib/brand"

const SignUpSchema = z.object({
  name: z.string().min(1, "What should we call you?"),
  email: z.email("Enter a valid email"),
  password: z.string().min(8, "Use at least 8 characters"),
})

type SignUpValues = z.infer<typeof SignUpSchema>

type OnboardingKey = {
  api_key_token: string
  api_key_prefix: string
  tenant_name: string
}

export default function SignUpPage() {
  const router = useRouter()
  const [submitting, setSubmitting] = useState(false)
  const [keyDialog, setKeyDialog] = useState<OnboardingKey | null>(null)

  const form = useForm<SignUpValues>({
    resolver: zodResolver(SignUpSchema),
    defaultValues: { name: "", email: "", password: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: SignUpValues) => {
    setSubmitting(true)
    try {
      const result = await signUp.email({
        name: values.name,
        email: values.email,
        password: values.password,
      })
      if (result.error) {
        toast.error(result.error.message ?? "Could not sign up.")
        return
      }
      // Better-auth auto-signs-in. The tenant scaffold was created by the
      // user.create.after hook in lib/auth.ts. We now mint a one-shot
      // plaintext key to show the customer.
      const res = await fetch("/api/customer/onboarding-key", {
        method: "POST",
        credentials: "include",
      })
      if (!res.ok) {
        // Still let them in — they can reveal the prefix from /dashboard
        // and issue another key later from /api-key.
        toast.error("Signed up, but could not generate your API key. Retry from API Key page.")
        router.push("/dashboard")
        return
      }
      const data: OnboardingKey = await res.json()
      setKeyDialog(data)
    } finally {
      setSubmitting(false)
    }
  }

  const handleCopy = async () => {
    if (!keyDialog) return
    await navigator.clipboard.writeText(keyDialog.api_key_token)
    toast.success("API key copied to clipboard.")
  }

  const handleContinue = () => {
    router.push("/dashboard")
    router.refresh()
  }

  return (
    <>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Create your {brand.displayName} account</CardTitle>
          <CardDescription>
            Create a support workspace with a tenant, billing record, and API key.
          </CardDescription>
        </CardHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)} className="contents">
          <CardContent>
            <FieldGroup>
              <Field data-invalid={form.formState.errors.name ? true : undefined}>
                <FieldLabel htmlFor="name">Name</FieldLabel>
                <Input
                  id="name"
                  autoComplete="name"
                  aria-invalid={form.formState.errors.name ? true : undefined}
                  {...form.register("name")}
                />
                {form.formState.errors.name ? (
                  <FieldDescription>{form.formState.errors.name.message}</FieldDescription>
                ) : null}
              </Field>
              <Field data-invalid={form.formState.errors.email ? true : undefined}>
                <FieldLabel htmlFor="email">Email</FieldLabel>
                <Input
                  id="email"
                  type="email"
                  autoComplete="email"
                  aria-invalid={form.formState.errors.email ? true : undefined}
                  {...form.register("email")}
                />
                {form.formState.errors.email ? (
                  <FieldDescription>{form.formState.errors.email.message}</FieldDescription>
                ) : null}
              </Field>
              <Field data-invalid={form.formState.errors.password ? true : undefined}>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <Input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  aria-invalid={form.formState.errors.password ? true : undefined}
                  {...form.register("password")}
                />
                {form.formState.errors.password ? (
                  <FieldDescription>{form.formState.errors.password.message}</FieldDescription>
                ) : null}
              </Field>
              <Button type="submit" disabled={submitting} className="w-full">
                {submitting ? "Provisioning..." : "Create account"}
              </Button>
              <FieldDescription className="text-center">
                Have an account?{" "}
                <Link className="underline-offset-4 hover:underline" href="/sign-in">
                  Sign in
                </Link>
              </FieldDescription>
            </FieldGroup>
          </CardContent>
        </form>
      </Card>

      <Dialog
        open={!!keyDialog}
        onOpenChange={(open) => {
          if (!open) handleContinue()
        }}
      >
        <DialogContent showCloseButton={false} className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="size-4" />
              Your API key
            </DialogTitle>
            <DialogDescription>
              This is the only time you will see the full key. Copy it now. You can issue a new one
              from the API Key page if you lose it.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="rounded-md border bg-muted p-3 font-mono text-xs break-all">
              {keyDialog?.api_key_token}
            </div>
            <div className="flex gap-2">
              <Button onClick={handleCopy} variant="outline" className="flex-1">
                <Copy data-icon="inline-start" />
                Copy
              </Button>
              <Button onClick={handleContinue} className="flex-1">
                Continue to workspace
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
