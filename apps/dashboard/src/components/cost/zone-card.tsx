// SPDX-License-Identifier: Apache-2.0

// Re-export from ui/ so Cost components that imported "./zone-card"
// keep working. The shared primitive lives in components/ui/zone-card.tsx
// now that Health reuses it.

export { ZoneCard, ZoneCardHeader } from "@/components/ui/zone-card"
