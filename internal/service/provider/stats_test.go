package provider

import "testing"

func TestStatsRecordTTFT(t *testing.T) {
	stats := NewStats()
	stats.providerStats["p1"] = &providerStatsAtomic{name: "p1"}

	stats.RecordTTFT("p1", 120)
	stats.RecordTTFT("p1", 80)

	got, ok := stats.Get("p1")
	if !ok {
		t.Fatal("Stats.Get(p1) ok = false, want true")
	}
	if got.AvgTTFTMs != 100 {
		t.Fatalf("AvgTTFTMs = %v, want 100", got.AvgTTFTMs)
	}
	if got.MinTTFTMs != 80 {
		t.Fatalf("MinTTFTMs = %d, want 80", got.MinTTFTMs)
	}
	if got.MaxTTFTMs != 120 {
		t.Fatalf("MaxTTFTMs = %d, want 120", got.MaxTTFTMs)
	}
}
