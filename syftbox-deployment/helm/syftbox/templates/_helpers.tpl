{{/*
Expand the name of the chart.
*/}}
{{- define "syftbox.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "syftbox.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "syftbox.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "syftbox.labels" -}}
helm.sh/chart: {{ include "syftbox.chart" . }}
{{ include "syftbox.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "syftbox.selectorLabels" -}}
app.kubernetes.io/name: {{ include "syftbox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Private Database URL
*/}}
{{- define "syftbox.privateDatabaseUrl" -}}
postgresql://{{ .Values.database.private.username }}:{{ .Values.database.private.password }}@{{ .Values.database.private.host }}:{{ .Values.database.private.port }}/{{ .Values.database.private.database }}?sslmode={{ .Values.database.private.sslmode }}
{{- end }}

{{/*
Mock Database URL
*/}}
{{- define "syftbox.mockDatabaseUrl" -}}
postgresql://{{ .Values.database.mock.username }}:{{ .Values.database.mock.password }}@{{ .Values.database.mock.host }}:{{ .Values.database.mock.port }}/{{ .Values.database.mock.database }}?sslmode={{ .Values.database.mock.sslmode }}
{{- end }}

{{/*
Cache server URL - mirrors SYFTBOX_SERVER_URL from docker-compose-client.yml
*/}}
{{- define "syftbox.cacheServerUrl" -}}
http://{{ include "syftbox.fullname" . }}-cache-server:{{ .Values.cacheServer.service.port }}
{{- end }}

{{/*
MinIO endpoint URL for blob storage
*/}}
{{- define "syftbox.minioEndpoint" -}}
http://{{ include "syftbox.fullname" . }}-minio:9000
{{- end }}