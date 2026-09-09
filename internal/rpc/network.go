package rpc

// NetworkPassphrases are the well-known Stellar network passphrases. A
// network's passphrase is its identity: two nodes with the same passphrase
// are the same network. SoroLens verifies at startup — in standalone mode —
// that the RPC endpoint it polls really belongs to the configured network,
// because the classic footgun is polling mainnet with testnet expectations
// and reading it as "the contract has no activity".
//
// Standalone quickstart networks (docker stellar/quickstart) use a
// passphrase of the operator's choosing; pass NETWORK_PASSPHRASE explicitly
// for those.
const (
	PassphraseTestnet   = "Test SDF Network ; September 2015"
	PassphraseMainnet   = "Public Global Stellar Network ; September 2015"
	PassphraseFuturenet = "Test SDF Future Network ; October 2022"
)

// Network is the result of the RPC's getNetwork method.
type Network struct {
	// Passphrase identifies the network the node belongs to.
	Passphrase string `json:"passphrase"`
	// FriendlierURL is a human-facing URL for the network, where one exists.
	FriendlierURL string `json:"friendlierUrl,omitempty"`
	// ProtocolVersion is the current ledger protocol version.
	ProtocolVersion int `json:"protocolVersion,omitempty"`
}

// NetworkName maps a known passphrase to a short label ("testnet",
// "mainnet", "futurenet"), or "" for an unknown passphrase.
func NetworkName(passphrase string) string {
	switch passphrase {
	case PassphraseTestnet:
		return "testnet"
	case PassphraseMainnet:
		return "mainnet"
	case PassphraseFuturenet:
		return "futurenet"
	}
	return ""
}
