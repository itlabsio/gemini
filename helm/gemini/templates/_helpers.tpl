{{- define "gemini.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "gemini.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "gemini.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "gemini.labels" -}}
helm.sh/chart: {{ include "gemini.chart" . }}
{{ include "gemini.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
gemini.itlabs.io/head-role: {{ .Values.headRole | quote }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{- define "gemini.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gemini.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "gemini.backend.selectorLabels" -}}
{{ include "gemini.selectorLabels" . }}
app.kubernetes.io/component: backend
{{- end -}}

{{- define "gemini.frontend.selectorLabels" -}}
{{ include "gemini.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end -}}

{{- define "gemini.serviceAccountName" -}}
{{- .Values.rbac.serviceAccountName | default "gemini" -}}
{{- end -}}

{{- define "gemini.backend.image" -}}
{{- $tag := .Values.image.backend.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.backend.repository $tag -}}
{{- end -}}

{{- define "gemini.frontend.image" -}}
{{- $tag := .Values.image.frontend.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.frontend.repository $tag -}}
{{- end -}}

{{- define "gemini.job.image" -}}
{{- .Values.job.image | default (include "gemini.backend.image" .) -}}
{{- end -}}

{{/* Валидация headRole на этапе рендера */}}
{{- define "gemini.validateRole" -}}
{{- if not (or (eq .Values.headRole "source") (eq .Values.headRole "target")) -}}
{{- fail (printf "gemini: headRole must be 'source' or 'target', got %q" .Values.headRole) -}}
{{- end -}}
{{- end -}}

{{/*
Внешний URL головы, выведенный из ingress: https://host (tls) | http://host.
Пусто, если ingress выключен или host не задан.
*/}}
{{- define "gemini.externalUrl" -}}
{{- if and .Values.ingress.enabled .Values.ingress.host -}}
{{- printf "%s://%s" (ternary "https" "http" .Values.ingress.tls) .Values.ingress.host -}}
{{- end -}}
{{- end -}}

{{/*
PUBLIC_URL backend'а: явный values.publicUrl, иначе выводится из ingress
(gemini.externalUrl). Пусто, если ни то ни другое не задано.
*/}}
{{- define "gemini.publicUrl" -}}
{{- .Values.publicUrl | default (include "gemini.externalUrl" .) | trimSuffix "/" -}}
{{- end -}}

{{/*
JSON с nodeSelector / tolerations / affinity / resources для dump/restore-подов.
Бэкенд читает его из env JOB_POD_OVERRIDES и применяет к Job и CronJob.
*/}}
{{- define "gemini.job.podOverrides" -}}
{{- $o := dict -}}
{{- with .Values.job.nodeSelector }}{{- $_ := set $o "nodeSelector" . }}{{- end -}}
{{- with .Values.job.tolerations }}{{- $_ := set $o "tolerations" . }}{{- end -}}
{{- with .Values.job.affinity }}{{- $_ := set $o "affinity" . }}{{- end -}}
{{- with .Values.job.resources }}{{- $_ := set $o "resources" . }}{{- end -}}
{{- $o | toJson -}}
{{- end -}}
