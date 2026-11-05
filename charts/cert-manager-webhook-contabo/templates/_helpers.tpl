{{/* Chart name, limited to 63 characters. */}}
{{- define "cmwc.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Full name of the release objects. Kept to 52 characters so that the longest
suffix ("-webhook-tls") still fits in 63.
*/}}
{{- define "cmwc.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 52 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 52 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 52 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "cmwc.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cmwc.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cmwc.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cmwc.labels" -}}
helm.sh/chart: {{ include "cmwc.chart" . }}
{{ include "cmwc.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: dns01-webhook
{{- end -}}

{{- define "cmwc.selfSignedIssuer" -}}{{ include "cmwc.fullname" . }}-selfsign{{- end -}}
{{- define "cmwc.rootCAIssuer" -}}{{ include "cmwc.fullname" . }}-ca{{- end -}}
{{- define "cmwc.rootCACertificate" -}}{{ include "cmwc.fullname" . }}-ca{{- end -}}
{{- define "cmwc.servingCertificate" -}}{{ include "cmwc.fullname" . }}-webhook-tls{{- end -}}

{{- define "cmwc.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ default .Chart.AppVersion .Values.image.tag }}
{{- end -}}
{{- end -}}

{{/* Fails early with a clear message when values cannot work. */}}
{{- define "cmwc.validate" -}}
{{- if not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$" .Values.groupName) -}}
{{- fail (printf "groupName %q must be a lower-case DNS-style name with at least one dot, for example contabo.acme.codejourney.io" .Values.groupName) -}}
{{- end -}}
{{- if and .Values.podDisruptionBudget.enabled (le (int .Values.replicaCount) (int .Values.podDisruptionBudget.minAvailable)) -}}
{{- fail "podDisruptionBudget.minAvailable must be lower than replicaCount, or node drains would hang forever" -}}
{{- end -}}
{{- end -}}
