/**
 * ApiReference.tsx — Scalar API reference embedded in Starlight.
 *
 * Why this component
 * ------------------
 * Starlight pages are static MDX; Scalar is a client-only React widget
 * that mounts an interactive OpenAPI browser. We isolate the import here
 * so the rest of the docs build stays SSR-friendly and only this page
 * pulls in the Scalar bundle.
 *
 * Config rationale
 * ----------------
 *   spec.url   = "/openapi.json"
 *     The snapshot is served by Astro from `public/openapi.json`. Using a
 *     relative URL means the same component works in dev, prod, and
 *     preview without per-env tweaks.
 *
 *   proxyUrl   = "http://localhost:38080"
 *     Scalar's "Try it" panel POSTs through a proxy to bypass CORS. We
 *     point it at the dev runtime; in prod the user can change the
 *     server dropdown to hit their own host.
 *
 *   theme      = "kepler" (a dark Scalar preset that aligns with the
 *     Starlight dark default).
 *
 *   hideClientButton = true
 *     Removes the "Open in client" upsell that markets Scalar's own
 *     desktop app. We want the page to feel native to the docs.
 *
 *   hideDownloadButton = false (default)
 *     The download button is genuinely useful — users grab openapi.json
 *     to feed their own codegen.
 */

import { ApiReferenceReact } from "@scalar/api-reference-react";
import "@scalar/api-reference-react/style.css";

export default function ApiReference() {
  return (
    <ApiReferenceReact
      configuration={{
        url: "/openapi.json",
        proxyUrl: "http://localhost:38080",
        theme: "kepler",
        hideClientButton: true,
        layout: "modern",
        // Keep the sidebar visible — that's the whole point of an API
        // browser. Scalar collapses it on narrow viewports automatically.
        showSidebar: true,
        // Default to the dark variant so the embedded reference matches
        // Starlight's dark-first palette. Scalar's own theme toggle still
        // works in the top-right of the panel.
        darkMode: true,
      }}
    />
  );
}
