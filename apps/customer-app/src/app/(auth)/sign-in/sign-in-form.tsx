// SPDX-License-Identifier: Apache-2.0

"use client"

import { Suspense, useState } from "react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Building2Icon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { signIn } from "@/lib/auth-client"
import { brand } from "@/lib/brand"
import { SSO_PROVIDER_ID } from "@/lib/sso"

const SignInSchema = z.object({
  email: z.email("Enter a valid email"),
  password: z.string().min(8, "At least 8 characters"),
})

type SignInValues = z.infer<typeof SignInSchema>

type SignInFormProps = {
  sso?: {
    enabled: boolean
    label: string
  }
}

function SignInInner({ sso }: SignInFormProps) {
  const router = useRouter()
  const searchParams = useSearchParams()
  const next = searchParams.get("next") ?? "/code-helper"
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<SignInValues>({
    resolver: zodResolver(SignInSchema),
    defaultValues: { email: "", password: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: SignInValues) => {
    setSubmitting(true)
    try {
      const result = await signIn.email({
        email: values.email,
        password: values.password,
      })
      if (result.error) {
        toast.error(result.error.message ?? "Could not sign in.")
        return
      }
      router.push(next)
      router.refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not sign in.")
    } finally {
      setSubmitting(false)
    }
  }

  const handleSSO = async () => {
    setSubmitting(true)
    try {
      const result = await signIn.oauth2({
        providerId: SSO_PROVIDER_ID,
        callbackURL: next,
      })
      if (result.error) {
        toast.error(result.error.message ?? "Could not start SSO.")
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not start SSO.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)} className="contents">
      <CardContent>
        <FieldGroup>
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
              autoComplete="current-password"
              aria-invalid={form.formState.errors.password ? true : undefined}
              {...form.register("password")}
            />
            {form.formState.errors.password ? (
              <FieldDescription>{form.formState.errors.password.message}</FieldDescription>
            ) : null}
          </Field>
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting ? "Signing in..." : "Sign in"}
          </Button>
          {sso?.enabled ? (
            <Button
              type="button"
              variant="outline"
              onClick={handleSSO}
              disabled={submitting}
              className="w-full"
            >
              <Building2Icon data-icon="inline-start" /> Continue with {sso.label}
            </Button>
          ) : null}
          <FieldDescription className="text-center">
            New here?{" "}
            <Link className="underline-offset-4 hover:underline" href="/sign-up">
              Create an account
            </Link>
          </FieldDescription>
        </FieldGroup>
      </CardContent>
    </form>
  )
}

export function SignInForm({ sso }: SignInFormProps) {
  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle>Sign in to {brand.displayName}</CardTitle>
        <CardDescription>
          Chat through support cases, account issues, and billing questions.
        </CardDescription>
      </CardHeader>
      <Suspense fallback={null}>
        <SignInInner sso={sso} />
      </Suspense>
    </Card>
  )
}
