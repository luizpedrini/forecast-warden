package synthetic

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

var Zones = []string{"Z1", "Z2", "Z3", "Z4", "Z5", "Z6", "Z7", "Z8"}

// SickProfiles intentionally sick zones on the latest run (demo / golden path).
// Z3/Z7 are also sick on wape so HighWAPE can fire in demos.
var SickProfiles = map[string]struct {
	MAPE       float64
	Bias       float64
	Coverage80 float64
	WAPE       float64
	RMSE       float64
	NActuals   int
}{
	"Z3": {MAPE: 0.42, Bias: 0.22, Coverage80: 0.55, WAPE: 0.48, RMSE: 22.0, NActuals: 80},
	"Z7": {MAPE: 0.31, Bias: -0.18, Coverage80: 0.72, WAPE: 0.35, RMSE: 12.5, NActuals: 60},
	"Z5": {MAPE: 0.12, Bias: 0.02, Coverage80: 0.81, WAPE: 0.14, RMSE: 4.0, NActuals: 12},
}

func GenerateMetrics(days int, endDate time.Time, seed int64) []models.MetricRow {
	if days <= 0 {
		days = 14
	}
	if endDate.IsZero() {
		endDate = time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)
	}
	rng := rand.New(rand.NewSource(seed))
	var rows []models.MetricRow
	for d := 0; d < days; d++ {
		day := endDate.AddDate(0, 0, -(days - 1 - d))
		runID := day.Format("2006-01-02")
		generatedAt := day
		for _, zone := range Zones {
			if d == days-1 {
				if p, ok := SickProfiles[zone]; ok {
					row := models.MetricRow{
						RunID:       runID,
						EntityType:  "zone",
						EntityID:    zone,
						NActuals:    p.NActuals,
						GeneratedAt: generatedAt,
						Metrics:     map[string]float64{},
					}
					row.SetMetric("mape", p.MAPE)
					row.SetMetric("bias", p.Bias)
					row.SetMetric("coverage_80", p.Coverage80)
					row.SetMetric("wape", p.WAPE)
					row.SetMetric("rmse", p.RMSE)
					rows = append(rows, row)
					continue
				}
			}
			mape := clamp(0.12+rng.NormFloat64()*0.02, 0.05, 0.20)
			biasSigma := 0.03
			biasCap := 0.10
			if d == days-1 {
				biasSigma = 0.015
				biasCap = 0.05
			}
			bias := clamp(rng.NormFloat64()*biasSigma, -biasCap, biasCap)
			coverage := clamp(0.82+rng.NormFloat64()*0.03, 0.70, 0.95)
			// WAPE tracks MAPE with a small volume-weight uplift; RMSE scaled ~40× mape.
			wape := clamp(mape*1.05+rng.NormFloat64()*0.01, 0.05, 0.22)
			rmse := clamp(mape*40+rng.NormFloat64()*0.5, 2.0, 9.0)
			n := int(clamp(70+rng.NormFloat64()*15, 40, 120))
			row := models.MetricRow{
				RunID:       runID,
				EntityType:  "zone",
				EntityID:    zone,
				NActuals:    n,
				GeneratedAt: generatedAt,
				Metrics:     map[string]float64{},
			}
			row.SetMetric("mape", round6(mape))
			row.SetMetric("bias", round6(bias))
			row.SetMetric("coverage_80", round6(coverage))
			row.SetMetric("wape", round6(wape))
			row.SetMetric("rmse", round6(rmse))
			rows = append(rows, row)
		}
	}
	return rows
}

func ComputeBaseline(metrics []models.MetricRow, windowDays int) []models.BaselineStat {
	if windowDays <= 0 {
		windowDays = 28
	}
	if len(metrics) == 0 {
		return nil
	}
	runSet := map[string]struct{}{}
	for _, m := range metrics {
		runSet[m.RunID] = struct{}{}
	}
	runIDs := make([]string, 0, len(runSet))
	for id := range runSet {
		runIDs = append(runIDs, id)
	}
	sort.Strings(runIDs)
	history := map[string]struct{}{}
	if len(runIDs) > 1 {
		for _, id := range runIDs[:len(runIDs)-1] {
			history[id] = struct{}{}
		}
	} else {
		for _, id := range runIDs {
			history[id] = struct{}{}
		}
	}
	byEntity := map[string][]models.MetricRow{}
	for _, m := range metrics {
		if _, ok := history[m.RunID]; !ok {
			continue
		}
		byEntity[m.EntityID] = append(byEntity[m.EntityID], m)
	}
	entityIDs := make([]string, 0, len(byEntity))
	for id := range byEntity {
		entityIDs = append(entityIDs, id)
	}
	sort.Strings(entityIDs)

	// Collect metric names present in history.
	metricSet := map[string]struct{}{}
	for _, rows := range byEntity {
		for _, r := range rows {
			for k := range r.Metrics {
				metricSet[k] = struct{}{}
			}
		}
	}
	metricNames := make([]string, 0, len(metricSet))
	for k := range metricSet {
		metricNames = append(metricNames, k)
	}
	sort.Strings(metricNames)

	var out []models.BaselineStat
	for _, entityID := range entityIDs {
		rows := byEntity[entityID]
		et := "zone"
		if len(rows) > 0 && rows[0].EntityType != "" {
			et = rows[0].EntityType
		}
		for _, metric := range metricNames {
			vals := make([]float64, 0, len(rows))
			for _, r := range rows {
				if v, ok := r.Metric(metric); ok {
					vals = append(vals, v)
				}
			}
			if len(vals) == 0 {
				continue
			}
			out = append(out, models.BaselineStat{
				EntityType: et,
				EntityID:   entityID,
				Metric:     metric,
				Mean:       mean(vals),
				Std:        std(vals),
				WindowDays: windowDays,
			})
		}
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func std(xs []float64) float64 {
	if len(xs) < 2 {
		return 0.01
	}
	m := mean(xs)
	variance := 0.0
	for _, x := range xs {
		d := x - m
		variance += d * d
	}
	variance /= float64(len(xs) - 1)
	return math.Max(math.Sqrt(variance), 0.01)
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func round6(x float64) float64 {
	return math.Round(x*1e6) / 1e6
}

func DefaultEndDate() time.Time {
	return time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)
}
