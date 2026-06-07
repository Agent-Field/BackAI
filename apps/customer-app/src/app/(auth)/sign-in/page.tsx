// SPDX-License-Identifier: Apache-2.0

"use client"

import { Suspense, useState } from "react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { signIn } from "@/lib/auth-client"

const SignInSchema = z.object({
  email: z.email("Enter a valid email"),
  password: z.string().min(8, "At least 8 characters"),
})

type SignInValues = z.infer<typeof SignInSchema>

function SignInInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const next = searchParams.get("next") ?? "/dashboard"
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
              <FieldDescription>
                {form.formState.errors.email.message}
              </FieldDescription>
            ) : null}
          </Field>
          <Field
            data-invalid={form.formState.errors.password ? true : undefined}
          >
            <FieldLabel htmlFor="password">Password</FieldLabel>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              aria-invalid={form.formState.errors.password ? true : undefined}
              {...form.register("password")}
            />
            {form.formState.errors.password ? (
              <FieldDescription>
                {form.formState.errors.password.message}
              </FieldDescription>
            ) : null}
          </Field>
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting ? "Signing in..." : "Sign in"}
          </Button>
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

export default function SignInPage() {
  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle>Sign in to SWE-AF</CardTitle>
        <CardDescription>
          Ask code questions. Watch the gateway answer. See the cost.
        </CardDescription>
      </CardHeader>
      <Suspense fallback={null}>
        <SignInInner />
      </Suspense>
    </Card>
  )
}
