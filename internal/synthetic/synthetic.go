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
var SickProfiles = map[string]struct {
	MAPE       float64
	Bias       float64
	Coverage80 float64
	NActuals   int
}{
	"Z3": {MAPE: 0.42, Bias: 0.22, Coverage80: 0.55, NActuals: 80},
	"Z7": {MAPE: 0.31, Bias: -0.18, Coverage80: 0.72, NActuals: 60},
	"Z5": {MAPE: 0.12, Bias: 0.02, Coverage80: 0.81, NActuals: 12},
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
					rows = append(rows, models.MetricRow{
						RunID:       runID,
						EntityType:  "zone",
						EntityID:    zone,
						MAPE:        p.MAPE,
						Bias:        p.Bias,
						Coverage80:  p.Coverage80,
						NActuals:    p.NActuals,
						GeneratedAt: generatedAt,
					})
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
			n := int(clamp(70+rng.NormFloat64()*15, 40, 120))
			rows = append(rows, models.MetricRow{
				RunID:       runID,
				EntityType:  "zone",
				EntityID:    zone,
				MAPE:        round6(mape),
				Bias:        round6(bias),
				Coverage80:  round6(coverage),
				NActuals:    n,
				GeneratedAt: generatedAt,
			})
		}
	}
	return rows
}

func ComputeBaseline(metrics []models.MetricRow, windowDays int) []models.BaselineStats {
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
	out := make([]models.BaselineStats, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		rows := byEntity[entityID]
		mapes := make([]float64, len(rows))
		biases := make([]float64, len(rows))
		for i, r := range rows {
			mapes[i] = r.MAPE
			biases[i] = r.Bias
		}
		out = append(out, models.BaselineStats{
			EntityType: "zone",
			EntityID:   entityID,
			MAPEMean:   mean(mapes),
			MAPEStd:    std(mapes),
			BiasMean:   mean(biases),
			BiasStd:    std(biases),
			WindowDays: windowDays,
		})
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
