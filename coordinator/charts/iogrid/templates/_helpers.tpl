{{/*
Common helpers for the iogrid coordinator chart. Each microservice is
templated by iterating .Values.services in the per-resource templates;
this file centralises naming, labels, and per-service value merging.
*/}}

{{/* Fully qualified service name: <release>-<service> */}}
{{- define "iogrid.fullname" -}}
{{- printf "%s-%s" .Release.Name .svcName | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Image reference: registry/repo:tag */}}
{{- define "iogrid.image" -}}
{{- $svc := .svcCfg -}}
{{- $registry := .Values.imageRegistry -}}
{{- printf "%s/%s:%s" $registry $svc.image.repository $svc.image.tag -}}
{{- end -}}

{{/* Standard labels applied to every resource */}}
{{- define "iogrid.labels" -}}
app.kubernetes.io/name: {{ .svcName }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: coordinator
app.kubernetes.io/part-of: iogrid
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Selector labels (subset of labels — must be stable) */}}
{{- define "iogrid.selectorLabels" -}}
app.kubernetes.io/name: {{ .svcName }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Merge .Values.defaults with the per-service block. Returns a dict whose keys
follow .Values.defaults but with per-service overrides applied. Use as:
  {{- $cfg := include "iogrid.svcConfig" (dict "Values" .Values "svcName" $name "svcCfg" $svc) | fromYaml }}
*/}}
{{- define "iogrid.svcConfig" -}}
{{- $defaults := .Values.defaults -}}
{{- $svc := .svcCfg -}}
{{- $merged := merge (deepCopy $svc) $defaults -}}
{{- toYaml $merged -}}
{{- end -}}
