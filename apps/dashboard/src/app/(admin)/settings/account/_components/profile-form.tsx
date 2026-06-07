// SPDX-License-Identifier: Apache-2.0

"use client"

// Profile form. Name is editable via better-auth's `updateUser`. Email is
// read-only here — changing it requires a verification flow we don't ship yet.

import { useState } from "react"
import { useRouter } from "next/navigation"
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

const ProfileSchema = z.object({
  name: z.string().min(1, "Required").max(120, "Too long"),
})

type ProfileValues = z.infer<typeof ProfileSchema>

type ProfileFormProps = {
  defaultName: string
  email: string
}

export function ProfileForm({ defaultName, email }: ProfileFormProps) {
  const router = useRouter()
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<ProfileValues>({
    resolver: zodResolver(ProfileSchema),
    defaultValues: { name: defaultName },
    mode: "onBlur",
  })

  const handleSubmit = async (values: ProfileValues) => {
    setSubmitting(true)
    try {
      const result = await authClient.updateUser({ name: values.name })
      if (result?.error) {
        toast.error(result.error.message ?? "Could not update profile.")
        return
      }
      toast.success("Profile updated.")
      router.refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not update profile.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={form.handleSubmit(handleSubmit)}>
      <FieldGroup>
        <Field data-invalid={form.formState.errors.name ? true : undefined}>
          <FieldLabel htmlFor="profile-name">Name</FieldLabel>
          <Input
            id="profile-name"
            autoComplete="name"
            aria-invalid={form.formState.errors.name ? true : undefined}
            {...form.register("name")}
          />
          {form.formState.errors.name ? (
            <FieldDescription>
              {form.formState.errors.name.message}
            </FieldDescription>
          ) : (
            <FieldDescription>
              How your name appears across the dashboard.
            </FieldDescription>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor="profile-email">Email</FieldLabel>
          <Input
            id="profile-email"
            type="email"
            value={email}
            readOnly
            disabled
          />
          <FieldDescription>
            Changing your sign-in email isn&apos;t supported yet. Reach out to
            another operator or rotate via the database for now.
          </FieldDescription>
        </Field>
        <div>
          <Button
            type="submit"
            size="sm"
            disabled={submitting || !form.formState.isDirty}
          >
            {submitting ? "Saving…" : "Save changes"}
          </Button>
        </div>
      </FieldGroup>
    </form>
  )
}