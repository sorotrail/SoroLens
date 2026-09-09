package decode

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustBase64 encodes an ScVal the way the RPC does, so the tests decode
// exactly what a node would send.
func mustBase64(t *testing.T, v xdr.ScVal) string {
	t.Helper()
	s, err := xdr.MarshalBase64(v)
	require.NoError(t, err)
	return s
}

func TestXDRDecoderDecodeScVal(t *testing.T) {
	sym := xdr.ScSymbol("transfer")
	str := xdr.ScString("hello")
	b := true
	u32 := xdr.Uint32(7)
	i32 := xdr.Int32(-7)
	u64 := xdr.Uint64(1 << 40)
	i64 := xdr.Int64(-(1 << 40))
	bytes := xdr.ScBytes{0xde, 0xad, 0xbe, 0xef}

	tests := []struct {
		name string
		val  xdr.ScVal
		want string
	}{
		{
			name: "symbol",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym},
			want: `{"symbol":"transfer"}`,
		},
		{
			name: "string",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &str},
			want: `{"string":"hello"}`,
		},
		{
			name: "bool",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvBool, B: &b},
			want: `{"bool":true}`,
		},
		{
			name: "void",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvVoid},
			want: `{"void":null}`,
		},
		{
			name: "u32",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u32},
			want: `{"u32":7}`,
		},
		{
			name: "i32 keeps its sign",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvI32, I32: &i32},
			want: `{"i32":-7}`,
		},
		{
			// 64-bit values become strings so no precision is lost passing
			// through JSON, which is what the RPC does too.
			name: "u64 is a string",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: &u64},
			want: `{"u64":"1099511627776"}`,
		},
		{
			name: "i64 is a string",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvI64, I64: &i64},
			want: `{"i64":"-1099511627776"}`,
		},
		{
			name: "bytes are hex",
			val:  xdr.ScVal{Type: xdr.ScValTypeScvBytes, Bytes: &bytes},
			want: `{"bytes":"deadbeef"}`,
		},
	}

	dec := NewXDRDecoder()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dec.DecodeScVal(mustBase64(t, tt.val))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestXDRDecoderWideIntegers(t *testing.T) {
	tests := []struct {
		name string
		val  xdr.ScVal
		want string
	}{
		{
			name: "u128 spanning both limbs",
			val: xdr.ScVal{Type: xdr.ScValTypeScvU128, U128: &xdr.UInt128Parts{
				Hi: xdr.Uint64(1), Lo: xdr.Uint64(0),
			}},
			// 1 * 2^64
			want: `{"u128":"18446744073709551616"}`,
		},
		{
			name: "u128 maximum",
			val: xdr.ScVal{Type: xdr.ScValTypeScvU128, U128: &xdr.UInt128Parts{
				Hi: xdr.Uint64(0xFFFFFFFFFFFFFFFF), Lo: xdr.Uint64(0xFFFFFFFFFFFFFFFF),
			}},
			want: `{"u128":"340282366920938463463374607431768211455"}`,
		},
		{
			// Two's complement: hi = -1, lo = 2^64-1 is exactly -1.
			name: "i128 negative one",
			val: xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{
				Hi: xdr.Int64(-1), Lo: xdr.Uint64(0xFFFFFFFFFFFFFFFF),
			}},
			want: `{"i128":"-1"}`,
		},
		{
			name: "i128 positive",
			val: xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{
				Hi: xdr.Int64(0), Lo: xdr.Uint64(1000),
			}},
			want: `{"i128":"1000"}`,
		},
		{
			name: "u256 across four limbs",
			val: xdr.ScVal{Type: xdr.ScValTypeScvU256, U256: &xdr.UInt256Parts{
				HiHi: xdr.Uint64(0), HiLo: xdr.Uint64(0),
				LoHi: xdr.Uint64(1), LoLo: xdr.Uint64(0),
			}},
			want: `{"u256":"18446744073709551616"}`,
		},
		{
			name: "i256 negative one",
			val: xdr.ScVal{Type: xdr.ScValTypeScvI256, I256: &xdr.Int256Parts{
				HiHi: xdr.Int64(-1), HiLo: xdr.Uint64(0xFFFFFFFFFFFFFFFF),
				LoHi: xdr.Uint64(0xFFFFFFFFFFFFFFFF), LoLo: xdr.Uint64(0xFFFFFFFFFFFFFFFF),
			}},
			want: `{"i256":"-1"}`,
		},
	}

	dec := NewXDRDecoder()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dec.DecodeScVal(mustBase64(t, tt.val))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestXDRDecoderNestedValues(t *testing.T) {
	sym := xdr.ScSymbol("transfer")
	amount := xdr.Uint32(42)

	t.Run("vec", func(t *testing.T) {
		vec := xdr.ScVec{
			{Type: xdr.ScValTypeScvSymbol, Sym: &sym},
			{Type: xdr.ScValTypeScvU32, U32: &amount},
		}
		vecPtr := &vec
		val := xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vecPtr}

		got, err := NewXDRDecoder().DecodeScVal(mustBase64(t, val))
		require.NoError(t, err)
		assert.JSONEq(t, `{"vec":[{"symbol":"transfer"},{"u32":42}]}`, string(got))
	})

	t.Run("map", func(t *testing.T) {
		entries := xdr.ScMap{
			{
				Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym},
				Val: xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &amount},
			},
		}
		entriesPtr := &entries
		val := xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &entriesPtr}

		got, err := NewXDRDecoder().DecodeScVal(mustBase64(t, val))
		require.NoError(t, err)
		assert.JSONEq(t,
			`{"map":[{"key":{"symbol":"transfer"},"val":{"u32":42}}]}`,
			string(got))
	})
}

func TestXDRDecoderRejectsGarbage(t *testing.T) {
	_, err := NewXDRDecoder().DecodeScVal("not-valid-base64-xdr!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshaling ScVal")
}

func TestBigFromParts(t *testing.T) {
	tests := []struct {
		name   string
		signed bool
		parts  []uint64
		want   string
	}{
		{"no parts is zero", false, nil, "0"},
		{"single unsigned limb", false, []uint64{7}, "7"},
		{"single signed limb", true, []uint64{uint64(0xFFFFFFFFFFFFFFFF)}, "-1"},
		{"two unsigned limbs", false, []uint64{1, 0}, "18446744073709551616"},
		{"signed two limbs is negative one", true, []uint64{uint64(0xFFFFFFFFFFFFFFFF), 0xFFFFFFFFFFFFFFFF}, "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bigFromParts(tt.signed, tt.parts...).String())
		})
	}
}
