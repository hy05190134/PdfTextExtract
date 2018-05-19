/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package cmap

import (
	"bytes"
)

func hexToUint64(shex cmapHexString) uint64 {
	val := uint64(0)

	for _, v := range shex.b {
		val <<= 8
		val |= uint64(v)
	}

	return val
}

//UTF16BE=>UTF8
func hexToString(shex cmapHexString) string {
	var buf bytes.Buffer

	// Assumes unicode in format <HHLL> with 2 bytes HH and LL representing a rune.
	for i := 0; i < len(shex.b)-1; i += 2 {
		b1 := uint64(shex.b[i])
		b2 := uint64(shex.b[i+1])
		r := rune((b1 << 8) | b2)

		buf.WriteRune(r)
	}

	return buf.String()
}

func Utf8CodepointToUtf8(utf8Codepoint uint) string {
	out := make([]byte, 4)
	if utf8Codepoint < 0x100 {
		out[0] = byte(utf8Codepoint)
		return string(out[0:1])
	} else if utf8Codepoint < 0x10000 {
		out[0] = byte(utf8Codepoint >> 8)
		out[1] = byte(utf8Codepoint & 0x000000FF)
		return string(out[0:2])
	} else if utf8Codepoint < 0x1000000 {
		out[0] = byte(utf8Codepoint >> 16)
		out[1] = byte((utf8Codepoint & 0x0000FF00) >> 8)
		out[2] = byte(utf8Codepoint & 0x000000FF)
		return string(out[0:3])
	} else {
		out[0] = byte(utf8Codepoint >> 24)
		out[1] = byte((utf8Codepoint & 0x00FF0000) >> 16)
		out[2] = byte((utf8Codepoint & 0x0000FF00) >> 8)
		out[3] = byte(utf8Codepoint & 0x000000FF)
		return string(out)
	}

	return ""
}
