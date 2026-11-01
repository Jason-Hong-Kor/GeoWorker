package main

import (
	"math"
	"testing"
)

func TestDistancePenalty(t *testing.T) {
	cases := []struct {
		d    float64
		want float64
	}{
		{0, 0}, {10, 0}, {15, 0.1}, {20, 0.1}, {20_000_000, 50}, {30_000_000, 50},
	}
	for _, c := range cases {
		if got := distanceCurve.Penalty(c.d); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("distance %.0fm: got %.1f want %.1f", c.d, got, c.want)
		}
	}
	if math.Abs(distanceCurve.raw(20_000_000)-50) > 1e-6 {
		t.Errorf("raw at 20000km = %v", distanceCurve.raw(20_000_000))
	}
}

func TestTimePenalty(t *testing.T) {
	cases := []struct {
		s    float64
		want float64
	}{
		{0, 0}, {30, 0}, {35, 0.1}, {40, 0.1}, {4 * 3600, 50}, {10 * 3600, 50},
	}
	for _, c := range cases {
		if got := timeCurve.Penalty(c.s); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("time %.0fs: got %.1f want %.1f", c.s, got, c.want)
		}
	}
}

func TestMonotonicAndDecelerating(t *testing.T) {
	for _, c := range []penaltyCurve{distanceCurve, timeCurve} {
		prev := 0.0
		step := (c.max - c.free) / 1000
		prevGain := math.Inf(1)
		for x := c.free + step; x <= c.max; x += step {
			p := c.raw(x)
			if p < prev {
				t.Fatalf("not monotonic at %v", x)
			}
			if gain := p - prev; gain > prevGain+1e-9 {
				t.Fatalf("penalty growth accelerating at %v", x)
			} else {
				prevGain = gain
			}
			prev = p
		}
	}
}

func TestComputeScore(t *testing.T) {
	if s := ComputeScore(5, 10).Score; s != 100 {
		t.Errorf("perfect: %v", s)
	}
	if s := ComputeScore(25_000_000, 5*3600).Score; s != 0 {
		t.Errorf("worst: %v", s)
	}
}

func TestHaversine(t *testing.T) {
	// 서울시청 ↔ 부산시청 약 325km
	d := HaversineMeters(37.5663, 126.9779, 35.1796, 129.0756)
	if d < 320_000 || d > 330_000 {
		t.Errorf("seoul-busan = %.0f", d)
	}
}
