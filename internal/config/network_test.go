package config

import (
	"strings"
	"testing"

	"github.com/sorotrail/sorolens/internal/rpc"
)

func TestParseNetwork(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantName   string
		wantRPC    string
		wantPhrase string
		wantErr    string
	}{
		{"unset defaults to testnet", map[string]string{}, "testnet",
			"https://soroban-testnet.stellar.org", rpc.PassphraseTestnet, ""},
		{"mainnet preset", map[string]string{"NETWORK": "mainnet"}, "mainnet",
			"https://soroban-mainnet.stellar.org", rpc.PassphraseMainnet, ""},
		{"futurenet preset", map[string]string{"NETWORK": "futurenet"}, "futurenet",
			"https://rpc-futurenet.stellar.org", rpc.PassphraseFuturenet, ""},
		{"custom rpc override keeps preset passphrase",
			map[string]string{"NETWORK": "testnet", "RPC_URL": "http://localhost:8000"},
			"testnet", "http://localhost:8000", rpc.PassphraseTestnet, ""},
		{"custom passphrase override",
			map[string]string{"NETWORK": "testnet", "NETWORK_PASSPHRASE": "Standalone Network ; February 7th 1974 at 8:37:19 PM"},
			"testnet", "https://soroban-testnet.stellar.org",
			"Standalone Network ; February 7th 1974 at 8:37:19 PM", ""},
		{"custom network with both set",
			map[string]string{"NETWORK": "custom", "RPC_URL": "http://localhost:8000", "NETWORK_PASSPHRASE": "p"},
			"custom", "http://localhost:8000", "p", ""},
		{"custom without rpc url",
			map[string]string{"NETWORK": "custom", "NETWORK_PASSPHRASE": "p"}, "", "", "", "RPC_URL is required"},
		{"custom without passphrase",
			map[string]string{"NETWORK": "custom", "RPC_URL": "http://localhost:8000"}, "", "", "", "NETWORK_PASSPHRASE is required"},
		{"unknown network", map[string]string{"NETWORK": "regtest"}, "", "", "", "invalid NETWORK"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseNetwork(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name != tt.wantName || got.RPCURL != tt.wantRPC || got.Passphrase != tt.wantPhrase {
				t.Fatalf("got %+v, want name=%s rpc=%s phrase=%s", got, tt.wantName, tt.wantRPC, tt.wantPhrase)
			}
		})
	}
}

func TestVerifyPassphrase(t *testing.T) {
	if err := VerifyPassphrase(rpc.PassphraseTestnet, rpc.PassphraseTestnet); err != nil {
		t.Fatalf("matching passphrases should verify, got %v", err)
	}
	if err := VerifyPassphrase("", ""); err != nil {
		t.Fatalf("empty passphrases skip verification, got %v", err)
	}
	err := VerifyPassphrase(rpc.PassphraseMainnet, rpc.PassphraseTestnet)
	if err == nil || !strings.Contains(err.Error(), "network mismatch") {
		t.Fatalf("mainnet config against testnet node must fail naming both, got %v", err)
	}
}
