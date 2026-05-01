{{- define "vacation-coverage.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "vacation-coverage.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "vacation-coverage.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "vacation-coverage.selectorLabels" -}}
app.kubernetes.io/name: vacation-coverage
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
