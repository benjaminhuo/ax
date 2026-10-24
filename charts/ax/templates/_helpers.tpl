{{- define "ax.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ax.fullname" -}}
{{- $fullname := .Values.fullnameOverride -}}
{{- if not $fullname -}}
{{- $name := include "ax.name" . -}}
{{- if contains $name .Release.Name -}}
{{- $fullname = .Release.Name -}}
{{- else -}}
{{- $fullname = printf "%s-%s" .Release.Name $name -}}
{{- end -}}
{{- end -}}
{{- if gt (len $fullname) 47 -}}
{{- printf "%s-%s" ($fullname | trunc 38 | trimSuffix "-") (sha256sum $fullname | trunc 8) -}}
{{- else -}}
{{- $fullname | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "ax.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/name: {{ include "ax.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "ax.serverSelectorLabels" -}}
app.kubernetes.io/name: {{ include "ax.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
{{- end -}}

{{- define "ax.redisSelectorLabels" -}}
app.kubernetes.io/name: {{ include "ax.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: redis
{{- end -}}

{{- define "ax.serviceAccountName" -}}
{{- default (printf "%s-server" (include "ax.fullname" .)) .Values.server.serviceAccount.name -}}
{{- end -}}

{{- define "ax.clusterRoleName" -}}
{{- $identity := printf "%s/%s" .Release.Namespace .Release.Name -}}
{{- printf "%s-server-%s" (include "ax.fullname" .) (sha256sum $identity | trunc 8) -}}
{{- end -}}

{{- define "ax.apiPolicyName" -}}
{{- printf "%s-api-%s" (include "ax.fullname" .) (sha256sum .Release.Namespace | trunc 8) -}}
{{- end -}}

{{- define "ax.image" -}}
{{- $repository := required "image.repository is required" .repository -}}
{{- if .digest -}}
{{- printf "%s:%s@%s" $repository .tag .digest -}}
{{- else -}}
{{- printf "%s:%s" $repository .tag -}}
{{- end -}}
{{- end -}}

{{- define "ax.runnerImage" -}}
{{- $_ := required "Set taskRunner.image.digest to the published multi-platform index digest (sha256:...); the kagent Substrate fork requires pinned runner images" .Values.taskRunner.image.digest -}}
{{- include "ax.image" .Values.taskRunner.image -}}
{{- end -}}

{{- define "ax.substrateAuthority" -}}
{{- default (printf "%s.%s.svc" .Values.substrate.apiService .Values.substrate.namespace) .Values.substrate.authority -}}
{{- end -}}

{{- define "ax.substrateEndpoint" -}}
{{- default (printf "dns:///%s.%s.svc:%v" .Values.substrate.apiService .Values.substrate.namespace .Values.substrate.apiPort) .Values.substrate.endpoint -}}
{{- end -}}

{{- define "ax.substrateAudience" -}}
{{- default (include "ax.substrateAuthority" .) .Values.substrate.auth.audience -}}
{{- end -}}

{{- define "ax.routerAddress" -}}
{{- default (printf "%s.%s.svc:%v" .Values.substrate.router.service .Values.substrate.namespace .Values.substrate.router.port) .Values.substrate.router.address -}}
{{- end -}}

{{- define "ax.redisAddress" -}}
{{- if .Values.redis.enabled -}}
{{- printf "%s-redis.%s.svc:6379" (include "ax.fullname" .) .Release.Namespace -}}
{{- else -}}
{{- required "Set redis.external.address when redis.enabled=false" .Values.redis.external.address -}}
{{- end -}}
{{- end -}}
