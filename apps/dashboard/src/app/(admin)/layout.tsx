// SPDX-License-Identifier: Apache-2.0

import { AdminShell } from "@/components/new-admin/admin-shell"

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return <AdminShell>{children}</AdminShell>
}
