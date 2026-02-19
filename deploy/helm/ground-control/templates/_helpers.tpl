{{/*
Chart name, truncated to 63 chars.
*/}}
{{- define "ground-control.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name.
*/}}
{{- define "ground-control.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label value.
*/}}
{{- define "ground-control.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "ground-control.labels" -}}
helm.sh/chart: {{ include "ground-control.chart" . }}
{{ include "ground-control.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "ground-control.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ground-control.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
PostgreSQL fullname.
*/}}
{{- define "ground-control.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "ground-control.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
PostgreSQL selector labels.
*/}}
{{- define "ground-control.postgresql.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ground-control.name" . }}-postgresql
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Database host: internal service name or external host.
*/}}
{{- define "ground-control.databaseHost" -}}
{{- if .Values.database.internal.enabled }}
{{- include "ground-control.postgresql.fullname" . }}
{{- else }}
{{- required "database.host is required when database.internal.enabled is false" .Values.database.host }}
{{- end }}
{{- end }}
