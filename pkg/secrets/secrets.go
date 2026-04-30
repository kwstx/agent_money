package secrets

import (
	"context"
	"fmt"
	"os"
)

// SecretProvider defines the interface for retrieving secrets.
type SecretProvider interface {
	GetSecret(ctx context.Context, key string) (string, error)
}

// EnvProvider retrieves secrets from environment variables.
type EnvProvider struct{}

func (p *EnvProvider) GetSecret(ctx context.Context, key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("secret %s not found in environment", key)
	}
	return val, nil
}

// VaultProvider (placeholder for HashiCorp Vault)
type VaultProvider struct {
	// Add vault client configuration here
}

func (p *VaultProvider) GetSecret(ctx context.Context, key string) (string, error) {
	// Implement Vault lookup logic here
	// In a real implementation, you would use the Vault API client
	return "", fmt.Errorf("vault provider not fully implemented")
}

var defaultProvider SecretProvider = &EnvProvider{}

// SetDefaultProvider sets the global secret provider.
func SetDefaultProvider(p SecretProvider) {
	defaultProvider = p
}

// Get retrieves a secret using the default provider.
func Get(ctx context.Context, key string) (string, error) {
	return defaultProvider.GetSecret(ctx, key)
}
