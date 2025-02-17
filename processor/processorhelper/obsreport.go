// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package processorhelper // import "go.opentelemetry.io/collector/processor/processorhelper"

import (
	"context"
	"errors"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/internal/obsreportconfig"
	"go.opentelemetry.io/collector/internal/obsreportconfig/obsmetrics"
	"go.opentelemetry.io/collector/processor"
)

var (
	processorName  = "processor"
	scopeName      = "go.opentelemetry.io/collector/obsreport"
	nameSep        = "/"
	processorScope = scopeName + nameSep + processorName
)

// BuildCustomMetricName is used to be build a metric name following
// the standards used in the Collector. The configType should be the same
// value used to identify the type on the config.
func BuildCustomMetricName(configType, metric string) string {
	componentPrefix := obsmetrics.ProcessorPrefix
	if !strings.HasSuffix(componentPrefix, obsmetrics.NameSep) {
		componentPrefix += obsmetrics.NameSep
	}
	if configType == "" {
		return componentPrefix
	}
	return componentPrefix + configType + obsmetrics.NameSep + metric
}

// ObsReport is a helper to add observability to a processor.
type ObsReport struct {
	level    configtelemetry.Level
	mutators []tag.Mutator

	logger *zap.Logger

	useOtelForMetrics bool
	otelAttrs         []attribute.KeyValue

	acceptedSpansCounter        metric.Int64Counter
	refusedSpansCounter         metric.Int64Counter
	droppedSpansCounter         metric.Int64Counter
	acceptedMetricPointsCounter metric.Int64Counter
	refusedMetricPointsCounter  metric.Int64Counter
	droppedMetricPointsCounter  metric.Int64Counter
	acceptedLogRecordsCounter   metric.Int64Counter
	refusedLogRecordsCounter    metric.Int64Counter
	droppedLogRecordsCounter    metric.Int64Counter
}

// ObsReportSettings are settings for creating an ObsReport.
type ObsReportSettings struct {
	ProcessorID             component.ID
	ProcessorCreateSettings processor.CreateSettings
}

// NewObsReport creates a new Processor.
func NewObsReport(cfg ObsReportSettings) (*ObsReport, error) {
	return newObsReport(cfg, obsreportconfig.UseOtelForInternalMetricsfeatureGate.IsEnabled())
}

func newObsReport(cfg ObsReportSettings, useOtel bool) (*ObsReport, error) {
	report := &ObsReport{
		level:             cfg.ProcessorCreateSettings.MetricsLevel,
		mutators:          []tag.Mutator{tag.Upsert(obsmetrics.TagKeyProcessor, cfg.ProcessorID.String(), tag.WithTTL(tag.TTLNoPropagation))},
		logger:            cfg.ProcessorCreateSettings.Logger,
		useOtelForMetrics: useOtel,
		otelAttrs: []attribute.KeyValue{
			attribute.String(obsmetrics.ProcessorKey, cfg.ProcessorID.String()),
		},
	}

	// ignore instrument name error as per workaround in https://github.com/open-telemetry/opentelemetry-collector/issues/8346
	// if err := proc.createOtelMetrics(cfg); err != nil {
	// 	return nil, err
	// }
	if err := report.createOtelMetrics(cfg); err != nil && !errors.Is(err, sdkmetric.ErrInstrumentName) {
		return nil, err
	}

	return report, nil
}

func (or *ObsReport) createOtelMetrics(cfg ObsReportSettings) error {
	if !or.useOtelForMetrics {
		return nil
	}
	meter := cfg.ProcessorCreateSettings.MeterProvider.Meter(processorScope)
	var errors, err error

	or.acceptedSpansCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.AcceptedSpansKey,
		metric.WithDescription("Number of spans successfully pushed into the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.refusedSpansCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.RefusedSpansKey,
		metric.WithDescription("Number of spans that were rejected by the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.droppedSpansCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.DroppedSpansKey,
		metric.WithDescription("Number of spans that were dropped."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.acceptedMetricPointsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.AcceptedMetricPointsKey,
		metric.WithDescription("Number of metric points successfully pushed into the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.refusedMetricPointsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.RefusedMetricPointsKey,
		metric.WithDescription("Number of metric points that were rejected by the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.droppedMetricPointsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.DroppedMetricPointsKey,
		metric.WithDescription("Number of metric points that were dropped."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.acceptedLogRecordsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.AcceptedLogRecordsKey,
		metric.WithDescription("Number of log records successfully pushed into the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.refusedLogRecordsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.RefusedLogRecordsKey,
		metric.WithDescription("Number of log records that were rejected by the next component in the pipeline."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	or.droppedLogRecordsCounter, err = meter.Int64Counter(
		obsmetrics.ProcessorPrefix+obsmetrics.DroppedLogRecordsKey,
		metric.WithDescription("Number of log records that were dropped."),
		metric.WithUnit("1"),
	)
	errors = multierr.Append(errors, err)

	return errors
}

func (or *ObsReport) recordWithOtel(ctx context.Context, dataType component.DataType, accepted, refused, dropped int64) {
	var acceptedCount, refusedCount, droppedCount metric.Int64Counter
	switch dataType {
	case component.DataTypeTraces:
		acceptedCount = or.acceptedSpansCounter
		refusedCount = or.refusedSpansCounter
		droppedCount = or.droppedSpansCounter
	case component.DataTypeMetrics:
		acceptedCount = or.acceptedMetricPointsCounter
		refusedCount = or.refusedMetricPointsCounter
		droppedCount = or.droppedMetricPointsCounter
	case component.DataTypeLogs:
		acceptedCount = or.acceptedLogRecordsCounter
		refusedCount = or.refusedLogRecordsCounter
		droppedCount = or.droppedLogRecordsCounter
	}

	acceptedCount.Add(ctx, accepted, metric.WithAttributes(or.otelAttrs...))
	refusedCount.Add(ctx, refused, metric.WithAttributes(or.otelAttrs...))
	droppedCount.Add(ctx, dropped, metric.WithAttributes(or.otelAttrs...))
}

func (or *ObsReport) recordWithOC(ctx context.Context, dataType component.DataType, accepted, refused, dropped int64) {
	var acceptedMeasure, refusedMeasure, droppedMeasure *stats.Int64Measure

	switch dataType {
	case component.DataTypeTraces:
		acceptedMeasure = obsmetrics.ProcessorAcceptedSpans
		refusedMeasure = obsmetrics.ProcessorRefusedSpans
		droppedMeasure = obsmetrics.ProcessorDroppedSpans
	case component.DataTypeMetrics:
		acceptedMeasure = obsmetrics.ProcessorAcceptedMetricPoints
		refusedMeasure = obsmetrics.ProcessorRefusedMetricPoints
		droppedMeasure = obsmetrics.ProcessorDroppedMetricPoints
	case component.DataTypeLogs:
		acceptedMeasure = obsmetrics.ProcessorAcceptedLogRecords
		refusedMeasure = obsmetrics.ProcessorRefusedLogRecords
		droppedMeasure = obsmetrics.ProcessorDroppedLogRecords
	}

	// ignore the error for now; should not happen
	_ = stats.RecordWithTags(
		ctx,
		or.mutators,
		acceptedMeasure.M(accepted),
		refusedMeasure.M(refused),
		droppedMeasure.M(dropped),
	)
}

func (or *ObsReport) recordData(ctx context.Context, dataType component.DataType, accepted, refused, dropped int64) {
	if or.useOtelForMetrics {
		or.recordWithOtel(ctx, dataType, accepted, refused, dropped)
	} else {
		or.recordWithOC(ctx, dataType, accepted, refused, dropped)
	}
}

// TracesAccepted reports that the trace data was accepted.
func (or *ObsReport) TracesAccepted(ctx context.Context, numSpans int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeTraces, int64(numSpans), int64(0), int64(0))
	}
}

// TracesRefused reports that the trace data was refused.
func (or *ObsReport) TracesRefused(ctx context.Context, numSpans int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeTraces, int64(0), int64(numSpans), int64(0))
	}
}

// TracesDropped reports that the trace data was dropped.
func (or *ObsReport) TracesDropped(ctx context.Context, numSpans int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeTraces, int64(0), int64(0), int64(numSpans))
	}
}

// MetricsAccepted reports that the metrics were accepted.
func (or *ObsReport) MetricsAccepted(ctx context.Context, numPoints int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeMetrics, int64(numPoints), int64(0), int64(0))
	}
}

// MetricsRefused reports that the metrics were refused.
func (or *ObsReport) MetricsRefused(ctx context.Context, numPoints int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeMetrics, int64(0), int64(numPoints), int64(0))
	}
}

// MetricsDropped reports that the metrics were dropped.
func (or *ObsReport) MetricsDropped(ctx context.Context, numPoints int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeMetrics, int64(0), int64(0), int64(numPoints))
	}
}

// LogsAccepted reports that the logs were accepted.
func (or *ObsReport) LogsAccepted(ctx context.Context, numRecords int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeLogs, int64(numRecords), int64(0), int64(0))
	}
}

// LogsRefused reports that the logs were refused.
func (or *ObsReport) LogsRefused(ctx context.Context, numRecords int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeLogs, int64(0), int64(numRecords), int64(0))
	}
}

// LogsDropped reports that the logs were dropped.
func (or *ObsReport) LogsDropped(ctx context.Context, numRecords int) {
	if or.level != configtelemetry.LevelNone {
		or.recordData(ctx, component.DataTypeLogs, int64(0), int64(0), int64(numRecords))
	}
}
