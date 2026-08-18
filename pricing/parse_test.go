package pricing

import (
	"os"
	"testing"
)

func TestParseRatesGoFile(t *testing.T) {
	src, err := os.ReadFile("rates_default.go")
	if err != nil {
		t.Fatal(err)
	}
	prices, err := ParseRatesGoFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) == 0 {
		t.Fatal("expected non-empty prices")
	}
	if prices[0].ModelID == "" {
		t.Error("first price has empty ModelID")
	}
	t.Logf("parsed %d prices, first: %s %.2f/%.2f", len(prices), prices[0].ModelID, prices[0].InputPerMillion, prices[0].OutputPerMillion)
}
