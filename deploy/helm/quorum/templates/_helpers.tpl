{{- define "quorum.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "quorum.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "quorum.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "quorum.selectorLabels" -}}
app.kubernetes.io/name: quorum
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
