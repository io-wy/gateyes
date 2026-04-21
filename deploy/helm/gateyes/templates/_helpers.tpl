{{- define "gateyes.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "gateyes.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "gateyes.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "gateyes.secretName" -}}
{{- if .Values.secrets.name -}}
{{- .Values.secrets.name -}}
{{- else -}}
{{- printf "%s-secrets" (include "gateyes.fullname" .) -}}
{{- end -}}
{{- end -}}
