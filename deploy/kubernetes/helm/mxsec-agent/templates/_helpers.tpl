{{/*
Common labels.
*/}}
{{- define "mxsec-agent.labels" -}}
app.kubernetes.io/name: mxsec-agent
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "mxsec-agent.selectorLabels" -}}
app.kubernetes.io/name: mxsec-agent
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Service account name.
*/}}
{{- define "mxsec-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- printf "%s-agent" .Release.Name }}
{{- end }}
{{- end }}
