{{/*
Standard Helm helpers — chart name / fullname / labels / selectorLabels.
These follow the bitnami-style conventions so the chart drops in next to
other Helm charts without name collisions.
*/}}

{{/* Expand the name of the chart. */}}
{{- define "af-stack.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Create a default fully qualified app name. Truncated at 63 chars because
some Kubernetes name fields are limited to that.
*/}}
{{- define "af-stack.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/* Chart name + version, used as a label value. */}}
{{- define "af-stack.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Common labels applied to every object. */}}
{{- define "af-stack.labels" -}}
helm.sh/chart: {{ include "af-stack.chart" . }}
{{ include "af-stack.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: af-stack
{{- end }}

{{/*
Selector labels — only the keys Kubernetes uses to select pods.
DO NOT include version/managed-by here; the selector is immutable and
adding mutable labels breaks `helm upgrade`.
*/}}
{{- define "af-stack.selectorLabels" -}}
app.kubernetes.io/name: {{ include "af-stack.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Per-workload (runtime / dashboard) full names. */}}
{{- define "af-stack.runtime.fullname" -}}
{{- printf "%s-runtime" (include "af-stack.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "af-stack.dashboard.fullname" -}}
{{- printf "%s-dashboard" (include "af-stack.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Per-workload selector labels — add component to disambiguate. */}}
{{- define "af-stack.runtime.selectorLabels" -}}
{{ include "af-stack.selectorLabels" . }}
app.kubernetes.io/component: runtime
{{- end }}

{{- define "af-stack.dashboard.selectorLabels" -}}
{{ include "af-stack.selectorLabels" . }}
app.kubernetes.io/component: dashboard
{{- end }}

{{- define "af-stack.runtime.labels" -}}
{{ include "af-stack.labels" . }}
app.kubernetes.io/component: runtime
{{- end }}

{{- define "af-stack.dashboard.labels" -}}
{{ include "af-stack.labels" . }}
app.kubernetes.io/component: dashboard
{{- end }}

{{/* ServiceAccount name. */}}
{{- define "af-stack.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "af-stack.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}

{{/*
Secret holding inline LLM keys + KMS key + auth secret. Only rendered
when the operator passes inline values (i.e. NOT using secret refs).
Other templates reference this name conditionally.
*/}}
{{- define "af-stack.secretName" -}}
{{- printf "%s-secrets" (include "af-stack.fullname" .) -}}
{{- end }}

{{/*
Postgres URL — resolve either to the bitnami sub-chart's service or the
external URL/secretRef. Returns "" if no DB is configured (validated by
the deployment template).
*/}}
{{- define "af-stack.databaseUrlSource" -}}
{{- if .Values.postgresql.enabled -}}
internal
{{- else if .Values.postgresql.external.urlSecretRef.name -}}
secretRef
{{- else if .Values.postgresql.external.url -}}
inline
{{- else -}}
{{- fail "postgresql is disabled and no external.url or external.urlSecretRef.name is set" -}}
{{- end -}}
{{- end }}

{{/* Compute the internal sub-chart Postgres URL when enabled. */}}
{{- define "af-stack.internalPostgresUrl" -}}
{{- $svc := printf "%s-postgresql" .Release.Name -}}
{{- printf "postgres://%s:$(POSTGRES_PASSWORD)@%s:5432/%s?sslmode=disable" .Values.postgresql.auth.username $svc .Values.postgresql.auth.database -}}
{{- end }}

{{/* Module env vars — emit AF_STACK_MODULE_<NAME>=true|false for each. */}}
{{- define "af-stack.moduleEnv" -}}
- name: AF_STACK_MODULE_MULTI_TENANCY
  value: {{ .Values.modules.multiTenancy.enabled | quote }}
- name: AF_STACK_MODULE_SANDBOX
  value: {{ .Values.modules.sandbox.enabled | quote }}
- name: AF_STACK_MODULE_LLM_GATEWAY
  value: {{ .Values.modules.llmGateway.enabled | quote }}
- name: AF_STACK_MODULE_LLM_CACHE
  value: {{ .Values.modules.llmCache.enabled | quote }}
- name: AF_STACK_MODULE_MEMORY
  value: {{ .Values.modules.memory.enabled | quote }}
- name: AF_STACK_MODULE_BILLING
  value: {{ .Values.modules.billing.enabled | quote }}
- name: AF_STACK_MODULE_NOTIFICATIONS
  value: {{ .Values.modules.notifications.enabled | quote }}
- name: AF_STACK_MODULE_WEBHOOKS
  value: {{ .Values.modules.webhooks.enabled | quote }}
- name: AF_STACK_SANDBOX_ADAPTER
  value: {{ .Values.modules.sandbox.adapter | quote }}
{{- end }}

{{/*
Storage env vars. Mode "off" emits nothing; mode "s3" or "minio" emits
the AF_STACK_S3_* contract. Credentials prefer secretRef when set.
*/}}
{{- define "af-stack.storageEnv" -}}
{{- if ne .Values.storage.mode "off" -}}
- name: AF_STACK_S3_ADAPTER
  value: {{ ternary "minio" "s3" (eq .Values.storage.mode "minio") | quote }}
- name: AF_STACK_S3_BUCKET
  value: {{ .Values.storage.s3.bucket | quote }}
- name: AF_STACK_S3_REGION
  value: {{ .Values.storage.s3.region | quote }}
{{- if eq .Values.storage.mode "minio" }}
- name: AF_STACK_S3_ENDPOINT
  value: {{ printf "%s-minio:9000" .Release.Name | quote }}
{{- else if .Values.storage.s3.endpoint }}
- name: AF_STACK_S3_ENDPOINT
  value: {{ .Values.storage.s3.endpoint | quote }}
{{- end }}
{{- if .Values.storage.s3.credentialsSecretRef.name }}
- name: AF_STACK_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.storage.s3.credentialsSecretRef.name | quote }}
      key: {{ .Values.storage.s3.credentialsSecretRef.accessKeyKey | quote }}
- name: AF_STACK_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.storage.s3.credentialsSecretRef.name | quote }}
      key: {{ .Values.storage.s3.credentialsSecretRef.secretKeyKey | quote }}
{{- else if eq .Values.storage.mode "minio" }}
- name: AF_STACK_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ printf "%s-minio" .Release.Name | quote }}
      key: root-user
- name: AF_STACK_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ printf "%s-minio" .Release.Name | quote }}
      key: root-password
{{- else }}
- name: AF_STACK_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: AF_STACK_S3_ACCESS_KEY
- name: AF_STACK_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: AF_STACK_S3_SECRET_KEY
{{- end }}
{{- end }}
{{- end }}

{{/*
Database URL env block — resolves to internal sub-chart or external
secretRef / inline.
*/}}
{{- define "af-stack.databaseEnv" -}}
{{- $source := include "af-stack.databaseUrlSource" . -}}
{{- if eq $source "internal" }}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ printf "%s-postgresql" .Release.Name | quote }}
      key: password
- name: AF_STACK_DATABASE_URL
  value: {{ include "af-stack.internalPostgresUrl" . | quote }}
{{- else if eq $source "secretRef" }}
- name: AF_STACK_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.postgresql.external.urlSecretRef.name | quote }}
      key: {{ .Values.postgresql.external.urlSecretRef.key | quote }}
{{- else if eq $source "inline" }}
- name: AF_STACK_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: AF_STACK_DATABASE_URL
{{- end }}
{{- end }}

{{/* KMS key env. */}}
{{- define "af-stack.kmsEnv" -}}
{{- if .Values.secrets.kmsKeySecretRef.name }}
- name: AF_STACK_KMS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.secrets.kmsKeySecretRef.name | quote }}
      key: {{ .Values.secrets.kmsKeySecretRef.key | quote }}
{{- else if .Values.secrets.kmsKey }}
- name: AF_STACK_KMS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: AF_STACK_KMS_KEY
{{- end }}
{{- end }}

{{/* LLM provider env. Pulls from providerSecretRef if set, else inline. */}}
{{- define "af-stack.llmEnv" -}}
{{- if .Values.llm.providerSecretRef.name }}
{{- range $provider := list "OPENROUTER_API_KEY" "ANTHROPIC_API_KEY" "OPENAI_API_KEY" "GOOGLE_API_KEY" }}
- name: {{ $provider }}
  valueFrom:
    secretKeyRef:
      name: {{ $.Values.llm.providerSecretRef.name | quote }}
      key: {{ $provider }}
      optional: true
{{- end }}
{{- else }}
{{- if .Values.llm.openrouterApiKey }}
- name: OPENROUTER_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: OPENROUTER_API_KEY
{{- end }}
{{- if .Values.llm.anthropicApiKey }}
- name: ANTHROPIC_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: ANTHROPIC_API_KEY
{{- end }}
{{- if .Values.llm.openaiApiKey }}
- name: OPENAI_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: OPENAI_API_KEY
{{- end }}
{{- if .Values.llm.googleApiKey }}
- name: GOOGLE_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "af-stack.secretName" . | quote }}
      key: GOOGLE_API_KEY
{{- end }}
{{- end }}
{{- end }}

{{/*
inlineSecret needed? True if the operator passed any inline secret values
that require a chart-managed Secret to be rendered.
*/}}
{{- define "af-stack.needsInlineSecret" -}}
{{- $needs := false -}}
{{- if and (eq (include "af-stack.databaseUrlSource" .) "inline") .Values.postgresql.external.url -}}{{- $needs = true -}}{{- end -}}
{{- if and (not .Values.secrets.kmsKeySecretRef.name) .Values.secrets.kmsKey -}}{{- $needs = true -}}{{- end -}}
{{- if and (not .Values.secrets.authSecretSecretRef.name) .Values.secrets.authSecret -}}{{- $needs = true -}}{{- end -}}
{{- if and (eq .Values.storage.mode "s3") (not .Values.storage.s3.credentialsSecretRef.name) (or .Values.storage.s3.accessKey .Values.storage.s3.secretKey) -}}{{- $needs = true -}}{{- end -}}
{{- if not .Values.llm.providerSecretRef.name -}}
{{- if or .Values.llm.openrouterApiKey .Values.llm.anthropicApiKey .Values.llm.openaiApiKey .Values.llm.googleApiKey -}}{{- $needs = true -}}{{- end -}}
{{- end -}}
{{- $needs -}}
{{- end }}
