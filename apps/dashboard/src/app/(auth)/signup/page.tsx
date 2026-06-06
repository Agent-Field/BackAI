"use client"

import { useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { signUp } from "@/lib/auth-client"

const SignupSchema = z.object({
  name: z.string().min(1, "What should we call you?"),
  email: z.email("Enter a valid email"),
  password: z.string().min(8, "Use at least 8 characters"),
})

type SignupValues = z.infer<typeof SignupSchema>

export default function SignupPage() {
  const router = useRouter()
  const [submitting, setSubmitting] = useState(false)
  const form = useForm<SignupValues>({
    resolver: zodResolver(SignupSchema),
    defaultValues: { name: "", email: "", password: "" },
    mode: "onBlur",
  })

  const handleSubmit = async (values: SignupValues) => {
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
      router.push("/")
      router.refresh()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle>Create an account</CardTitle>
        <CardDescription>
          Operators sign in here. Customers and end-users live in your product.
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
                <FieldDescription>
                  {form.formState.errors.name.message}
                </FieldDescription>
              ) : null}
            </Field>
            <Field
              data-invalid={form.formState.errors.email ? true : undefined}
            >
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
                autoComplete="new-password"
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
              {submitting ? "Creating account…" : "Create account"}
            </Button>
            <FieldDescription className="text-center">
              Already have an account?{" "}
              <Link
                className="underline-offset-4 hover:underline"
                href="/login"
              >
                Sign in
              </Link>
            </FieldDescription>
          </FieldGroup>
        </CardContent>
      </form>
    </Card>
  )
}
