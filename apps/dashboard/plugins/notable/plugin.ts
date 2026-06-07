// Notable dashboard plugin (example 01).
//
// Adds a sidebar entry under "Customers" because operators reach for
// this page when answering "which tenant is using how much?" — that's
// a customer-management question, not an operate/build question.
//
// Discovered at build time by scripts/generate-plugins-manifest.mjs;
// the route proxy at src/app/(admin)/plugins/notable/page.tsx is
// generated automatically.

import { Notebook } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "notable",
  label: "Notable",
  name: "Notable",
  icon: Notebook,
  iconName: "Notebook",
  description:
    "Per-tenant note counts + token spend from the Notable example app.",
  group: "customers",
  version: "0.1.0",
})
