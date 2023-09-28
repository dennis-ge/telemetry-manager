package builder

import (
	"fmt"
	"sort"
	"strings"

	telemetryv1alpha1 "github.com/kyma-project/telemetry-manager/apis/telemetry/v1alpha1"
	"github.com/kyma-project/telemetry-manager/internal/utils/envvar"
)

// Considering Fluent Bit's exponential back-off and jitter algorithm with the default scheduler.base and scheduler.cap,
// this retry limit should be enough to cover about 3 days of retrying. See
// https://docs.fluentbit.io/manual/administration/scheduling-and-retries. We do not want unlimited retries to avoid
// that malformed logs stay in the buffer forever.
var retryLimit = "300"

func createOutputSection(pipeline *telemetryv1alpha1.LogPipeline, defaults PipelineDefaults) string {
	output := &pipeline.Spec.Output
	if output.IsCustomDefined() {
		return generateCustomOutput(output, defaults.FsBufferLimit, pipeline.Name)
	}

	if output.IsHTTPDefined() {
		return generateHTTPOutput(output.HTTP, defaults.FsBufferLimit, pipeline.Name)
	}

	if output.IsLokiDefined() {
		return generateLokiOutput(output.Loki, defaults.FsBufferLimit, pipeline.Name)
	}

	return ""
}

func generateCustomOutput(output *telemetryv1alpha1.Output, fsBufferLimit string, name string) string {
	sb := NewOutputSectionBuilder()
	customOutputParams := parseMultiline(output.Custom)
	var outputName string
	if customOutputParams.GetByKey("name") != nil {
		outputName = customOutputParams.GetByKey("name").Value
	}
	aliasPresent := customOutputParams.ContainsKey("alias")
	for _, p := range customOutputParams {
		sb.AddConfigParam(p.Key, p.Value)
	}
	if !aliasPresent {
		sb.AddConfigParam("alias", fmt.Sprintf("%s-%s", name, outputName))
	}
	sb.AddConfigParam("match", fmt.Sprintf("%s.*", name))
	sb.AddConfigParam("storage.total_limit_size", fsBufferLimit)
	sb.AddConfigParam("retry_limit", retryLimit)
	return sb.Build()
}

func generateHTTPOutput(httpOutput *telemetryv1alpha1.HTTPOutput, fsBufferLimit string, name string) string {
	sb := NewOutputSectionBuilder()
	sb.AddConfigParam("name", "http")
	sb.AddConfigParam("allow_duplicated_headers", "true")
	sb.AddConfigParam("match", fmt.Sprintf("%s.*", name))
	sb.AddConfigParam("alias", fmt.Sprintf("%s-http", name))
	sb.AddConfigParam("storage.total_limit_size", fsBufferLimit)
	sb.AddConfigParam("retry_limit", retryLimit)
	sb.AddIfNotEmpty("uri", httpOutput.URI)
	sb.AddIfNotEmpty("compress", httpOutput.Compress)
	sb.AddIfNotEmptyOrDefault("port", httpOutput.Port, "443")
	sb.AddIfNotEmptyOrDefault("format", httpOutput.Format, "json")

	if httpOutput.Host.IsDefined() {
		value := resolveValue(httpOutput.Host, name)
		sb.AddConfigParam("host", value)
	}
	if httpOutput.Password.IsDefined() {
		value := resolveValue(httpOutput.Password, name)
		sb.AddConfigParam("http_passwd", value)
	}
	if httpOutput.User.IsDefined() {
		value := resolveValue(httpOutput.User, name)
		sb.AddConfigParam("http_user", value)
	}
	tlsEnabled := "on"
	if httpOutput.TLSConfig.Disabled {
		tlsEnabled = "off"
	}
	sb.AddConfigParam("tls", tlsEnabled)
	tlsVerify := "on"
	if httpOutput.TLSConfig.SkipCertificateValidation {
		tlsVerify = "off"
	}
	sb.AddConfigParam("tls.verify", tlsVerify)
	if httpOutput.TLSConfig.CA.IsDefined() {
		sb.AddConfigParam("tls.ca_file", fmt.Sprintf("/fluent-bit/tls/%s-ca.crt", name))
	}
	if httpOutput.TLSConfig.Cert.IsDefined() {
		sb.AddConfigParam("tls.crt_file", fmt.Sprintf("/fluent-bit/tls/%s-cert.crt", name))
	}
	if httpOutput.TLSConfig.Key.IsDefined() {
		sb.AddConfigParam("tls.key_file", fmt.Sprintf("/fluent-bit/tls/%s-key.key", name))
	}

	return sb.Build()
}

func generateLokiOutput(lokiOutput *telemetryv1alpha1.LokiOutput, fsBufferLimit string, name string) string {
	sb := NewOutputSectionBuilder()
	sb.AddConfigParam("labelMapPath", "/fluent-bit/etc/loki-labelmap.json")
	sb.AddConfigParam("loglevel", "warn")
	sb.AddConfigParam("lineformat", "json")
	sb.AddConfigParam("match", fmt.Sprintf("%s.*", name))
	sb.AddConfigParam("storage.total_limit_size", fsBufferLimit)
	sb.AddConfigParam("name", "grafana-loki")
	sb.AddConfigParam("alias", fmt.Sprintf("%s-grafana-loki", name))
	sb.AddConfigParam("url", resolveValue(lokiOutput.URL, name))
	if len(lokiOutput.Labels) != 0 {
		value := concatenateLabels(lokiOutput.Labels)
		sb.AddConfigParam("labels", value)
	}
	if len(lokiOutput.RemoveKeys) != 0 {
		str := strings.Join(lokiOutput.RemoveKeys, ", ")
		sb.AddConfigParam("removeKeys", str)
	}
	return sb.Build()
}

func concatenateLabels(labels map[string]string) string {
	var labelsSlice []string
	for k, v := range labels {
		labelsSlice = append(labelsSlice, fmt.Sprintf("%s=\"%s\"", k, v))
	}
	sort.Strings(labelsSlice)
	return fmt.Sprintf("{%s}", strings.Join(labelsSlice, ", "))
}

func resolveValue(value telemetryv1alpha1.ValueType, logPipeline string) string {
	if value.Value != "" {
		return value.Value
	}
	if value.ValueFrom != nil && value.ValueFrom.IsSecretKeyRef() {
		secretKeyRef := value.ValueFrom.SecretKeyRef
		return fmt.Sprintf("${%s}", envvar.FormatEnvVarName(logPipeline, secretKeyRef.Namespace, secretKeyRef.Name, secretKeyRef.Key))
	}
	return ""
}
