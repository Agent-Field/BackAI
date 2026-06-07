"""Pydantic models shared by the admin sub-package.

Each model mirrors a zod schema declared in
``apps/dashboard/src/lib/api.ts`` (the canonical contract). Keep the
field names — and especially the snake_case wire keys — in sync. The
dashboard's ``safeParse`` is the canary that catches drift; these
models are the SDK-side mirror.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

# ─── Tenants ──────────────────────────────────────────────────────────────


class Tenant(BaseModel):
    """Mirrors ``TenantSchema``.

    ``settings`` and ``quota`` are free-form maps; the runtime emits ``{}``
    rather than ``null`` so we default to an empty dict here too.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    slug: str
    name: str
    plan: str
    settings: dict[str, Any] = Field(default_factory=dict)
    quota: dict[str, Any] = Field(default_factory=dict)
    created_at: str
    deleted_at: str | None = None


class TenantList(BaseModel):
    """Mirrors ``TenantListSchema``."""

    model_config = ConfigDict(extra="allow")

    tenants: list[Tenant] = Field(default_factory=list)


class CreateTenantInput(BaseModel):
    """Mirrors ``CreateTenantInputSchema``.

    Validation rules (slug charset, length) live server-side; the SDK
    sends the raw payload through and surfaces server-side errors via
    :class:`AFStackError`.
    """

    model_config = ConfigDict(extra="allow")

    slug: str
    name: str
    plan: str | None = None


class UpdateTenantInput(BaseModel):
    """Mirrors ``UpdateTenantInputSchema``.

    Each field is optional; the server treats a missing field as "leave
    alone" and an empty dict for ``settings``/``quota`` as "clear".
    """

    model_config = ConfigDict(extra="allow")

    name: str | None = None
    plan: str | None = None
    settings: dict[str, Any] | None = None
    quota: dict[str, Any] | None = None


# ─── Users ────────────────────────────────────────────────────────────────


class User(BaseModel):
    """Mirrors ``UserSchema``."""

    model_config = ConfigDict(extra="allow")

    id: str
    # Wire field is a string; full RFC 5322 validation happens server-side.
    # Mirrors zod's ``z.email()`` shape without adding ``email-validator``
    # as a hard dependency.
    email: str
    name: str | None = None
    avatar_url: str | None = None
    created_at: str
    deleted_at: str | None = None


class UserList(BaseModel):
    """Mirrors ``UserListSchema``."""

    model_config = ConfigDict(extra="allow")

    users: list[User] = Field(default_factory=list)


# ─── Memberships ──────────────────────────────────────────────────────────

# Roles are validated server-side; mirror the enum so static checkers
# catch obvious typos client-side too.
Role = Literal["owner", "admin", "member", "viewer"]


class Membership(BaseModel):
    """Mirrors ``MembershipSchema``."""

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    user_id: str
    role: Role
    invited_at: str
    accepted_at: str | None = None


class MembershipList(BaseModel):
    """Mirrors ``MembershipListSchema``."""

    model_config = ConfigDict(extra="allow")

    memberships: list[Membership] = Field(default_factory=list)


# ─── API keys ─────────────────────────────────────────────────────────────


class APIKey(BaseModel):
    """Mirrors ``APIKeySchema`` (no plaintext value).

    The plaintext is ONLY ever returned by ``POST /api/v1/admin/keys`` and
    is modelled separately in :class:`IssuedAPIKey`. List/get/detail flows
    must never include it.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str
    prefix: str
    name: str | None = None
    scopes: list[str] = Field(default_factory=list)
    created_by: str | None = None
    created_at: str
    last_used_at: str | None = None
    expires_at: str | None = None
    revoked_at: str | None = None


class APIKeyList(BaseModel):
    """Mirrors ``APIKeyListSchema``."""

    model_config = ConfigDict(extra="allow")

    keys: list[APIKey] = Field(default_factory=list)


class IssuedAPIKey(APIKey):
    """Mirrors ``IssuedAPIKeySchema`` = ``APIKey`` + ``value``.

    Returned ONCE by :func:`af_stack.admin.keys.issue`. ``value`` is the
    plaintext secret; persist it before the request returns, the runtime
    cannot re-derive it.
    """

    model_config = ConfigDict(extra="allow")

    value: str


class IssueAPIKeyInput(BaseModel):
    """Mirrors ``IssueAPIKeyInputSchema``."""

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    name: str | None = None
    scopes: list[str] = Field(default_factory=list)
    expires_at: str | None = None


# ─── Tenant detail (joined view) ──────────────────────────────────────────


class TenantMember(BaseModel):
    """Mirrors ``TenantDetailSchema.members[]``."""

    model_config = ConfigDict(extra="allow")

    user: User
    role: str


class TenantUsage(BaseModel):
    """Mirrors ``TenantDetailSchema.usage``."""

    model_config = ConfigDict(extra="allow")

    requests_30d: int = 0
    cost_usd_30d: float = 0.0
    storage_bytes: int = 0
    secrets_count: int = 0


class TenantDetail(BaseModel):
    """Mirrors ``TenantDetailSchema``.

    Single-shot fetch for the dashboard tenant detail page: the tenant
    record + members + api keys + 30-day usage in one round-trip.
    """

    model_config = ConfigDict(extra="allow")

    tenant: Tenant
    members: list[TenantMember] = Field(default_factory=list)
    api_keys: list[APIKey] = Field(default_factory=list)
    usage: TenantUsage = Field(default_factory=TenantUsage)


# ─── Budgets (Phase 7) ────────────────────────────────────────────────────


class Budget(BaseModel):
    """Mirrors ``BudgetSchema`` in ``apps/dashboard/src/lib/api.ts``.

    Returned by both the per-tenant ``GET /admin/budgets/{tenant_id}``
    and as a row in ``GET /admin/budgets``.
    """

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    monthly_usd: float
    alert_threshold_pct: float
    spent_this_period_usd: float
    remaining_usd: float
    resets_at: str


class BudgetList(BaseModel):
    """Mirrors ``BudgetListSchema``."""

    model_config = ConfigDict(extra="allow")

    budgets: list[Budget] = Field(default_factory=list)


class SetBudgetInput(BaseModel):
    """Mirrors ``SetBudgetInputSchema``.

    ``alert_threshold_pct`` defaults to 80 on the server when omitted;
    we keep the same default here so the wire payload is stable.
    """

    model_config = ConfigDict(extra="allow")

    tenant_id: str
    monthly_usd: float
    alert_threshold_pct: float = 80.0


# ─── Audit ────────────────────────────────────────────────────────────────


class AuditEntry(BaseModel):
    """Mirrors ``AuditEntrySchema``."""

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str | None = None
    user_id: str | None = None
    api_key_id: str | None = None
    action: str
    resource_type: str | None = None
    resource_id: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)
    occurred_at: str


class AuditList(BaseModel):
    """Mirrors ``AuditListSchema``."""

    model_config = ConfigDict(extra="allow")

    entries: list[AuditEntry] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


__all__ = [
    "APIKey",
    "APIKeyList",
    "AuditEntry",
    "AuditList",
    "Budget",
    "BudgetList",
    "CreateTenantInput",
    "IssueAPIKeyInput",
    "IssuedAPIKey",
    "Membership",
    "MembershipList",
    "Role",
    "SetBudgetInput",
    "Tenant",
    "TenantDetail",
    "TenantList",
    "TenantMember",
    "TenantUsage",
    "UpdateTenantInput",
    "User",
    "UserList",
]
