/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package cmap

import (
	"../common"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	//"github.com/unidoc/unidoc/pdf/model/textencoding"
	"io"
	"math"
)

// CMap represents a character code to unicode mapping used in PDF files.
type CMap struct {
	*cMapParser

	// Text encoder to look up runes from input glyph names.
	//encoder textencoding.TextEncoder

	codeMap map[uint64]string

	name       string
	ctype      int
	codespaces []codespace
	// use to show the code space length, 0x10, 0x100, 0x1000, 0x10000
	codeSpan int8
}

func (c *CMap) GetCodeMap() map[uint64]string {
	return c.codeMap
}

// codespace represents a single codespace range used in the CMap.
type codespace struct {
	low  uint64
	high uint64
}

// Name returns the name of the CMap.
func (cmap *CMap) Name() string {
	return cmap.name
}

// Type returns the type of the CMap.
func (cmap *CMap) Type() int {
	return cmap.ctype
}

// CharcodeBytesToUnicode converts a byte array of charcodes to a unicode string representation.
func (cmap *CMap) CharcodeBytesToUnicode(src []byte, simpleEncoding []uint, flag bool) string {
	var buf bytes.Buffer

	// Maximum number of possible bytes per code.
	maxLen := 4

	i := 0
	for i < len(src) {
		var code uint64
		var j int
		encodingList := make([]string, 4)

		for j = 0; j < maxLen && i+j < len(src); j++ {
			b := src[i+j]

			if flag {
				encodingList = append(encodingList, Utf8CodepointToUtf8(simpleEncoding[b]))
			}

			code <<= 8
			code |= uint64(b)

			tgt, has := cmap.codeMap[code]
			if has && (cmap.codeSpan&int8(math.Pow(2.0, float64(j+1)))) > 0 {
				buf.WriteString(tgt)
				break
			} else if j == maxLen-1 || i+j == len(src)-1 {
				/*if !flag {
					common.Log.Debug("Error: can't map to unicode, need check, src: 0X%X, 0X%X, 0X%X, 0X%X", code, code>>8, code>>16, code>>24)
					if i+j-3 > 0 {
						buf.WriteString(string(src[i+j-3 : i+j+1]))
					} else {
						buf.WriteString(string(src[0 : i+j+1]))
					}
				} else {
					for k := 0; k < len(encodingList); k++ {
						buf.WriteString(encodingList[k])
					}
				}*/
				break
			}
		}
		i += j + 1
	}

	return buf.String()
}

// CharcodeBytesToUnicode converts a byte array of charcodes to a unicode string representation.
func (cmap *CMap) CharcodeBytesToCidStr(src []byte) string {
	var buf bytes.Buffer

	// Maximum number of possible bytes per code.
	maxLen := 4

	i := 0
	for i < len(src) {
		var code uint64
		var j int

		for j = 0; j < maxLen && i+j < len(src); j++ {
			b := src[i+j]

			code <<= 8
			code |= uint64(b)

			tgt, has := cmap.codeMap[code]
			if has && (cmap.codeSpan&int8(math.Pow(2.0, float64(j+1)))) > 0 {
				//tgt is hex string for codeid
				if decoded, err := hex.DecodeString(tgt); err == nil {
					buf.WriteString(string(decoded))
				}
				break
			} else if j == maxLen-1 || i+j == len(src)-1 {
				common.Log.Debug("Error: can't map to cid code, need check, src: 0X%X, 0X%X, 0X%X, 0X%X", code, code>>8, code>>16, code>>24)
				if i+j-3 > 0 {
					buf.WriteString(string(src[i+j-3 : i+j+1]))
				} else {
					buf.WriteString(string(src[0 : i+j+1]))
				}
				break
			}
		}
		i += j + 1
	}

	return buf.String()
}

// CharcodeToUnicode converts a single character code to unicode string.
func (cmap *CMap) CharcodeToUnicode(srcCode uint64) string {
	if c, has := cmap.codeMap[srcCode]; has {
		return c
	}

	// Not found.
	return "?"
}

// newCMap returns an initialized CMap.
func newCMap() *CMap {
	cmap := &CMap{}
	cmap.codespaces = []codespace{}
	//TODO: If codeSpan conflict, uint64 should be {val: uint64, codeLen: int8}
	cmap.codeMap = map[uint64]string{}
	cmap.codeSpan = 0
	return cmap
}

// LoadCmapFromData parses CMap data in memory through a byte vector and returns a CMap which
// can be used for character code to unicode conversion.
func LoadCmapFromData(data []byte) (*CMap, error) {
	cmap := newCMap()
	cmap.cMapParser = newCMapParser(data)

	err := cmap.parse()
	if err != nil {
		return cmap, err
	}

	return cmap, nil
}

// parse parses the CMap file and loads into the CMap structure.
func (cmap *CMap) parse() error {
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}

			common.Log.Debug("Error parsing CMap: %v", err)
			return err
		}

		if op, isOp := o.(cmapOperand); isOp {
			common.Log.Trace("Operand: %s", op.Operand)

			if op.Operand == begincodespacerange {
				err := cmap.parseCodespaceRange()
				if err != nil {
					return err
				}
			} else if op.Operand == beginbfchar {
				err := cmap.parseBfchar()
				if err != nil {
					return err
				}
			} else if op.Operand == beginbfrange {
				err := cmap.parseBfrange()
				if err != nil {
					return err
				}
			} else if op.Operand == begincidrange {
				err := cmap.parseCidrange()
				if err != nil {
					return err
				}
			} else if op.Operand == begincidchar {
				err := cmap.parseCidchar()
				if err != nil {
					return err
				}
			} else if op.Operand == beginnotdefrange {
				err := cmap.parseNotdefrange()
				if err != nil {
					return err
				}
			}
		} else if n, isName := o.(cmapName); isName {
			if n.Name == cmapname {
				o, err := cmap.parseObject()
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
				cmap.name = fmt.Sprintf("%s", o)
			} else if n.Name == cmaptype {
				o, err := cmap.parseObject()
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
				typeInt, ok := o.(cmapInt)
				if !ok {
					return errors.New("CMap type not an integer")
				}
				cmap.ctype = int(typeInt.val)
			}
		} else {
			common.Log.Trace("Unhandled object: %T %#v", o, o)
		}
	}

	return nil
}

// parseCodespaceRange parses the codespace range section of a CMap.
func (cmap *CMap) parseCodespaceRange() error {
	for {
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		hexLow, isHex := o.(cmapHexString)
		if !isHex {
			if op, isOperand := o.(cmapOperand); isOperand {
				if op.Operand == endcodespacerange {
					return nil
				}
				return errors.New("Unexpected operand")
			}
		}

		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		hexHigh, ok := o.(cmapHexString)
		if !ok {
			return errors.New("Non-hex high")
		}

		low := hexToUint64(hexLow)
		high := hexToUint64(hexHigh)

		cspace := codespace{low, high}
		cmap.codespaces = append(cmap.codespaces, cspace)

		cmap.codeSpan = cmap.codeSpan | int8(math.Pow(2.0, float64(len(hexHigh.b))))
	}

	return nil
}

// parseCidchar parses a bfchar section of a CMap file.
func (cmap *CMap) parseCidchar() error {
	for {
		// Src code.
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var srcCode uint64

		switch v := o.(type) {
		case cmapOperand:
			if v.Operand == endcidchar {
				return nil
			}
			return errors.New("Unexpected operand")
		case cmapHexString:
			srcCode = hexToUint64(v)
		default:
			return errors.New("Unexpected type")
		}

		// Target code.
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var toCode string

		switch v := o.(type) {
		case cmapOperand:
			if v.Operand == endbfchar {
				return nil
			}
			return errors.New("Unexpected operand")
		case cmapHexString:
			toCode = hexToString(v)
		case cmapInt:
			if v.val <= int64(0xF) {
				toCode = "000"
				toCode += fmt.Sprintf("%X", v.val)
			} else if v.val <= int64(0xFF) {
				toCode = "00"
				toCode += fmt.Sprintf("%X", v.val)
			} else if v.val <= int64(0xFFF) {
				toCode = "0"
				toCode += fmt.Sprintf("%X", v.val)
			} else {
				toCode = fmt.Sprintf("%X", v.val)
			}
		case cmapName:
			toCode = "?"
			/*if cmap.encoder != nil {
				if r, found := cmap.encoder.GlyphToRune(v.Name); found {
					toCode = string(r)
				}
			}*/
		default:
			return errors.New("Unexpected type")
		}

		cmap.codeMap[srcCode] = toCode
	}

	return nil
}

// parseBfchar parses a bfchar section of a CMap file.
func (cmap *CMap) parseBfchar() error {
	for {
		// Src code.
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var srcCode uint64

		switch v := o.(type) {
		case cmapOperand:
			if v.Operand == endbfchar {
				return nil
			}
			return errors.New("Unexpected operand")
		case cmapHexString:
			srcCode = hexToUint64(v)
		default:
			return errors.New("Unexpected type")
		}

		// Target code.
		o, err = cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var toCode string

		switch v := o.(type) {
		case cmapOperand:
			if v.Operand == endbfchar {
				return nil
			}
			return errors.New("Unexpected operand")
		case cmapHexString:
			toCode = hexToString(v)
		case cmapInt:
			if v.val <= int64(0xFF) {
				toCode = "00"
				toCode += fmt.Sprintf("%X", v.val)
			} else if v.val <= int64(0xFFF) {
				toCode = "0"
				toCode += fmt.Sprintf("%X", v.val)
			} else {
				toCode = fmt.Sprintf("%X", v.val)
			}
		case cmapName:
			toCode = "?"
			/*if cmap.encoder != nil {
				if r, found := cmap.encoder.GlyphToRune(v.Name); found {
					toCode = string(r)
				}
			}*/
		default:
			return errors.New("Unexpected type")
		}

		cmap.codeMap[srcCode] = toCode
	}

	return nil
}

// parseNotdefrange parses a notdefrange section of a CMap file.
func (cmap *CMap) parseNotdefrange() error {
	for {
		// The specifications are in pairs of 3.
		// <srcCodeFrom> <srcCodeTo> <target>
		// where target can be either a uint code.

		// Src code from.
		var srcCodeFrom uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endnotdefrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeFrom = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// Src code to.
		var srcCodeTo uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endbfrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeTo = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// target(s).
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch v := o.(type) {
		case cmapHexString:
			// <srcCodeFrom> <srcCodeTo> <dstCode>, maps [from,to] to [dstCode,dstCode+to-from].
			// in hex format.
			target := hexToUint64(v)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				cmap.codeMap[sc] = string(target)
			}
		case cmapInt:
			target := uint64(v.val)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				if target <= 0xFF {
					hexTocode := "00"
					hexTocode += fmt.Sprintf("%X", target)
					cmap.codeMap[sc] = hexTocode
				} else {
					cmap.codeMap[sc] = fmt.Sprintf("%X", target)
				}
			}
		default:
			return errors.New("Unexpected type")
		}
	}

	return nil
}

// parseCidrange parses a bfrange section of a CMap file.
func (cmap *CMap) parseCidrange() error {
	for {
		// The specifications are in pairs of 3.
		// <srcCodeFrom> <srcCodeTo> <target>
		// where target can be either <destFrom> as a hex code, or a list.

		// Src code from.
		var srcCodeFrom uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endcidrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeFrom = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// Src code to.
		var srcCodeTo uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endbfrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeTo = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// target(s).
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch v := o.(type) {
		case cmapArray:
			sc := srcCodeFrom
			for _, o := range v.Array {
				hexs, ok := o.(cmapHexString)
				if !ok {
					return errors.New("Non-hex string in array")
				}
				cmap.codeMap[sc] = hexToString(hexs)
				sc++
			}
			if sc != srcCodeTo+1 {
				return errors.New("Invalid number of items in array")
			}
		case cmapHexString:
			// <srcCodeFrom> <srcCodeTo> <dstCode>, maps [from,to] to [dstCode,dstCode+to-from].
			// in hex format.
			target := hexToUint64(v)
			i := uint64(0)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				r := target + i
				cmap.codeMap[sc] = string(r)
				i++
			}
		case cmapInt:
			target := uint64(v.val)
			i := uint64(0)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				r := target + i
				if r <= 0xF {
					hexTocode := "000"
					hexTocode += fmt.Sprintf("%X", r)
					cmap.codeMap[sc] = hexTocode
				} else if r <= 0xFF {
					hexTocode := "00"
					hexTocode += fmt.Sprintf("%X", r)
					cmap.codeMap[sc] = hexTocode
				} else if r <= 0xFFF {
					hexTocode := "0"
					hexTocode += fmt.Sprintf("%X", r)
					cmap.codeMap[sc] = hexTocode
				} else {
					cmap.codeMap[sc] = fmt.Sprintf("%X", r)
				}
				i++
			}
		default:
			return errors.New("Unexpected type")
		}
	}

	return nil
}

// parseBfrange parses a bfrange section of a CMap file.
func (cmap *CMap) parseBfrange() error {
	for {
		// The specifications are in pairs of 3.
		// <srcCodeFrom> <srcCodeTo> <target>
		// where target can be either <destFrom> as a hex code, or a list.

		// Src code from.
		var srcCodeFrom uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endbfrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeFrom = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// Src code to.
		var srcCodeTo uint64
		{
			o, err := cmap.parseObject()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			switch v := o.(type) {
			case cmapOperand:
				if v.Operand == endbfrange {
					return nil
				}
				return errors.New("Unexpected operand")
			case cmapHexString:
				srcCodeTo = hexToUint64(v)
			default:
				return errors.New("Unexpected type")
			}
		}

		// target(s).
		o, err := cmap.parseObject()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch v := o.(type) {
		case cmapArray:
			sc := srcCodeFrom
			for _, o := range v.Array {
				hexs, ok := o.(cmapHexString)
				if !ok {
					return errors.New("Non-hex string in array")
				}
				cmap.codeMap[sc] = hexToString(hexs)
				sc++
			}
			if sc != srcCodeTo+1 {
				return errors.New("Invalid number of items in array")
			}
		case cmapHexString:
			// <srcCodeFrom> <srcCodeTo> <dstCode>, maps [from,to] to [dstCode,dstCode+to-from].
			// in hex format.
			target := hexToUint64(v)
			i := uint64(0)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				r := target + i
				cmap.codeMap[sc] = string(r)
				i++
			}
		case cmapInt:
			target := uint64(v.val)
			i := uint64(0)
			for sc := srcCodeFrom; sc <= srcCodeTo; sc++ {
				r := target + i
				if r <= 0xFF {
					hexTocode := "00"
					hexTocode += fmt.Sprintf("%X", r)
					cmap.codeMap[sc] = hexTocode
				} else if r <= 0xFFF {
					hexTocode := "0"
					hexTocode += fmt.Sprintf("%X", r)
					cmap.codeMap[sc] = hexTocode
				} else {
					cmap.codeMap[sc] = fmt.Sprintf("%X", r)
				}
				i++
			}
		default:
			return errors.New("Unexpected type")
		}
	}

	return nil
}
