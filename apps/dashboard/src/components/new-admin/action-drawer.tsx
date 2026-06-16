// SPDX-License-Identifier: Apache-2.0

"use client"

import { Copy, Play, Save } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"

export function ActionDrawer({ action, page }: { action: string; page: string }) {
  return (
    <Sheet>
      <SheetTrigger
        render={
        <Button size="lg" className="h-9 gap-2">
          <Play className="size-3.5" />
          {action}
        </Button>
        }
      />
      <SheetContent className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{action}</SheetTitle>
          <SheetDescription>
            Drawer mutation pattern for {page}. Saving should write an audit entry and show the SDK equivalent.
          </SheetDescription>
        </SheetHeader>
        <div className="grid flex-1 auto-rows-min gap-5 px-4">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="drawer-name">Name</FieldLabel>
              <Input id="drawer-name" placeholder="prod-server" />
              <FieldDescription>Human-readable alias used in tables and audit logs.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="drawer-scope">Tenant or scope</FieldLabel>
              <Input id="drawer-scope" placeholder="platform or tenant id" />
            </Field>
            <Field>
              <FieldLabel htmlFor="drawer-note">Operator note</FieldLabel>
              <Textarea id="drawer-note" placeholder="Reason for this change..." />
            </Field>
          </FieldGroup>
          <div className="flex items-center justify-between rounded-md border bg-muted/30 p-3">
            <div>
              <Label htmlFor="show-code" className="text-sm">Show as code</Label>
              <p className="text-xs text-muted-foreground">Expose the equivalent SDK or CLI call.</p>
            </div>
            <Switch id="show-code" defaultChecked />
          </div>
          <pre className="overflow-x-auto rounded-md border bg-background p-3 font-mono text-xs text-muted-foreground">
            {`await suite.audit.withEntry("${action.toLowerCase().replaceAll(" ", ".")}", async () => {
  // ${page}
})`}
          </pre>
        </div>
        <SheetFooter>
          <Button size="lg" className="h-9 gap-2">
            <Save className="size-3.5" />
            Save and audit
          </Button>
          <Button variant="outline" size="lg" className="h-9 gap-2" type="button">
            <Copy className="size-3.5" />
            Copy code
          </Button>
          <SheetClose render={<Button variant="ghost">Cancel</Button>} />
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
