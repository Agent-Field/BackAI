# GlitchTip E2E

This compose file is intentionally local to the GlitchTip adapter tests and is
not part of the operator stack.

```bash
docker compose -f services/runtime/internal/observability/errors/adapters/glitchtip/testdata/docker-compose.e2e.yml up -d
```

Create an organization, project, and auth token in the GlitchTip UI at
`http://localhost:18095`, then run:

```bash
BACKAI_E2E_GLITCHTIP_URL=http://localhost:18095 \
BACKAI_E2E_GLITCHTIP_ORG=<org-slug> \
BACKAI_E2E_GLITCHTIP_TOKEN=<token> \
go test -tags=e2e_glitchtip ./services/runtime/internal/observability/errors/adapters/glitchtip
```

For automated local runs, seed a minimal org/token/issue through GlitchTip's
Django ORM:

```bash
docker compose -f services/runtime/internal/observability/errors/adapters/glitchtip/testdata/docker-compose.e2e.yml exec -T glitchtip python manage.py shell <<'PY'
from django.apps import apps
from django.contrib.auth import get_user_model
from django.utils import timezone

User = get_user_model()
Org = apps.get_model("organizations_ext", "Organization")
OU = apps.get_model("organizations_ext", "OrganizationUser")
Team = apps.get_model("teams", "Team")
Project = apps.get_model("projects", "Project")
Issue = apps.get_model("issue_events", "Issue")
APIToken = apps.get_model("api_tokens", "APIToken")

try:
    user = User.objects.get(email="e2e@example.com")
except User.DoesNotExist:
    user = User.objects.create_user(email="e2e@example.com", password="e2e-pass")
org, _ = Org.objects.get_or_create(slug="backai-e2e", defaults={"name": "BackAI E2E"})
OU.objects.get_or_create(user=user, organization=org, defaults={"email": user.email, "role": 3})
team, _ = Team.objects.get_or_create(slug="backai", organization=org)
project, _ = Project.objects.get_or_create(slug="runtime", organization=org, defaults={"name": "runtime", "platform": "python"})
try:
    project.teams.add(team)
except Exception:
    pass
Issue.objects.update_or_create(
    project=project,
    short_id=1,
    defaults={"title": "BackAI e2e deliberate error", "culprit": "runtime.worker", "metadata": {}, "count": 3, "last_seen": timezone.now()},
)
token, _ = APIToken.objects.update_or_create(user=user, label="backai-e2e", defaults={"token": "backai-e2e-token", "scopes": 2**16 - 1})
print("BACKAI_E2E_GLITCHTIP_ORG=backai-e2e")
print("BACKAI_E2E_GLITCHTIP_TOKEN=backai-e2e-token")
PY
```

The main runtime compose file is not modified by this test.
