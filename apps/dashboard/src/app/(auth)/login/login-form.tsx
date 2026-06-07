// SPDX-License-Identifier: Apache-2.0

"use client"

import { Suspense, useState } from "react"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { MailIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { signIn } from "@/lib/auth-client"

const LoginSchema = z.object({
  email: z.email("Enter a valid email"),
  password: z.string().min(8, "At least 8 characters"),
})

type LoginValues = z.infer<typeof LoginSchema>

function LoginFormInner() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const next = searchParams.get("next") ?? "/"
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<LoginValues>({
    resolver: zodResolver(LoginSchema),
    defaultValues: { email: "", password: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: LoginValues) => {
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

  const handleMagicLink = async () => {
    const email = form.getValues("email")
    const parsed = z.email().safeParse(email)
    if (!parsed.success) {
      form.setError("email", { message: "Enter a valid email" })
      return
    }
    setSubmitting(true)
    try {
      const result = await signIn.magicLink({ email })
      if (result.error) {
        toast.error(result.error.message ?? "Could not send magic link.")
        return
      }
      toast.success("Magic link sent. Check your email.")
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
            {submitting ? "Signing in…" : "Sign in"}
          </Button>
          <Separator />
          <Button
            type="button"
            variant="outline"
            onClick={handleMagicLink}
            disabled={submitting}
            className="w-full"
          >
            <MailIcon data-icon="inline-start" /> Email me a magic link
          </Button>
          <FieldDescription className="text-center">
            Don&apos;t have an account?{" "}
            <Link className="underline-offset-4 hover:underline" href="/signup">
              Sign up
            </Link>
          </FieldDescription>
        </FieldGroup>
      </CardContent>
    </form>
  )
}

export function LoginForm() {
  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle>Sign in</CardTitle>
        <CardDescription>Welcome back to your stack.</CardDescription>
      </CardHeader>
      <Suspense fallback={null}>
        <LoginFormInner />
      </Suspense>
    </Card>
  )
}