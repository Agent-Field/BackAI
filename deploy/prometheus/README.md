# BackAI Prometheus alert rules

`alerts.yml` ships the default alert rules for the R7 production operating
contract. They cover readiness flapping, LLM provider failure rate, background
job queue backlog, budget-enforcement rejection spikes, outbound webhook
delivery failures, Postgres pool saturation, and backup-test staleness.

Every `backai_*` metric referenced is exported by the runtime at `/metrics`
(registered in `services/runtime/internal/appmetrics`). A Go test
(`TestAlertsReferenceOnlyExistingMetrics`) fails the build if a rule ever
references a metric the runtime doesn't export, so these rules can't rot.

## Wiring

Plain Prometheus:

```yaml
# prometheus.yml
rule_files:
  - /etc/prometheus/alerts.yml       # mount deploy/prometheus/alerts.yml here

scrape_configs:
  - job_name: backai-runtime         # the readiness rules assume this job name
    metrics_path: /metrics
    static_configs:
      - targets: ["runtime:9090"]     # the runtime metrics listener (AF_STACK_METRICS_ADDR)
```

The `up{job="backai-runtime"}` selector in the readiness rules assumes the
scrape job is named `backai-runtime`. If you scrape under a different job name
(e.g. via the Prometheus Operator `ServiceMonitor` in
`deploy/helm/af-stack`), update that selector to match.

## Validating locally

```bash
promtool check rules deploy/prometheus/alerts.yml   # if promtool is installed
go test ./services/runtime/internal/appmetrics/...   # structural + metric-existence check
```

## Notes

- The `/metrics` endpoint is served on its own listener
  (`AF_STACK_METRICS_ADDR`, default `:9090`) so per-tenant cost/usage labels are
  never exposed on the public API port. Restrict it to your Prometheus.
- `BackAIBackupTestStale` only fires once at least one successful backup test
  has been recorded (`backai_backup_test_last_success_timestamp > 0`), so it
  stays quiet when `BACKUP_TEST_ENABLED` is off.
