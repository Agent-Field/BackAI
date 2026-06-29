// SPDX-License-Identifier: Apache-2.0

import { AlertTriangle } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

// Banner rendered when every Home data source failed at the network
// layer. The middleware already gates auth, so this state means the
// runtime itself is unreachable — surface that clearly and leave the
// sidebar / nav rendered (per home.md §20 question 5).

export function RuntimeUnreachable({ details }: { details?: string }) {
  return (
    <Alert variant="destructive">
      <AlertTriangle />
      <AlertTitle>Runtime unreachable</AlertTitle>
      <AlertDescription>
        {details ??
          "The dashboard could not reach the runtime. Verify the RUNTIME_URL env var and that the runtime container is up."}
      </AlertDescription>
    </Alert>
  )
}
