package solana

import (
	"errors"
	"fmt"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Index = func() [256]int8 {
	var index [256]int8
	for i := range index {
		index[i] = -1
	}
	for i, c := range base58Alphabet {
		index[c] = int8(i)
	}
	return index
}()

// Base58Encode encodes b in the Bitcoin alphabet Solana uses; each leading
// zero byte becomes a leading '1'.
func Base58Encode(b []byte) string {
	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}
	// log(256)/log(58) < 1.37
	digits := make([]byte, 0, len(b)*137/100+1)
	for _, v := range b[zeros:] {
		carry := int(v)
		for i := range digits {
			carry += int(digits[i]) << 8
			digits[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			digits = append(digits, byte(carry%58))
			carry /= 58
		}
	}
	out := make([]byte, zeros+len(digits))
	for i := range zeros {
		out[i] = '1'
	}
	for i, d := range digits {
		out[len(out)-1-i] = base58Alphabet[d]
	}
	return string(out)
}

// Base58Decode reverses Base58Encode.
func Base58Decode(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("solana: empty base58 string")
	}
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	// log(58)/log(256) < 0.733
	bytes := make([]byte, 0, len(s)*733/1000+1)
	for i := zeros; i < len(s); i++ {
		d := base58Index[s[i]]
		if d < 0 {
			return nil, fmt.Errorf("solana: invalid base58 character %q", s[i])
		}
		carry := int(d)
		for j := range bytes {
			carry += int(bytes[j]) * 58
			bytes[j] = byte(carry)
			carry >>= 8
		}
		for carry > 0 {
			bytes = append(bytes, byte(carry))
			carry >>= 8
		}
	}
	out := make([]byte, zeros+len(bytes))
	for i, b := range bytes {
		out[len(out)-1-i] = b
	}
	return out, nil
}
