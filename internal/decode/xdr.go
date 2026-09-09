package decode

import (
	"encoding/json"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// XDRDecoder decodes base64 XDR ScVals using the Stellar SDK. It produces the
// same single-key wrapper shape the RPC emits under xdrFormat "json"
// (for example {"symbol":"transfer"} or {"i128":"1000"}), so stored rows look
// identical no matter which path produced them.
type XDRDecoder struct{}

// NewXDRDecoder returns the default Decoder.
func NewXDRDecoder() XDRDecoder { return XDRDecoder{} }

// DecodeScVal implements Decoder.
func (XDRDecoder) DecodeScVal(base64XDR string) (json.RawMessage, error) {
	var sc xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(base64XDR, &sc); err != nil {
		return nil, fmt.Errorf("unmarshaling ScVal: %w", err)
	}
	v, err := scValToJSON(sc)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// scValToJSON maps an ScVal onto the RPC's JSON wrapper vocabulary. Integers
// wider than 64 bits are emitted as decimal strings, matching the RPC, so no
// precision is lost passing through JSON.
func scValToJSON(sc xdr.ScVal) (any, error) {
	switch sc.Type {
	case xdr.ScValTypeScvBool:
		if v, ok := sc.GetB(); ok {
			return wrap("bool", v), nil
		}
	case xdr.ScValTypeScvVoid:
		return wrap("void", nil), nil
	case xdr.ScValTypeScvU32:
		if v, ok := sc.GetU32(); ok {
			return wrap("u32", uint32(v)), nil
		}
	case xdr.ScValTypeScvI32:
		if v, ok := sc.GetI32(); ok {
			return wrap("i32", int32(v)), nil
		}
	case xdr.ScValTypeScvU64:
		if v, ok := sc.GetU64(); ok {
			return wrap("u64", fmt.Sprint(uint64(v))), nil
		}
	case xdr.ScValTypeScvI64:
		if v, ok := sc.GetI64(); ok {
			return wrap("i64", fmt.Sprint(int64(v))), nil
		}
	case xdr.ScValTypeScvTimepoint:
		if v, ok := sc.GetTimepoint(); ok {
			return wrap("timepoint", fmt.Sprint(uint64(v))), nil
		}
	case xdr.ScValTypeScvDuration:
		if v, ok := sc.GetDuration(); ok {
			return wrap("duration", fmt.Sprint(uint64(v))), nil
		}
	case xdr.ScValTypeScvU128:
		if v, ok := sc.GetU128(); ok {
			return wrap("u128", u128String(v)), nil
		}
	case xdr.ScValTypeScvI128:
		if v, ok := sc.GetI128(); ok {
			return wrap("i128", i128String(v)), nil
		}
	case xdr.ScValTypeScvU256:
		if v, ok := sc.GetU256(); ok {
			return wrap("u256", u256String(v)), nil
		}
	case xdr.ScValTypeScvI256:
		if v, ok := sc.GetI256(); ok {
			return wrap("i256", i256String(v)), nil
		}
	case xdr.ScValTypeScvBytes:
		if v, ok := sc.GetBytes(); ok {
			return wrap("bytes", fmt.Sprintf("%x", []byte(v))), nil
		}
	case xdr.ScValTypeScvString:
		if v, ok := sc.GetStr(); ok {
			return wrap("string", string(v)), nil
		}
	case xdr.ScValTypeScvSymbol:
		if v, ok := sc.GetSym(); ok {
			return wrap("symbol", string(v)), nil
		}
	case xdr.ScValTypeScvAddress:
		if v, ok := sc.GetAddress(); ok {
			s, err := v.String()
			if err != nil {
				return nil, fmt.Errorf("rendering address: %w", err)
			}
			return wrap("address", s), nil
		}
	case xdr.ScValTypeScvVec:
		if v, ok := sc.GetVec(); ok && v != nil {
			items := make([]any, 0, len(*v))
			for _, item := range *v {
				decoded, err := scValToJSON(item)
				if err != nil {
					return nil, err
				}
				items = append(items, decoded)
			}
			return wrap("vec", items), nil
		}
	case xdr.ScValTypeScvMap:
		if v, ok := sc.GetMap(); ok && v != nil {
			entries := make([]any, 0, len(*v))
			for _, e := range *v {
				k, err := scValToJSON(e.Key)
				if err != nil {
					return nil, err
				}
				val, err := scValToJSON(e.Val)
				if err != nil {
					return nil, err
				}
				entries = append(entries, map[string]any{"key": k, "val": val})
			}
			return wrap("map", entries), nil
		}
	}

	// Errors, contract instances, ledger keys and any type added by a future
	// protocol version fall back to the SDK's own rendering rather than being
	// dropped, so nothing is silently lost.
	return wrap("unknown", sc.String()), nil
}

func wrap(key string, v any) map[string]any { return map[string]any{key: v} }

func u128String(v xdr.UInt128Parts) string {
	return bigFromParts(false, uint64(v.Hi), uint64(v.Lo)).String()
}

func i128String(v xdr.Int128Parts) string {
	return bigFromParts(true, uint64(v.Hi), uint64(v.Lo)).String()
}

func u256String(v xdr.UInt256Parts) string {
	return bigFromParts(false, uint64(v.HiHi), uint64(v.HiLo), uint64(v.LoHi), uint64(v.LoLo)).String()
}

func i256String(v xdr.Int256Parts) string {
	return bigFromParts(true, uint64(v.HiHi), uint64(v.HiLo), uint64(v.LoHi), uint64(v.LoLo)).String()
}
