/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"bytes"
	"errors"
	"fmt"

	"../cmap"
	"../common"
	"../contentstream"
	"../core"
	"../model"
)

// ExtractText processes and extracts all text data in content streams and returns as a string. Takes into
// account character encoding via CMaps in the PDF file.
// The text is processed linearly e.g. in the order in which it appears. A best effort is done to add
// spaces and newlines.
func (e *Extractor) ExtractText() (string, error) {
	var buf bytes.Buffer

	cstreamParser := contentstream.NewContentStreamParser(e.contents)
	operations, err := cstreamParser.Parse()
	if err != nil {
		return buf.String(), err
	}

	processor := contentstream.NewContentStreamProcessor(*operations)

	var codemap *cmap.CMap
	var cidCodemap *cmap.CMap
	var font *model.Font
	inText := false
	xPos, yPos := float64(-1), float64(-1)

	preRect0, preRect1, preRect2, preRect3 := float64(-1), float64(-1), float64(-1), float64(-1)
	rect0, rect1, rect2, rect3 := float64(-1), float64(-1), float64(-1), float64(-1)

	processor.AddHandler(contentstream.HandlerConditionEnumAllOperands, "",
		func(op *contentstream.ContentStreamOperation, f model.FontsByNames) error {
			operand := op.Operand
			switch operand {
			case "re":
				if inText {
					common.Log.Debug("re operand outside text")
					return nil
				}
				if len(op.Params) != 4 {
					common.Log.Debug("Error re should only get 4 input params, got %d", len(op.Params))
					return errors.New("Incorrect parameter count")
				}

				rect0, err = core.GetNumberAsFloat(op.Params[0])
				if err != nil {
					common.Log.Debug("re Float parse error")
					return nil
				}
				rect1, err = core.GetNumberAsFloat(op.Params[1])
				if err != nil {
					common.Log.Debug("re Float parse error")
					return nil
				}
				rect2, err = core.GetNumberAsFloat(op.Params[2])
				if err != nil {
					common.Log.Debug("re Float parse error")
					return nil
				}
				rect3, err = core.GetNumberAsFloat(op.Params[3])
				if err != nil {
					common.Log.Debug("re Float parse error")
					return nil
				}
			case "BT":
				inText = true
			case "ET":
				inText = false
				preRect0 = rect0
				preRect1 = rect1
				preRect2 = rect2
				preRect3 = rect3
			case "Tf":
				if !inText {
					common.Log.Debug("Tf operand outside text")
					return nil
				}

				if len(op.Params) != 2 {
					common.Log.Debug("Error Tf should only get 2 input params, got %d", len(op.Params))
					return errors.New("Incorrect parameter count")
				}

				fontName, ok := op.Params[0].(*core.PdfObjectName)
				if !ok {
					common.Log.Debug("Error Tf font input not a name, %s", op.Params[0])
					return errors.New("Tf range error")
				}

				common.Log.Trace("fontName: %s", fontName)

				font = nil
				codemap = nil
				cidCodemap = nil
				if font, ok = f[core.PdfObjectName(*fontName)]; ok {
					codemap = font.GetCmap()
					cidCodemap = font.GetCidCmap()
				} else {
					common.Log.Debug("Error: can't find Tf font by name")
					return errors.New("can't find Tf font by name")
				}
			case "T*":
				if !inText {
					common.Log.Debug("T* operand outside text")
					return nil
				}
				if rect0 != preRect0 || rect1 != preRect1 || rect2 != preRect2 || rect3 != preRect3 {
					buf.WriteString("\n")
				}
			case "'":
				//quote = T* + Tj
				if !inText {
					common.Log.Debug("quote operand outside text")
					return nil
				}
				if rect0 != preRect0 || rect1 != preRect1 || rect2 != preRect2 || rect3 != preRect3 {
					buf.WriteString("\n")
				}
				if len(op.Params) < 1 {
					return nil
				}
				param, ok := op.Params[0].(*core.PdfObjectString)
				if !ok {
					return fmt.Errorf("Invalid parameter type, not string (%T)", op.Params[0])
				}

				//first change charcode to cid string
				if font != nil && font.GetmPredefinedCmap() && cidCodemap != nil {
					str := cidCodemap.CharcodeBytesToCidStr([]byte(*param))
					param = core.MakeString(str)
				}

				if codemap != nil {
					if font.GetSimpleEncodingTableFlag() {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), font.GetSimpleEncodingTable(), true))
					} else {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), []uint{}, false))
					}
				} else {
					if font != nil && font.GetSimpleEncodingTableFlag() {
						for _, cid := range []byte(*param) {
							r := cmap.Utf8CodepointToUtf8(font.GetSimpleEncodingTable()[cid])
							buf.WriteString(r)
						}
					} else {
						buf.WriteString(string(*param))
					}
				}
			case "\"":
				//quote = T* + ac + aw + Tj
				if !inText {
					common.Log.Debug("double quote operand outside text")
					return nil
				}
				if rect0 != preRect0 || rect1 != preRect1 || rect2 != preRect2 || rect3 != preRect3 {
					buf.WriteString("\n")
				}
				if len(op.Params) < 1 {
					return nil
				}
				param, ok := op.Params[2].(*core.PdfObjectString)
				if !ok {
					return fmt.Errorf("Invalid parameter type, not string (%T)", op.Params[2])
				}

				//first change charcode to cid string
				if font != nil && font.GetmPredefinedCmap() && cidCodemap != nil {
					str := cidCodemap.CharcodeBytesToCidStr([]byte(*param))
					param = core.MakeString(str)
				}

				if codemap != nil {
					if font.GetSimpleEncodingTableFlag() {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), font.GetSimpleEncodingTable(), true))
					} else {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), []uint{}, false))
					}
				} else {
					if font != nil && font.GetSimpleEncodingTableFlag() {
						for _, cid := range []byte(*param) {
							r := cmap.Utf8CodepointToUtf8(font.GetSimpleEncodingTable()[cid])
							buf.WriteString(r)
						}
					} else {
						buf.WriteString(string(*param))
					}
				}
			case "Td", "TD":
				if !inText {
					common.Log.Debug("Td/TD operand outside text")
					return nil
				}

				// Params: [tx ty], corresponeds to Tm=Tlm=[1 0 0;0 1 0;tx ty 1]*Tm
				if len(op.Params) != 2 {
					common.Log.Debug("Td/TD invalid arguments")
					return nil
				}
				tx, err := core.GetNumberAsFloat(op.Params[0])
				if err != nil {
					common.Log.Debug("Td Float parse error")
					return nil
				}
				ty, err := core.GetNumberAsFloat(op.Params[1])
				if err != nil {
					common.Log.Debug("Td Float parse error")
					return nil
				}

				if tx > 0 {
					//buf.WriteString(" ")
				}
				if ty < 0 {
					// TODO: More flexible space characters?
					if rect0 != preRect0 || rect1 != preRect1 || rect2 != preRect2 || rect3 != preRect3 {
						buf.WriteString("\n")
					}
				}
			case "Tm":
				if !inText {
					common.Log.Debug("Tm operand outside text")
					return nil
				}

				// Params: a,b,c,d,e,f as in Tm = [a b 0; c d 0; e f 1].
				// The last two (e,f) represent translation.
				if len(op.Params) != 6 {
					return errors.New("Tm: Invalid number of inputs")
				}
				xfloat, ok := op.Params[4].(*core.PdfObjectFloat)
				if !ok {
					xint, ok := op.Params[4].(*core.PdfObjectInteger)
					if !ok {
						return nil
					}
					xfloat = core.MakeFloat(float64(*xint))
				}
				yfloat, ok := op.Params[5].(*core.PdfObjectFloat)
				if !ok {
					yint, ok := op.Params[5].(*core.PdfObjectInteger)
					if !ok {
						return nil
					}
					yfloat = core.MakeFloat(float64(*yint))
				}

				if yPos == -1 {
					yPos = float64(*yfloat)
				} else if yPos > float64(*yfloat) {
					if rect0 != preRect0 || rect1 != preRect1 || rect2 != preRect2 || rect3 != preRect3 {
						buf.WriteString("\n")
					}
					xPos = float64(*xfloat)
					yPos = float64(*yfloat)
					return nil
				} else {
					yPos = float64(*yfloat)
				}

				if xPos == -1 {
					xPos = float64(*xfloat)
				} else if xPos < float64(*xfloat) {
					buf.WriteString("\t")
					xPos = float64(*xfloat)
				}
			case "TJ":
				if !inText {
					common.Log.Debug("TJ operand outside text")
					return nil
				}
				if len(op.Params) < 1 {
					return nil
				}
				paramList, ok := op.Params[0].(*core.PdfObjectArray)
				if !ok {
					return fmt.Errorf("Invalid parameter type, no array (%T)", op.Params[0])
				}
				for _, obj := range *paramList {
					switch v := obj.(type) {
					case *core.PdfObjectString:
						//first change charcode to cid string
						if font != nil && font.GetmPredefinedCmap() && cidCodemap != nil {
							str := cidCodemap.CharcodeBytesToCidStr([]byte(*v))
							v = core.MakeString(str)
						}

						//common.Log.Debug("origin: %X", []byte(*v))

						// has ToUnicode
						if codemap != nil {
							//common.Log.Debug("parsed str: %s", codemap.CharcodeBytesToUnicode([]byte(*v), []uint{}, false))
							if font.GetSimpleEncodingTableFlag() {
								buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*v), font.GetSimpleEncodingTable(), true))
							} else {
								buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*v), []uint{}, false))
							}
						} else {
							//no ToUnicode but has font encoding
							if font != nil && font.GetSimpleEncodingTableFlag() {
								for _, cid := range []byte(*v) {
									r := cmap.Utf8CodepointToUtf8(font.GetSimpleEncodingTable()[cid])
									buf.WriteString(r)
								}
							} else {
								buf.WriteString(string(*v))
							}
						}
					case *core.PdfObjectFloat:
						if *v < -100 {
							//buf.WriteString(" ")
						}
					case *core.PdfObjectInteger:
						if *v < -100 {
							//buf.WriteString(" ")
						}
					}
				}
			case "Tj":
				if !inText {
					common.Log.Debug("Tj operand outside text")
					return nil
				}
				if len(op.Params) < 1 {
					return nil
				}
				param, ok := op.Params[0].(*core.PdfObjectString)
				if !ok {
					return fmt.Errorf("Invalid parameter type, not string (%T)", op.Params[0])
				}

				//first change charcode to cid string
				if font != nil && font.GetmPredefinedCmap() && cidCodemap != nil {
					str := cidCodemap.CharcodeBytesToCidStr([]byte(*param))
					param = core.MakeString(str)
				}

				//common.Log.Debug("origin: %X", []byte(*param))

				if codemap != nil {
					//common.Log.Debug("parsed str: %s", codemap.CharcodeBytesToUnicode([]byte(*param), []uint{}, false))
					if font.GetSimpleEncodingTableFlag() {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), font.GetSimpleEncodingTable(), true))
					} else {
						buf.WriteString(codemap.CharcodeBytesToUnicode([]byte(*param), []uint{}, false))
					}
				} else {
					if font != nil && font.GetSimpleEncodingTableFlag() {
						for _, cid := range []byte(*param) {
							r := cmap.Utf8CodepointToUtf8(font.GetSimpleEncodingTable()[cid])
							buf.WriteString(r)
						}
					} else {
						buf.WriteString(string(*param))
					}
				}
			}

			return nil
		})

	err = processor.Process(e.fontNamesMap)
	if err != nil {
		common.Log.Error("Error processing: %v", err)
		return buf.String(), err
	}

	//procBuf(&buf)

	return buf.String(), nil
}
