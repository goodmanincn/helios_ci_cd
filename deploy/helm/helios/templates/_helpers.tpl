{{/*
Helios common labels
*/}}
{{- define "helios.labels" -}}
app.kubernetes.io/name: {{ include "helios.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Helios name
*/}}
{{- define "helios.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Helios fullname
*/}}
{{- define "helios.fullname" -}}
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
{{- end -}}

{{/*
Helios chart name
*/}}
{{- define "helios.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Helios service account name
*/}}
{{- define "helios.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "helios.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end -}}

{{/*
Helios selector labels
*/}}
{{- define "helios.selectorLabels" -}}
app.kubernetes.io/name: {{ include "helios.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
