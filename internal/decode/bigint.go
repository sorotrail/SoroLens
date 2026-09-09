package decode

import "math/big"

// bigFromParts reassembles a wide integer from 64-bit limbs given
// most-significant first. When signed is true the leading limb carries the
// sign, so two's-complement values such as i128 -1 (hi = -1, lo = 2^64-1)
// reassemble exactly: (-1)*2^64 + (2^64-1) = -1.
func bigFromParts(signed bool, parts ...uint64) *big.Int {
	if len(parts) == 0 {
		return big.NewInt(0)
	}

	var out *big.Int
	if signed {
		out = big.NewInt(int64(parts[0]))
	} else {
		out = new(big.Int).SetUint64(parts[0])
	}

	for _, part := range parts[1:] {
		out.Lsh(out, 64)
		out.Add(out, new(big.Int).SetUint64(part))
	}
	return out
}
