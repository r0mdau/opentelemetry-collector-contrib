// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package fluentforwardexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fluentforwardexporter"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/plog"
)

type fluentforwardExporter struct {
	config            *Config
	logsMarshaler     plog.Marshaler
	settings          component.TelemetrySettings
	endpoint          string
	generatorURL      string
	defaultSeverity   string
	severityAttribute string
}

func (s *fluentforwardExporter) pushLogs(_ context.Context, _ plog.Logs) error {
	// To implement
	return nil
}

func (s *fluentforwardExporter) start(_ context.Context, _ component.Host) error {
	// To implement
	return nil
}

func (s *fluentforwardExporter) shutdown(context.Context) error {
	// To implement
	return nil
}

func newFluentForwardExporter(cfg *Config, set component.TelemetrySettings) *fluentforwardExporter {

	return &fluentforwardExporter{
		config:            cfg,
		settings:          set,
		logsMarshaler:     &plog.JSONMarshaler{},
		endpoint:          cfg.HTTPClientSettings.Endpoint,
		generatorURL:      cfg.GeneratorURL,
		defaultSeverity:   cfg.DefaultSeverity,
		severityAttribute: cfg.SeverityAttribute,
	}
}

func newLogsExporter(ctx context.Context, cfg component.Config, set exporter.CreateSettings) (exporter.Logs, error) {
	config := cfg.(*Config)

	s := newFluentForwardExporter(config, set.TelemetrySettings)

	return exporterhelper.NewLogsExporter(
		ctx,
		set,
		cfg,
		s.pushLogs,
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		// Disable Timeout/RetryOnFailure and SendingQueue
		exporterhelper.WithStart(s.start),
		exporterhelper.WithTimeout(config.TimeoutSettings),
		exporterhelper.WithRetry(config.RetrySettings),
		exporterhelper.WithQueue(config.QueueSettings),
		exporterhelper.WithShutdown(s.shutdown),
	)
}
