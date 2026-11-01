package main

import "math"

// 감점 곡선: penalty(x) = s·k·ln(1 + (x - free)/k)
//   - x <= free 이면 0 (만점 구간)
//   - 시작 기울기는 s (예: 10m당 0.1점 = 0.01점/m)
//   - x 가 커질수록 0.1점 차감에 필요한 간격이 로그적으로 늘어남
//   - x = max 에서 정확히 maxPenalty 가 되도록 k 를 수치적으로 결정, 이후는 maxPenalty 고정
type penaltyCurve struct {
	free, max, slope, maxPenalty, k float64
}

func newPenaltyCurve(free, max, slope, maxPenalty float64) penaltyCurve {
	c := penaltyCurve{free: free, max: max, slope: slope, maxPenalty: maxPenalty}
	span := max - free
	// g(k) = s·k·ln(1+span/k) 는 k 에 대해 단조 증가 → 이분 탐색
	lo, hi := 1e-9, 1e15
	for i := 0; i < 300; i++ {
		mid := math.Sqrt(lo * hi) // 범위가 넓으므로 기하 평균으로 탐색
		if slope*mid*math.Log1p(span/mid) < maxPenalty {
			lo = mid
		} else {
			hi = mid
		}
	}
	c.k = (lo + hi) / 2
	return c
}

func (c penaltyCurve) raw(x float64) float64 {
	switch {
	case x <= c.free:
		return 0
	case x >= c.max:
		return c.maxPenalty
	}
	return math.Min(c.maxPenalty, c.slope*c.k*math.Log1p((x-c.free)/c.k))
}

// Penalty 는 0.1점 단위로 올림한 감점. (예: 10m 를 넘는 순간 0.1점 감점 시작)
func (c penaltyCurve) Penalty(x float64) float64 {
	return math.Min(c.maxPenalty, math.Ceil(c.raw(x)*10-1e-9)/10)
}

var (
	// 거리: 10m 이내 만점, 10m 당 0.1점부터 시작, 20,000km 에서 50점 감점
	distanceCurve = newPenaltyCurve(10, 20_000_000, 0.1/10, 50)
	// 시간: 30초 이내 만점, 10초 당 0.1점부터 시작, 4시간에서 50점 감점
	timeCurve = newPenaltyCurve(30, 4*3600, 0.1/10, 50)
)

type ScoreBreakdown struct {
	Score           float64 `json:"score"`
	DistancePenalty float64 `json:"distancePenalty"`
	TimePenalty     float64 `json:"timePenalty"`
}

func ComputeScore(distanceM, durationS float64) ScoreBreakdown {
	dp := distanceCurve.Penalty(distanceM)
	tp := timeCurve.Penalty(durationS)
	return ScoreBreakdown{
		Score:           math.Max(0, math.Round((100-dp-tp)*10)/10),
		DistancePenalty: dp,
		TimePenalty:     tp,
	}
}

const earthRadiusM = 6_371_008.8

// HaversineMeters 는 두 위경도 사이의 대원 거리(m).
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	toRad := math.Pi / 180
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}
