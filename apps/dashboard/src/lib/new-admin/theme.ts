// SPDX-License-Identifier: Apache-2.0

export const adminTheme = {
  chrome: {
    sidebarWidth: "206px",
    sidebarRailWidth: "56px",
    topbarHeight: "48px",
    pagePadding: "24px",
    pageGap: "16px",
  },
  motion: {
    fast: "120ms",
    standard: "150ms",
    drawer: "200ms",
    easing: "ease-out",
  },
  typography: {
    pageTitle: "20px / 600",
    section: "15px / 600",
    body: "14px / 400",
    label: "12px / 500",
    data: "13px mono",
  },
  status: {
    ok: "#4ade80",
    fail: "#f87171",
    warn: "#fbbf24",
    running: "#e4e4e7",
  },
} as const
