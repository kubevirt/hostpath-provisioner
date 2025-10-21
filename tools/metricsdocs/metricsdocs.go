package main

import (
	"fmt"

	"github.com/rhobs/operator-observability-toolkit/pkg/docs"

	"kubevirt.io/hostpath-provisioner/pkg/monitoring/metrics"
)

const tpl = `# Hostpath Provisioner Metrics

{{- range . }}

{{ $deprecatedVersion := "" -}}
{{- with index .ExtraFields "DeprecatedVersion" -}}
    {{- $deprecatedVersion = printf " in %s" . -}}
{{- end -}}

{{- $stabilityLevel := "" -}}
{{- if and (.ExtraFields.StabilityLevel) (ne .ExtraFields.StabilityLevel "STABLE") -}}
	{{- $stabilityLevel = printf "[%s%s] " .ExtraFields.StabilityLevel $deprecatedVersion -}}
{{- end -}}

### {{ .Name }}
{{ print $stabilityLevel }}{{ .Help }}. Type: {{ .Type -}}.

{{- end }}

## Developing new metrics

All metrics documented here are auto-generated and reflect exactly what is being
exposed. After developing new metrics or changing old ones please regenerate
this document.
`

func main() {
	err := metrics.SetupMetrics()
	if err != nil {
		panic(err)
	}

	metricsList := metrics.ListMetrics()

	docsString := docs.BuildMetricsDocsWithCustomTemplate(metricsList, nil, tpl)
	fmt.Print(docsString)
}
