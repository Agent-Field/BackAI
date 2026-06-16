# Prometheus Metrics E2E

This compose file is for the Block 5 Prometheus adapter E2E only. It is not
part of the operator stack.

```bash
docker compose -f services/runtime/internal/observability/metrics/adapters/prometheus/testdata/docker-compose.e2e.yml up -d
BACKAI_E2E_PROM_URL=http://localhost:9090 go test -tags=e2e_prom ./services/runtime/internal/observability/metrics/adapters/prometheus
docker compose -f services/runtime/internal/observability/metrics/adapters/prometheus/testdata/docker-compose.e2e.yml down -v
```

The test Prometheus scrapes itself, cAdvisor, and a runtime on
`host.docker.internal:18080` when one is running.
