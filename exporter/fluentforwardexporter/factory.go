// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package fluentforwardexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fluentforwardexporter"

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fluentforwardexporter/internal/metadata"
)

// NewFactory creates a factory for OTLP exporter.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		metadata.Type,
		createDefaultConfig,
		exporter.WithLogs(createLogsExporter, metadata.LogsStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		GeneratorURL:    "opentelemetry-collector",
		DefaultSeverity: "info",
		TimeoutSettings: exporterhelper.NewDefaultTimeoutSettings(),
		RetrySettings:   exporterhelper.NewDefaultRetrySettings(),
		QueueSettings:   exporterhelper.NewDefaultQueueSettings(),
		HTTPClientSettings: confighttp.HTTPClientSettings{
			Endpoint: "http://localhost:9093",
			Timeout:  30 * time.Second,
			Headers:  map[string]configopaque.String{},
			// We almost read 0 bytes, so no need to tune ReadBufferSize.
			WriteBufferSize: 512 * 1024,
		},
	}
}

func createLogsExporter(ctx context.Context, set exporter.CreateSettings, config component.Config) (exporter.Logs, error) {
	cfg := config.(*Config)

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("exporter config requires a non-empty \"endpoint\"")
	}
	return newLogsExporter(ctx, cfg, set)
}
