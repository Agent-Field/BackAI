// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Sparkles } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
}

const ONBOARDING_KEY_STORAGE = "backai:onboarding-api-key"

export default function SignUpPage() {
  const router = useRouter()
  const [submitting, setSubmitting] = useState(false)

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
      // Better-auth auto-signs-in. The support account scaffold is created by
      // the user.create.after hook. We mint an internal demo token so the chat
      // can call the runtime, but the customer app does not expose API keys.
      const res = await fetch("/api/customer/onboarding-key", {
        method: "POST",
        credentials: "include",
      })
      if (res.ok) {
        const data: OnboardingKey = await res.json()
        try {
          window.sessionStorage.setItem(ONBOARDING_KEY_STORAGE, data.api_key_token)
        } catch {
          // Chat can mint a fresh internal token later if storage is unavailable.
        }
      }
      router.push("/dashboard")
      router.refresh()
    } finally {
      setSubmitting(false)
    }
  }

  const handleUseDemoDetails = () => {
    const suffix = Date.now().toString(36)
    form.setValue("name", "Demo Customer", { shouldDirty: true, shouldValidate: true })
    form.setValue("email", `demo+${suffix}@backai.local`, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue("password", "backai-demo-pwd", {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle>Create your {brand.displayName} account</CardTitle>
        <CardDescription>
          Create your account and get help with realistic support cases.
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
              {submitting ? "Creating..." : "Create account"}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleUseDemoDetails}
              disabled={submitting}
              className="w-full"
            >
              <Sparkles data-icon="inline-start" />
              Use demo details
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
  )
}
