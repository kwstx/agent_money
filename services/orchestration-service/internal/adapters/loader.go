package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Config represents the adapter configuration file structure
type Config struct {
	Adapters []AdapterConfig `json:"adapters"`
}

type AdapterConfig struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"` // lightning, stripe, stablecoin, x402
	Options map[string]string `json:"options"`
}

// LoadFromConfig populates the registry based on a JSON configuration file
func LoadFromConfig(registry *RailRegistry, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read adapter config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse adapter config: %w", err)
	}

	for _, aCfg := range cfg.Adapters {
		var adapter RailAdapter
		
		switch aCfg.Type {
		case "lightning":
			adapter = NewLightningAdapter()
		case "stripe":
			adapter = NewStripeAdapter()
		case "stablecoin":
			chain := aCfg.Options["chain"]
			adapter = NewStablecoinAdapter(chain)
		case "x402":
			adapter = NewX402Adapter()
		default:
			return fmt.Errorf("unknown adapter type: %s", aCfg.Type)
		}

		// Wrap with resilience by default
		registry.Register(NewResilienceWrapper(adapter))
	}

	return nil
}

// InitializeDefaultRegistry creates a registry with hardcoded defaults if no config is found
func InitializeDefaultRegistry(ctx context.Context) *RailRegistry {
	registry := NewRailRegistry()
	
	// Default set
	registry.Register(NewResilienceWrapper(NewLightningAdapter()))
	registry.Register(NewResilienceWrapper(NewStripeAdapter()))
	registry.Register(NewResilienceWrapper(NewStablecoinAdapter("solana")))
	registry.Register(NewResilienceWrapper(NewStablecoinAdapter("ethereum")))
	registry.Register(NewResilienceWrapper(NewX402Adapter()))

	return registry
}
