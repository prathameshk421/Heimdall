package budget

import (
	"context"
	"strings"
	"testing"
)

type ledgerFake struct {
	total    float64
	purposes map[string]float64
}

func (l ledgerFake) Total(context.Context) (float64, map[string]float64, error) {
	return l.total, l.purposes, nil
}

type balanceFake float64

func (b balanceFake) Balance(context.Context) (float64, error) { return float64(b), nil }

func TestCheckRefusesCapPurposeAndBalance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		total    float64
		purposes map[string]float64
		balance  float64
		want     string
	}{
		{"cap", 2.8, nil, 10, "3"},
		{"purpose", 0, map[string]float64{"eval": .9}, 10, "eval"},
		{"balance", 0, nil, .19, "balance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := Guard{CapUSD: 3, PerPurpose: map[string]float64{"eval": 1}, Ledger: ledgerFake{tc.total, tc.purposes}, Balance: balanceFake(tc.balance)}
			err := g.Check(context.Background(), "eval", .1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Check() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestCheckRefusesExactlyAtLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		total    float64
		purposes map[string]float64
		cap      float64
		purpose  float64
	}{
		{name: "testing cap", total: 2.8, purposes: map[string]float64{"eval": .2}, cap: 3, purpose: 1},
		{name: "purpose budget", total: .9, purposes: map[string]float64{"eval": .8}, cap: 3, purpose: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := Guard{CapUSD: tc.cap, PerPurpose: map[string]float64{"eval": tc.purpose}, Ledger: ledgerFake{tc.total, tc.purposes}, Balance: balanceFake(10)}
			if err := g.Check(context.Background(), "eval", .1); err == nil {
				t.Fatalf("Check() = nil at exact budget limit")
			}
		})
	}
}

func TestStopAfterTwiceEstimate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		actual   float64
		estimate float64
		want     bool
	}{
		{name: "below", actual: .19, estimate: .1, want: false},
		{name: "exact", actual: .2, estimate: .1, want: true},
		{name: "above", actual: .21, estimate: .1, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StopAfterTwiceEstimate(tc.actual, tc.estimate); got != tc.want {
				t.Fatalf("StopAfterTwiceEstimate(%v, %v) = %v, want %v", tc.actual, tc.estimate, got, tc.want)
			}
		})
	}
}
