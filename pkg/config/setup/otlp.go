// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021-present Datadog, Inc.

package setup

import (
	pkgconfigmodel "github.com/DataDog/datadog-agent/pkg/config/model"
)

// OTLP configuration paths.
const (
	OTLPSection               = "otlp_config"
	OTLPTracesSubSectionKey   = "traces"
	OTLPTracePort             = OTLPSection + "." + OTLPTracesSubSectionKey + ".internal_port"
	OTLPTracesEnabled         = OTLPSection + "." + OTLPTracesSubSectionKey + ".enabled"
	OTLPLogsSubSectionKey     = "logs"
	OTLPLogsEnabled           = OTLPSection + "." + OTLPLogsSubSectionKey + ".enabled"
	OTLPReceiverSubSectionKey = "receiver"
	OTLPReceiverSection       = OTLPSection + "." + OTLPReceiverSubSectionKey
	OTLPMetricsSubSectionKey  = "metrics"
	OTLPMetrics               = OTLPSection + "." + OTLPMetricsSubSectionKey
	OTLPMetricsEnabled        = OTLPSection + "." + OTLPMetricsSubSectionKey + ".enabled"
	OTLPTagCardinalityKey     = OTLPMetrics + ".tag_cardinality"
	OTLPDebugKey              = "debug"
	OTLPDebug                 = OTLPSection + "." + OTLPDebugKey
)

// OTLP related configuration.
func OTLP(config pkgconfigmodel.Setup) {
	config.BindEnvAndSetDefault(OTLPTracePort, 5003)
	config.BindEnvAndSetDefault(OTLPMetricsEnabled, true)
	config.BindEnvAndSetDefault(OTLPTracesEnabled, true)
	config.BindEnvAndSetDefault(OTLPLogsEnabled, false)

	// NOTE: This only partially works.
	// The environment variable is also manually checked in comp/otelcol/otlp/config.go
	config.BindEnvAndSetDefault(OTLPTagCardinalityKey, "low", "DD_OTLP_TAG_CARDINALITY")

	config.SetKnown(OTLPMetrics)
	// Set all subkeys of otlp_config.metrics as known
	config.SetKnown(OTLPMetrics + ".*")
	config.SetKnown(OTLPReceiverSection)
	// Set all subkeys of otlp_config.receiver as known
	config.SetKnown(OTLPReceiverSection + ".*")
	config.SetKnown(OTLPDebug)
	// Set all subkeys of otlp_config.debug as known
	config.SetKnown(OTLPDebug + ".*")

	// set environment variables for selected fields
	setupOTLPEnvironmentVariables(config)
}

// setupOTLPEnvironmentVariables sets up the environment variables associated with different OTLP ingest settings:
// If there are changes in the OTLP receiver configuration, they should be reflected here.
//
// We don't need to set the default value: it is dealt with at the unmarshaling level
// since we get the configuration through GetStringMap
//
// We are missing TLS settings: since some of them need more work to work right they are not included here.
func setupOTLPEnvironmentVariables(config pkgconfigmodel.Setup) {
	// gRPC settings
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.endpoint")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.transport")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.max_recv_msg_size_mib")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.max_concurrent_streams")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.read_buffer_size")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.write_buffer_size")
	config.BindEnv(OTLPSection + ".receiver.protocols.grpc.include_metadata")

	// Traces settings
	config.BindEnvAndSetDefault("otlp_config.traces.span_name_remappings", map[string]string{})
	config.BindEnv("otlp_config.traces.span_name_as_resource_name")
	config.BindEnvAndSetDefault("otlp_config.traces.ignore_missing_datadog_fields", false, "DD_OTLP_CONFIG_IGNORE_MISSING_DATADOG_FIELDS")
	config.BindEnvAndSetDefault("otlp_config.traces.probabilistic_sampler.sampling_percentage", 100.,
		"DD_OTLP_CONFIG_TRACES_PROBABILISTIC_SAMPLER_SAMPLING_PERCENTAGE")

	// HTTP settings
	config.BindEnv(OTLPSection + ".receiver.protocols.http.endpoint")
	config.BindEnv(OTLPSection + ".receiver.protocols.http.max_request_body_size")
	config.BindEnv(OTLPSection + ".receiver.protocols.http.include_metadata")

	// Metrics settings
	config.BindEnv(OTLPSection + ".metrics.delta_ttl")
	config.BindEnv(OTLPSection + ".metrics.resource_attributes_as_tags")
	config.BindEnv(OTLPSection + ".metrics.instrumentation_scope_metadata_as_tags")
	config.BindEnv(OTLPSection + ".metrics.tag_cardinality")
	config.BindEnv(OTLPSection + ".metrics.histograms.mode")
	config.BindEnv(OTLPSection + ".metrics.histograms.send_count_sum_metrics")
	config.BindEnv(OTLPSection + ".metrics.histograms.send_aggregation_metrics")
	config.BindEnv(OTLPSection + ".metrics.sums.cumulative_monotonic_mode")
	config.BindEnv(OTLPSection + ".metrics.sums.initial_cumulative_monotonic_value")
	config.BindEnv(OTLPSection + ".metrics.summaries.mode")

	// Debug settings
	config.BindEnv(OTLPSection + ".debug.verbosity")
}
