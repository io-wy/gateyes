package main

import (
	"fmt"
	"github.com/gateyes/gateway/internal/config"
)

func main() {
	cfg, err := config.Load("benchmark/deploy/bench.yaml")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for _, p := range cfg.Providers {
		fmt.Printf("Provider: %s, BaseURL: %q, APIKey: %q, Model: %q\n", p.Name, p.BaseURL, p.APIKey, p.Model)
	}
}
