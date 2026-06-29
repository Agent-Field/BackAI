// SPDX-License-Identifier: Apache-2.0

// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import mdx from "@astrojs/mdx";
import react from "@astrojs/react";

const GITHUB_REPO = "https://github.com/Agent-Field/backai";
const GITHUB_EDIT_BASE = `${GITHUB_REPO}/edit/main/docs-site/`;

export default defineConfig({
  // MDX + React only used by the API reference page today (Scalar). Adding
  // them globally costs nothing for the rest of the site and lets future
  // pages embed interactive widgets without re-wiring the config.
  integrations: [
    // Integration order matters in Astro 5:
    //   1. starlight() — registers astro-expressive-code internally.
    //      Expressive Code requires "I come before mdx()".
    //   2. mdx() — adds `.mdx` to the content collection extensions
    //      Starlight's docsLoader globs. Must come after starlight.
    //   3. react() — needed by the API reference page (Scalar widget).
    starlight({
      title: "AF Stack",
      description: "The open backend for the AI era",
      // Drop a logo.svg in src/assets later if desired:
      //   logo: { src: "./src/assets/logo.svg" }
      // Starlight 0.32+ takes `social` as an array of icon links
      // (was an object keyed by platform in <=0.31).
      social: [{ icon: "github", label: "GitHub", href: GITHUB_REPO }],
      editLink: {
        baseUrl: GITHUB_EDIT_BASE,
      },
      // Pagefind search is enabled by default in Starlight; nothing to
      // configure unless we want to disable it.
      // Dark mode default — Starlight respects user preference via the
      // theme switcher but defaults to dark when CSS hints it.
      customCss: ["./src/styles/custom.css"],
      sidebar: [
        {
          label: "Get Started",
          items: [
            { label: "Introduction", link: "/get-started/introduction/" },
            { label: "Quickstart (60s)", link: "/get-started/quickstart/" },
            { label: "Architecture", link: "/get-started/architecture/" },
          ],
        },
        {
          label: "Guides",
          items: [
            {
              label: "Customize the dashboard",
              link: "/guides/customize-dashboard/",
            },
            { label: "Swap defaults", link: "/guides/swap-defaults/" },
            { label: "Theming", link: "/guides/theming/" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Modules", link: "/reference/modules/" },
            { label: "Adapters", link: "/reference/adapters/" },
            { label: "Sandbox adapters", link: "/reference/adapters/sandbox/" },
            { label: "Hooks", link: "/reference/hooks/" },
            { label: "API Reference", link: "/reference/api/" },
            { label: "API Conventions", link: "/reference/api-conventions/" },
            { label: "Workload Modules", link: "/reference/workload-modules/" },
            { label: "Dashboard Plugins", link: "/reference/dashboard-plugins/" },
            { label: "Backup & Restore", link: "/reference/backup-restore/" },
          ],
        },
        {
          label: "Deploy",
          items: [
            { label: "Overview", link: "/deploy/" },
            {
              label: "Helm chart",
              link: `${GITHUB_REPO}/blob/main/deploy/helm/af-stack/README.md`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "Fly.io",
              link: `${GITHUB_REPO}/blob/main/deploy/fly/README.md`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "Railway",
              link: `${GITHUB_REPO}/blob/main/deploy/railway/README.md`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "Render",
              link: `${GITHUB_REPO}/blob/main/deploy/render/README.md`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            { label: "Multi-replica", link: "/deploy/multi-replica/" },
            { label: "Production internals", link: "/deploy/internals/" },
          ],
        },
        {
          label: "Examples",
          items: [
            {
              label: "Overview",
              link: `${GITHUB_REPO}/blob/main/examples/README.md`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "03 — LLM gateway only",
              link: `${GITHUB_REPO}/blob/main/examples/03-llm-gateway-only/`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "01 — Notable (full SaaS)",
              link: `${GITHUB_REPO}/blob/main/examples/01-notable/`,
              attrs: { target: "_blank", rel: "noopener" },
            },
            {
              label: "06 — Deep research",
              link: `${GITHUB_REPO}/blob/main/examples/06-deep-research/`,
              attrs: { target: "_blank", rel: "noopener" },
            },
          ],
        },
        {
          label: "Launch",
          items: [
            { label: "Blog post draft", link: "/launch/blog-post/" },
            { label: "Social drafts", link: "/launch/social/" },
          ],
        },
      ],
    }),
    mdx(),
    react(),
  ],
});