# Third-Party Licenses

AF Stack is licensed under Apache 2.0 (see `LICENSE`). It bundles or links
against third-party software listed below.

Direct dependencies are listed here. Transitive dependencies are licensed
compatibly (Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, or ISC). If you
spot a license that needs separate notice, please open an issue.

This file is generated and lightly hand-curated; for the full transitive
tree run `go list -m -json all`, `pnpm licenses list`, or `pip-licenses`
locally.

---

## Go (`go.mod`)

| Package | Version | License | Source |
|---|---|---|---|
| github.com/BurntSushi/toml | v1.6.0 | MIT | https://github.com/BurntSushi/toml |
| github.com/docker/docker | v28.5.2+incompatible | Apache-2.0 | https://github.com/moby/moby |
| github.com/jackc/pgx/v5 | v5.10.0 | MIT | https://github.com/jackc/pgx |
| github.com/minio/minio-go/v7 | v7.2.0 | Apache-2.0 | https://github.com/minio/minio-go |
| github.com/pressly/goose/v3 | v3.27.1 | MIT | https://github.com/pressly/goose |
| github.com/prometheus/client_golang | v1.23.2 | Apache-2.0 | https://github.com/prometheus/client_golang |
| github.com/riverqueue/river | v0.39.0 | MPL-2.0 | https://github.com/riverqueue/river |
| github.com/riverqueue/river/riverdriver/riverpgxv5 | v0.39.0 | MPL-2.0 | https://github.com/riverqueue/river |
| github.com/riverqueue/river/rivertype | v0.39.0 | MPL-2.0 | https://github.com/riverqueue/river |
| github.com/robfig/cron/v3 | v3.0.1 | MIT | https://github.com/robfig/cron |
| github.com/stripe/stripe-go/v82 | v82.5.1 | MIT | https://github.com/stripe/stripe-go |
| go.opentelemetry.io/otel | v1.44.0 | Apache-2.0 | https://github.com/open-telemetry/opentelemetry-go |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace | v1.44.0 | Apache-2.0 | https://github.com/open-telemetry/opentelemetry-go |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc | v1.44.0 | Apache-2.0 | https://github.com/open-telemetry/opentelemetry-go |
| go.opentelemetry.io/otel/sdk | v1.44.0 | Apache-2.0 | https://github.com/open-telemetry/opentelemetry-go |
| go.opentelemetry.io/otel/trace | v1.44.0 | Apache-2.0 | https://github.com/open-telemetry/opentelemetry-go |
| golang.org/x/crypto | v0.51.0 | BSD-3-Clause | https://cs.opensource.google/go/x/crypto |
| golang.org/x/time | v0.15.0 | BSD-3-Clause | https://cs.opensource.google/go/x/time |
| gopkg.in/yaml.v3 | v3.0.1 | MIT + Apache-2.0 | https://github.com/go-yaml/yaml |

**Notice on River Queue (MPL-2.0):** River and its subpackages
(`riverdriver/riverpgxv5`, `rivertype`) are distributed under the
Mozilla Public License 2.0. We link against River as a library; under
MPL-2.0 this is permitted without source disclosure of AF Stack itself.
If you modify River source files, the MPL-2.0 file-level reciprocity
applies to those files only. See https://www.mozilla.org/MPL/2.0/.

---

## Node.js (`apps/dashboard`, `packages/sdk-ts`, `docs-site`)

Direct dependencies across the three workspaces. Use
`pnpm -r licenses list` to regenerate a full per-workspace breakdown.

### apps/dashboard

| Package | Version | License | Source |
|---|---|---|---|
| @base-ui/react | ^1.5.0 | MIT | https://github.com/mui/base-ui |
| @hookform/resolvers | ^5.4.0 | MIT | https://github.com/react-hook-form/resolvers |
| @tanstack/react-table | ^8.21.3 | MIT | https://github.com/TanStack/table |
| @types/pg | ^8.20.0 | MIT | https://github.com/DefinitelyTyped/DefinitelyTyped |
| better-auth | ^1.6.14 | MIT | https://github.com/better-auth/better-auth |
| class-variance-authority | ^0.7.1 | Apache-2.0 | https://github.com/joe-bell/cva |
| clsx | ^2.1.1 | MIT | https://github.com/lukeed/clsx |
| cmdk | ^1.1.1 | MIT | https://github.com/pacocoursey/cmdk |
| date-fns | ^4.4.0 | MIT | https://github.com/date-fns/date-fns |
| kysely | ^0.28.0 | MIT | https://github.com/kysely-org/kysely |
| lucide-react | ^1.17.0 | ISC | https://github.com/lucide-icons/lucide |
| next | 16.2.7 | MIT | https://github.com/vercel/next.js |
| next-themes | ^0.4.6 | MIT | https://github.com/pacocoursey/next-themes |
| pg | ^8.21.0 | MIT | https://github.com/brianc/node-postgres |
| react | 19.2.4 | MIT | https://github.com/facebook/react |
| react-dom | 19.2.4 | MIT | https://github.com/facebook/react |
| react-hook-form | ^7.77.0 | MIT | https://github.com/react-hook-form/react-hook-form |
| recharts | 3.8.0 | MIT | https://github.com/recharts/recharts |
| shadcn | ^4.10.0 | MIT | https://github.com/shadcn-ui/ui |
| sonner | ^2.0.7 | MIT | https://github.com/emilkowalski/sonner |
| tailwind-merge | ^3.6.0 | MIT | https://github.com/dcastil/tailwind-merge |
| tw-animate-css | ^1.4.0 | MIT | https://github.com/Wombosvideo/tw-animate-css |
| zod | ^4.4.3 | MIT | https://github.com/colinhacks/zod |

### packages/sdk-ts

| Package | Version | License | Source |
|---|---|---|---|
| zod | ^3.23.8 | MIT | https://github.com/colinhacks/zod |

### docs-site

| Package | Version | License | Source |
|---|---|---|---|
| @astrojs/mdx | ^4.0.0 | MIT | https://github.com/withastro/astro |
| @astrojs/react | ^5.0.7 | MIT | https://github.com/withastro/astro |
| @astrojs/starlight | ^0.30.0 | MIT | https://github.com/withastro/starlight |
| @scalar/api-reference-react | ^0.9.42 | MIT | https://github.com/scalar/scalar |
| astro | ^5.0.0 | MIT | https://github.com/withastro/astro |
| react | ^19.0.0 | MIT | https://github.com/facebook/react |
| react-dom | ^19.0.0 | MIT | https://github.com/facebook/react |
| sharp | ^0.33.0 | Apache-2.0 | https://github.com/lovell/sharp |

---

## Python (`packages/sdk-py`)

| Package | Version | License | Source |
|---|---|---|---|
| httpx | >= 0.27 | BSD-3-Clause | https://github.com/encode/httpx |
| pydantic | >= 2.0 | MIT | https://github.com/pydantic/pydantic |
| anyio | >= 4.0 | MIT | https://github.com/agronholm/anyio |

---

## Regenerating this file

```bash
# Go
go list -m -json all | jq -r '.Path + " " + .Version'
# or, with go-licenses installed:
go-licenses report ./services/runtime/...

# Node (per workspace)
pnpm -r licenses list

# Python (in the sdk-py venv)
pip-licenses --format=markdown
```
