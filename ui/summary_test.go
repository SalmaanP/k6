/*
 *
 * k6 - a next-generation load testing tool
 * Copyright (C) 2018 Load Impact
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package ui

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/loadimpact/k6/stats"
	"github.com/stretchr/testify/assert"
	"gopkg.in/guregu/null.v3"
)

var verifyTests = []struct {
	in  string
	out error
}{
	{"avg", nil},
	{"min", nil},
	{"med", nil},
	{"max", nil},
	{"p(0)", nil},
	{"p(90)", nil},
	{"p(95)", nil},
	{"p(99)", nil},
	{"p(99.9)", nil},
	{"p(99.9999)", nil},
	{"count", nil},
	{"nil", ErrStatUnknownFormat},
	{" avg", ErrStatUnknownFormat},
	{"avg ", ErrStatUnknownFormat},
	{"", ErrStatEmptyString},
}

var defaultTrendColumns = TrendColumns

func createTestTrendSink(count int) *stats.TrendSink {
	sink := stats.TrendSink{}

	for i := 0; i < count; i++ {
		sink.Add(stats.Sample{Value: float64(i)})
	}

	return &sink
}

func TestVerifyTrendColumnStat(t *testing.T) {
	for _, testCase := range verifyTests {
		err := VerifyTrendColumnStat(testCase.in)
		assert.Equal(t, testCase.out, err)
	}
}

func TestUpdateTrendColumns(t *testing.T) {
	tcOld := TrendColumns
	defer func() {
		TrendColumns = tcOld
	}()
	sink := createTestTrendSink(100)

	t.Run("No stats", func(t *testing.T) {
		TrendColumns = defaultTrendColumns

		UpdateTrendColumns(make([]string, 0))

		assert.Equal(t, defaultTrendColumns, TrendColumns)
	})

	t.Run("One stat", func(t *testing.T) {
		TrendColumns = defaultTrendColumns

		UpdateTrendColumns([]string{"avg"})

		assert.Exactly(t, 1, len(TrendColumns))
		assert.Exactly(t,
			sink.Avg,
			TrendColumns[0].Get(sink))
	})

	t.Run("Multiple stats", func(t *testing.T) {
		TrendColumns = defaultTrendColumns

		UpdateTrendColumns([]string{"med", "max", "count"})

		assert.Exactly(t, 3, len(TrendColumns))
		assert.Exactly(t, sink.Med, TrendColumns[0].Get(sink))
		assert.Exactly(t, sink.Max, TrendColumns[1].Get(sink))
		assert.Exactly(t, float64(100), TrendColumns[2].Get(sink))
	})

	t.Run("Ignore invalid stats", func(t *testing.T) {
		TrendColumns = defaultTrendColumns

		UpdateTrendColumns([]string{"med", "max", "invalid"})

		assert.Exactly(t, 2, len(TrendColumns))
		assert.Exactly(t, sink.Med, TrendColumns[0].Get(sink))
		assert.Exactly(t, sink.Max, TrendColumns[1].Get(sink))
	})

	t.Run("Percentile stats", func(t *testing.T) {
		TrendColumns = defaultTrendColumns

		UpdateTrendColumns([]string{"p(99.9999)"})

		assert.Exactly(t, 1, len(TrendColumns))
		assert.Exactly(t, sink.P(0.999999), TrendColumns[0].Get(sink))
	})
}

func TestGeneratePercentileTrendColumn(t *testing.T) {
	sink := createTestTrendSink(100)

	t.Run("Happy path", func(t *testing.T) {
		colFunc, err := generatePercentileTrendColumn("p(99)")

		assert.NotNil(t, colFunc)
		assert.Exactly(t, sink.P(0.99), colFunc(sink))
		assert.NotEqual(t, sink.P(0.98), colFunc(sink))
		assert.Nil(t, err)
	})

	t.Run("Empty stat", func(t *testing.T) {
		colFunc, err := generatePercentileTrendColumn("")

		assert.Nil(t, colFunc)
		assert.Exactly(t, err, ErrStatEmptyString)
	})

	t.Run("Invalid format", func(t *testing.T) {
		colFunc, err := generatePercentileTrendColumn("p90")

		assert.Nil(t, colFunc)
		assert.Exactly(t, err, ErrStatUnknownFormat)
	})

	t.Run("Invalid format 2", func(t *testing.T) {
		colFunc, err := generatePercentileTrendColumn("p(90")

		assert.Nil(t, colFunc)
		assert.Exactly(t, err, ErrStatUnknownFormat)
	})

	t.Run("Invalid float", func(t *testing.T) {
		colFunc, err := generatePercentileTrendColumn("p(a)")

		assert.Nil(t, colFunc)
		assert.Exactly(t, err, ErrPercentileStatInvalidValue)
	})
}

func createTestMetrics() map[string]*stats.Metric {
	metrics := make(map[string]*stats.Metric)
	gaugeMetric := stats.New("vus", stats.Gauge)
	gaugeMetric.Sink.Add(stats.Sample{Value: 1})
	countMetric := stats.New("http_reqs", stats.Counter)
	countMetric.Tainted = null.BoolFrom(true)
	checksMetric := stats.New("checks", stats.Rate)
	checksMetric.Tainted = null.BoolFrom(false)
	sink := &stats.TrendSink{}

	samples := []float64{10.0, 15.0, 20.0}
	for _, s := range samples {
		sink.Add(stats.Sample{Value: s})
		checksMetric.Sink.Add(stats.Sample{Value: 1})
		countMetric.Sink.Add(stats.Sample{Value: 1})
	}

	metrics["vus"] = gaugeMetric
	metrics["http_reqs"] = countMetric
	metrics["checks"] = checksMetric
	metrics["my_trend"] = &stats.Metric{Name: "my_trend", Type: stats.Trend, Contains: stats.Time, Sink: sink}

	return metrics
}

func TestSummarizeMetrics(t *testing.T) {
	tcOld := TrendColumns
	defer func() {
		TrendColumns = tcOld
	}()

	trendCountColumn := TrendColumn{"count", func(s *stats.TrendSink) float64 { return float64(s.Count) }}

	var (
		checksOut = " ✓ checks......: 100.00% ✓ 3   ✗ 0  \n"
		countOut  = " ✗ http_reqs...: 3       3/s\n"
		gaugeOut  = "   vus.........: 1       min=1 max=1\n"
		trendOut  = "   my_trend....: avg=15ms min=10ms med=15ms max=20ms p(90)=19ms p(95)=19.5ms\n"
	)

	metrics := createTestMetrics()
	testCases := []struct {
		columns  []TrendColumn
		expected string
	}{
		{tcOld, checksOut + countOut + trendOut + gaugeOut},
		{[]TrendColumn{trendCountColumn}, checksOut + countOut + "   my_trend....: count=3\n" + gaugeOut},
		{[]TrendColumn{TrendColumns[0], trendCountColumn},
			checksOut + countOut + "   my_trend....: avg=15ms count=3\n" + gaugeOut},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			TrendColumns = tc.columns
			var w bytes.Buffer
			SummarizeMetrics(&w, " ", time.Second, "", metrics)
			assert.Equal(t, tc.expected, w.String())
		})
	}
}
