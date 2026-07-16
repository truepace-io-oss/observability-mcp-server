{{- define "observability-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "observability-mcp.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s" (include "observability-mcp.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "observability-mcp.labels" -}}
app.kubernetes.io/name: {{ include "observability-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "observability-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "observability-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "observability-mcp.serviceAccountName" -}}
{{ include "observability-mcp.fullname" . }}
{{- end -}}

{{/* Secret name that holds one datasource's credentials (password / token / ca.crt). */}}
{{- define "observability-mcp.datasourceSecretName" -}}
{{- printf "%s-ds-%s" (include "observability-mcp.fullname" .root) .datasource.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
