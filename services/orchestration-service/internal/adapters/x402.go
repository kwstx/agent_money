package adapters

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

type X402Adapter struct {
	Client *http.Client
}

func NewX402Adapter() *X402Adapter {
	return &X402Adapter{
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (a *X402Adapter) GetID() string {
	return "x402"
}

func (a *X402Adapter) Execute(ctx context.Context, tx Transaction) (*ExecutionResult, error) {
	targetURL, ok := tx.Context["target_url"].(string)
	if !ok {
		return nil, fmt.Errorf("x402: missing target_url in context")
	}

	log.Printf("[x402] Initial request to %s for transaction %s", targetURL, tx.ID)

	// 1. Initial request to target URL
	req, _ := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x402 initial request failed: %w", err)
	}
	defer resp.Body.Close()

	// 2. Catch 402 response
	if resp.StatusCode == http.StatusPaymentRequired {
		log.Printf("[x402] Received 402 Payment Required. Extracting payment info...")

		// 3. Extract 'Link' or 'WWW-Authenticate' headers for payment info
		// Mock extraction of BOLT11 invoice or payment request
		paymentRequest := resp.Header.Get("X-Payment-Request")
		if paymentRequest == "" {
			paymentRequest = "mock_bolt11_invoice_from_402_header"
		}

		log.Printf("[x402] Found payment request: %s", paymentRequest)

		// 4. Re-send original request with proof (Authorization: x402)
		// We'll simulate the retry with the header
		log.Printf("[x402] Retrying request with Authorization: x402 header")
		
		retryReq, _ := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		// The header contains a signed transaction or proof
		proof := fmt.Sprintf("signed_tx_%s_proof", tx.ID)
		retryReq.Header.Set("Authorization", fmt.Sprintf("x402 %s", proof))

		retryResp, err := a.Client.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("x402 retry request failed: %w", err)
		}
		defer retryResp.Body.Close()

		if retryResp.StatusCode == http.StatusOK {
			return &ExecutionResult{
				TransactionID: tx.ID,
				ProviderID:    fmt.Sprintf("x402-session-%s", tx.ID),
				Status:        "completed",
				Metadata: map[string]interface{}{
					"target_url": targetURL,
					"proof":      proof,
				},
			}, nil
		}
		return nil, fmt.Errorf("x402 retry failed with status: %d", retryResp.StatusCode)
	}

	return &ExecutionResult{
		TransactionID: tx.ID,
		ProviderID:    fmt.Sprintf("direct-%s", tx.ID),
		Status:        "completed",
		Metadata: map[string]interface{}{
			"target_url": targetURL,
			"note":       "No 402 required",
		},
	}, nil
}

func (a *X402Adapter) GetCostEstimate(amount float64, context map[string]interface{}) (float64, error) {
	return 0.05, nil
}

func (a *X402Adapter) GetLatencyEstimate() int {
	return 1500
}

func (a *X402Adapter) HealthCheck() bool {
	return true
}
