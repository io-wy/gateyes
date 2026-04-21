package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestLiveGRPCVLLMProvider(t *testing.T) {
	if os.Getenv("GATEYES_LIVE") != "1" {
		t.Skip("set GATEYES_LIVE=1 to run live grpc-vllm checks")
	}

	cfgPath := os.Getenv("GATEYES_LIVE_CONFIG")
	if strings.TrimSpace(cfgPath) == "" {
		cfgPath = "configs/config_grpc.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config %s: %v", cfgPath, err)
	}

	providers := selectLiveGRPCProviders(cfg, os.Getenv("GATEYES_LIVE_PROVIDERS"))
	if len(providers) == 0 {
		t.Skip("no enabled grpc-vllm provider selected for live grpc probe")
	}

	for _, providerCfg := range providers {
		providerCfg := providerCfg
		t.Run(providerCfg.Name, func(t *testing.T) {
			if strings.Contains(providerCfg.GRPCTarget, "${") || strings.Contains(providerCfg.Model, "${") {
				t.Skipf("provider %s still has unresolved grpc env placeholders", providerCfg.Name)
			}

			instance, err := newProvider(providerCfg)
			if err != nil {
				t.Fatalf("newProvider(%s): %v", providerCfg.Name, err)
			}
			grpcProvider, ok := instance.(*grpcProvider)
			if !ok {
				t.Fatalf("provider type = %T, want *grpcProvider", instance)
			}

			ctx := context.Background()
			conn, outgoing, err := grpcProvider.prepareCall(ctx)
			if err != nil {
				t.Fatalf("prepareCall(%s): %v", providerCfg.Name, err)
			}
			archive, err := fetchGRPCTokenizerArchive(ctx, conn, outgoing)
			if err != nil {
				t.Fatalf("fetchGRPCTokenizerArchive(%s): %v", providerCfg.Name, err)
			}
			if len(archive) == 0 {
				t.Fatalf("fetchGRPCTokenizerArchive(%s) returned empty archive", providerCfg.Name)
			}

			t.Run("create_response", func(t *testing.T) {
				resp, err := grpcProvider.CreateResponse(ctx, &ResponseRequest{
					Model: providerCfg.Model,
					Messages: []Message{{
						Role:    "user",
						Content: TextBlocks("Reply with a short sentence proving Gateyes gRPC live probe works."),
					}},
					MaxTokens: 128,
				})
				if err != nil {
					t.Fatalf("CreateResponse(%s): %v", providerCfg.Name, err)
				}
				if strings.TrimSpace(resp.OutputText()) == "" {
					t.Fatalf("CreateResponse(%s) returned empty text: %+v", providerCfg.Name, resp)
				}
			})

			t.Run("stream_response", func(t *testing.T) {
				events, errs := grpcProvider.StreamResponse(ctx, &ResponseRequest{
					Model: providerCfg.Model,
					Messages: []Message{{
						Role:    "user",
						Content: TextBlocks("Stream a short sentence proving Gateyes gRPC live probe works."),
					}},
					Stream:    true,
					MaxTokens: 128,
				})

				var sawDelta bool
				var sawCompleted bool
				var completedText string
				for event := range events {
					switch event.Type {
					case EventContentDelta:
						if strings.TrimSpace(event.Text()) != "" {
							sawDelta = true
						}
					case EventResponseCompleted:
						sawCompleted = true
						if event.Response != nil {
							completedText = event.Response.OutputText()
						}
					}
				}
				for err := range errs {
					if err != nil {
						t.Fatalf("StreamResponse(%s): %v", providerCfg.Name, err)
					}
				}
				if !sawDelta {
					t.Fatalf("StreamResponse(%s) saw no non-empty delta", providerCfg.Name)
				}
				if !sawCompleted {
					t.Fatalf("StreamResponse(%s) saw no completed response", providerCfg.Name)
				}
				if strings.TrimSpace(completedText) == "" {
					t.Fatalf("StreamResponse(%s) completed with empty text", providerCfg.Name)
				}
			})

			if closer, ok := any(grpcProvider).(interface{ CloseIdleConnections() }); ok {
				closer.CloseIdleConnections()
			}
		})
	}
}

func selectLiveGRPCProviders(cfg *config.Config, selected string) []config.ProviderConfig {
	if cfg == nil {
		return nil
	}

	enabled := make(map[string]config.ProviderConfig)
	order := make([]config.ProviderConfig, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if !providerCfg.Enabled {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(providerCfg.Type), "grpc") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(providerCfg.Vendor), "vllm") {
			continue
		}
		order = append(order, providerCfg)
		enabled[providerCfg.Name] = providerCfg
	}

	if strings.TrimSpace(selected) == "" {
		return order
	}

	var result []config.ProviderConfig
	for _, name := range strings.Split(selected, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		providerCfg, ok := enabled[name]
		if !ok {
			continue
		}
		result = append(result, providerCfg)
	}
	return result
}

func TestSelectLiveGRPCProvidersFiltersOnlyEnabledGRPCVLLM(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "grpc-vllm-a", Type: "grpc", Vendor: "vllm", Enabled: true},
			{Name: "grpc-vllm-disabled", Type: "grpc", Vendor: "vllm", Enabled: false},
			{Name: "grpc-other", Type: "grpc", Vendor: "other", Enabled: true},
			{Name: "openai-http", Type: "openai", Vendor: "vllm", Enabled: true},
		},
	}

	all := selectLiveGRPCProviders(cfg, "")
	if len(all) != 1 || all[0].Name != "grpc-vllm-a" {
		t.Fatalf("selectLiveGRPCProviders(all) = %+v, want only enabled grpc-vllm", all)
	}

	selected := selectLiveGRPCProviders(cfg, "grpc-vllm-a,grpc-other,missing")
	if len(selected) != 1 || selected[0].Name != "grpc-vllm-a" {
		t.Fatalf("selectLiveGRPCProviders(selected) = %+v, want only grpc-vllm-a", selected)
	}
}
