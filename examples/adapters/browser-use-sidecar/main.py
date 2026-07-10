"""
BackAI Browser Sidecar — Playwright Reference Implementation

Implements the HTTP contract the runtime's `browser-use` adapter speaks
(services/runtime/internal/tools/adapters/browser/browseruse):

    POST /navigate      {url, session_id}              -> {title, url, status_code}
    POST /extract-text  {session_id}                   -> {text}
    POST /screenshot    {session_id}                   -> {screenshot_base64}
    POST /click         {selector, session_id}         -> {}
    POST /fill          {selector, value, session_id}  -> {}

One headless Chromium runs for the process lifetime; each session_id maps
to an isolated browser context (empty session_id = shared default). This
is a reference sidecar for development and self-hosting — production
deployments may prefer a hosted provider (steel) or a hardened fork.
"""

import asyncio
import base64
import time

from fastapi import FastAPI, HTTPException
from playwright.async_api import Browser, Page, async_playwright
from pydantic import BaseModel

NAV_TIMEOUT_MS = 30_000
ACTION_TIMEOUT_MS = 10_000

app = FastAPI(title="backai-browser-sidecar")

START_TIME = time.time()


class SessionRequest(BaseModel):
    session_id: str = ""


class NavigateRequest(SessionRequest):
    url: str


class ClickRequest(SessionRequest):
    selector: str


class FillRequest(SessionRequest):
    selector: str
    value: str


class Sessions:
    """Lazily-created Playwright pages keyed by session_id."""

    def __init__(self) -> None:
        self._browser: Browser | None = None
        self._pages: dict[str, Page] = {}
        self._lock = asyncio.Lock()
        self._pw = None

    async def page(self, session_id: str) -> Page:
        async with self._lock:
            if self._browser is None:
                self._pw = await async_playwright().start()
                self._browser = await self._pw.chromium.launch(headless=True)
            page = self._pages.get(session_id)
            if page is None or page.is_closed():
                context = await self._browser.new_context()
                page = await context.new_page()
                page.set_default_timeout(ACTION_TIMEOUT_MS)
                self._pages[session_id] = page
            return page

    async def close(self) -> None:
        async with self._lock:
            if self._browser is not None:
                await self._browser.close()
            if self._pw is not None:
                await self._pw.stop()
            self._pages.clear()
            self._browser = None
            self._pw = None


sessions = Sessions()


@app.on_event("shutdown")
async def shutdown() -> None:
    await sessions.close()


def _bad_gateway(exc: Exception) -> HTTPException:
    return HTTPException(status_code=502, detail=f"{type(exc).__name__}: {exc}")


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok", "uptime_s": int(time.time() - START_TIME)}


@app.post("/navigate")
async def navigate(req: NavigateRequest) -> dict:
    page = await sessions.page(req.session_id)
    try:
        resp = await page.goto(req.url, timeout=NAV_TIMEOUT_MS, wait_until="domcontentloaded")
    except Exception as exc:
        raise _bad_gateway(exc) from exc
    return {
        "url": page.url,
        "title": await page.title(),
        "status_code": resp.status if resp else 0,
    }


@app.post("/extract-text")
async def extract_text(req: SessionRequest) -> dict:
    page = await sessions.page(req.session_id)
    try:
        text = await page.inner_text("body")
    except Exception as exc:
        raise _bad_gateway(exc) from exc
    return {"text": text}


@app.post("/screenshot")
async def screenshot(req: SessionRequest) -> dict:
    page = await sessions.page(req.session_id)
    try:
        png = await page.screenshot(type="png")
    except Exception as exc:
        raise _bad_gateway(exc) from exc
    return {"screenshot_base64": base64.b64encode(png).decode("ascii")}


@app.post("/click")
async def click(req: ClickRequest) -> dict:
    page = await sessions.page(req.session_id)
    try:
        await page.click(req.selector)
    except Exception as exc:
        raise _bad_gateway(exc) from exc
    return {}


@app.post("/fill")
async def fill(req: FillRequest) -> dict:
    page = await sessions.page(req.session_id)
    try:
        await page.fill(req.selector, req.value)
    except Exception as exc:
        raise _bad_gateway(exc) from exc
    return {}
