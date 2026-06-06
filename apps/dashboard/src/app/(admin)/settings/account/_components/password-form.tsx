"use client"

// Change password. Backed by better-auth's `/change-password` endpoint
// (exposed via `authClient.changePassword`).

import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { authClient } from "@/lib/auth-client"

const PasswordSchema = z
  .object({
    currentPassword: z.string().min(8, "Enter your current password"),
    newPassword: z.string().min(8, "At least 8 characters"),
    confirmPassword: z.string(),
  })
  .refine((v) => v.newPassword === v.confirmPassword, {
    path: ["confirmPassword"],
    message: "Passwords don't match",
  })
  .refine((v) => v.newPassword !== v.currentPassword, {
    path: ["newPassword"],
    message: "New password must differ from current",
  })

type PasswordValues = z.infer<typeof PasswordSchema>

export function PasswordForm() {
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<PasswordValues>({
    resolver: zodResolver(PasswordSchema),
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
    mode: "onBlur",
  })

  const handleSubmit = async (values: PasswordValues) => {
    setSubmitting(true)
    try {
      const result = await authClient.changePassword({
        currentPassword: values.currentPassword,
        newPassword: values.newPassword,
        revokeOtherSessions: true,
      })
      if (result?.error) {
        toast.error(result.error.message ?? "Could not change password.")
        return
      }
      toast.success("Password changed. Other sessions were signed out.")
      form.reset({ currentPassword: "", newPassword: "", confirmPassword: "" })
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Could not change password.",
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)}>
      <FieldGroup>
        <Field
          data-invalid={form.formState.errors.currentPassword ? true : undefined}
        >
          <FieldLabel htmlFor="current-password">Current password</FieldLabel>
          <Input
            id="current-password"
            type="password"
            autoComplete="current-password"
            aria-invalid={
              form.formState.errors.currentPassword ? true : undefined
            }
            {...form.register("currentPassword")}
          />
          {form.formState.errors.currentPassword ? (
            <FieldDescription>
              {form.formState.errors.currentPassword.message}
            </FieldDescription>
          ) : null}
        </Field>
        <Field
          data-invalid={form.formState.errors.newPassword ? true : undefined}
        >
          <FieldLabel htmlFor="new-password">New password</FieldLabel>
          <Input
            id="new-password"
            type="password"
            autoComplete="new-password"
            aria-invalid={form.formState.errors.newPassword ? true : undefined}
            {...form.register("newPassword")}
          />
          {form.formState.errors.newPassword ? (
            <FieldDescription>
              {form.formState.errors.newPassword.message}
            </FieldDescription>
          ) : (
            <FieldDescription>Use at least 8 characters.</FieldDescription>
          )}
        </Field>
        <Field
          data-invalid={
            form.formState.errors.confirmPassword ? true : undefined
          }
        >
          <FieldLabel htmlFor="confirm-password">Confirm new password</FieldLabel>
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            aria-invalid={
              form.formState.errors.confirmPassword ? true : undefined
            }
            {...form.register("confirmPassword")}
          />
          {form.formState.errors.confirmPassword ? (
            <FieldDescription>
              {form.formState.errors.confirmPassword.message}
            </FieldDescription>
          ) : null}
        </Field>
        <div>
          <Button type="submit" size="sm" disabled={submitting}>
            {submitting ? "Updating…" : "Update password"}
          </Button>
        </div>
        <FieldDescription>
          Changing your password signs you out of every other session.
        </FieldDescription>
      </FieldGroup>
    </form>
  )
}
