// This file and its contents are licensed under the Apache License 2.0.
// Please see the included NOTICE for copyright information and
// LICENSE for a copy of the license.

package query

import (
	"math"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/timescale/promscale/pkg/promql"
)

func NewEngine(logger log.Logger, queryTimeout time.Duration, subqueryDefaultStepInterval time.Duration) *promql.Engine {
	return promql.NewEngine(
		promql.EngineOpts{
			Logger:                   logger,
			Reg:                      prometheus.NewRegistry(),
			MaxSamples:               math.MaxInt32,
			Timeout:                  queryTimeout,
			NoStepSubqueryIntervalFn: func(int64) int64 { return durationMilliseconds(subqueryDefaultStepInterval) },
		},
	)
}

func durationMilliseconds(d time.Duration) int64 {
	return int64(d / (time.Millisecond / time.Nanosecond))
}
