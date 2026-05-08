package config

import (
	"fmt"
	"testing"
)

func TestLoadRealProviders(t *testing.T) {
	cfg, err := Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Providers {
		fmt.Printf("name=%s baseURL=%q apiKey=%q model=%q\n", p.Name, p.BaseURL, p.APIKey, p.Model)
	}
}
