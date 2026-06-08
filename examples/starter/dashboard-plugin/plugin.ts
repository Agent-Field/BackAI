// SPDX-License-Identifier: Apache-2.0

import { Sparkles } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "starter",
  label: "Starter Metric",
  name: "Starter Metric",
  icon: Sparkles,
  iconName: "Sparkles",
  description: "One custom metric from this fork.",
  group: "build",
  version: "0.1.0",
})
