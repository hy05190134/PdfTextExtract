/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package contentstream

import (
	"bytes"
	"fmt"

	"../common"
	"../core"
)

// A representation of an inline image in a Content stream. Everything between the BI and EI operands.
// ContentStreamInlineImage implements the core.PdfObject interface although strictly it is not a PDF object.
type ContentStreamInlineImage struct {
	BitsPerComponent core.PdfObject
	ColorSpace       core.PdfObject
	Decode           core.PdfObject
	DecodeParms      core.PdfObject
	Filter           core.PdfObject
	Height           core.PdfObject
	ImageMask        core.PdfObject
	Intent           core.PdfObject
	Interpolate      core.PdfObject
	Width            core.PdfObject
	stream           []byte
}

func (this *ContentStreamInlineImage) String() string {
	s := fmt.Sprintf("InlineImage(len=%d)\n", len(this.stream))
	if this.BitsPerComponent != nil {
		s += "- BPC " + this.BitsPerComponent.DefaultWriteString() + "\n"
	}
	if this.ColorSpace != nil {
		s += "- CS " + this.ColorSpace.DefaultWriteString() + "\n"
	}
	if this.Decode != nil {
		s += "- D " + this.Decode.DefaultWriteString() + "\n"
	}
	if this.DecodeParms != nil {
		s += "- DP " + this.DecodeParms.DefaultWriteString() + "\n"
	}
	if this.Filter != nil {
		s += "- F " + this.Filter.DefaultWriteString() + "\n"
	}
	if this.Height != nil {
		s += "- H " + this.Height.DefaultWriteString() + "\n"
	}
	if this.ImageMask != nil {
		s += "- IM " + this.ImageMask.DefaultWriteString() + "\n"
	}
	if this.Intent != nil {
		s += "- Intent " + this.Intent.DefaultWriteString() + "\n"
	}
	if this.Interpolate != nil {
		s += "- I " + this.Interpolate.DefaultWriteString() + "\n"
	}
	if this.Width != nil {
		s += "- W " + this.Width.DefaultWriteString() + "\n"
	}
	return s
}

func (this *ContentStreamInlineImage) DefaultWriteString() string {
	var output bytes.Buffer

	// We do not start with "BI" as that is the operand and is written out separately.
	// Write out the parameters
	s := ""

	if this.BitsPerComponent != nil {
		s += "/BPC " + this.BitsPerComponent.DefaultWriteString() + "\n"
	}
	if this.ColorSpace != nil {
		s += "/CS " + this.ColorSpace.DefaultWriteString() + "\n"
	}
	if this.Decode != nil {
		s += "/D " + this.Decode.DefaultWriteString() + "\n"
	}
	if this.DecodeParms != nil {
		s += "/DP " + this.DecodeParms.DefaultWriteString() + "\n"
	}
	if this.Filter != nil {
		s += "/F " + this.Filter.DefaultWriteString() + "\n"
	}
	if this.Height != nil {
		s += "/H " + this.Height.DefaultWriteString() + "\n"
	}
	if this.ImageMask != nil {
		s += "/IM " + this.ImageMask.DefaultWriteString() + "\n"
	}
	if this.Intent != nil {
		s += "/Intent " + this.Intent.DefaultWriteString() + "\n"
	}
	if this.Interpolate != nil {
		s += "/I " + this.Interpolate.DefaultWriteString() + "\n"
	}
	if this.Width != nil {
		s += "/W " + this.Width.DefaultWriteString() + "\n"
	}
	output.WriteString(s)

	output.WriteString("ID ")
	output.Write(this.stream)
	output.WriteString("\nEI\n")

	return output.String()
}

// Parse an inline image from a content stream, both read its properties and binary data.
// When called, "BI" has already been read from the stream.  This function
// finishes reading through "EI" and then returns the ContentStreamInlineImage.
func (this *ContentStreamParser) ParseInlineImage() (*ContentStreamInlineImage, error) {
	// Reading parameters.
	im := ContentStreamInlineImage{}

	for {
		this.skipSpaces()
		obj, err, isOperand := this.parseObject()
		if err != nil {
			return nil, err
		}

		if !isOperand {
			// Not an operand.. Read key value properties..
			param, ok := obj.(*core.PdfObjectName)
			if !ok {
				common.Log.Debug("Invalid inline image property (expecting name) - %T", obj)
				return nil, fmt.Errorf("Invalid inline image property (expecting name) - %T", obj)
			}

			valueObj, err, isOperand := this.parseObject()
			if err != nil {
				return nil, err
			}
			if isOperand {
				return nil, fmt.Errorf("Not expecting an operand")
			}

			if *param == "BPC" || *param == "BitsPerComponent" {
				im.BitsPerComponent = valueObj
			} else if *param == "CS" || *param == "ColorSpace" {
				im.ColorSpace = valueObj
			} else if *param == "D" || *param == "Decode" {
				im.Decode = valueObj
			} else if *param == "DP" || *param == "DecodeParms" {
				im.DecodeParms = valueObj
			} else if *param == "F" || *param == "Filter" {
				im.Filter = valueObj
			} else if *param == "H" || *param == "Height" {
				im.Height = valueObj
			} else if *param == "IM" {
				im.ImageMask = valueObj
			} else if *param == "Intent" {
				im.Intent = valueObj
			} else if *param == "I" {
				im.Interpolate = valueObj
			} else if *param == "W" || *param == "Width" {
				im.Width = valueObj
			} else {
				return nil, fmt.Errorf("Unknown inline image parameter %s", *param)
			}
		}

		if isOperand {
			operand, ok := obj.(*core.PdfObjectString)
			if !ok {
				return nil, fmt.Errorf("Failed to read inline image - invalid operand")
			}

			if *operand == "EI" {
				// Image fully defined
				common.Log.Trace("Inline image finished...")
				return &im, nil
			} else if *operand == "ID" {
				// Inline image data.
				// Should get a single space (0x20) followed by the data and then EI.
				common.Log.Trace("ID start")

				// Skip the space if its there.
				b, err := this.reader.Peek(1)
				if err != nil {
					return nil, err
				}
				if core.IsWhiteSpace(b[0]) {
					this.reader.Discard(1)
				}

				// Unfortunately there is no good way to know how many bytes to read since it
				// depends on the Filter and encoding etc.
				// Therefore we will simply read until we find "<ws>EI<ws>" where <ws> is whitespace
				// although of course that could be a part of the data (even if unlikely).
				im.stream = []byte{}
				state := 0
				var skipBytes []byte
				for {
					c, err := this.reader.ReadByte()
					if err != nil {
						common.Log.Debug("Unable to find end of image EI in inline image data")
						return nil, err
					}

					if state == 0 {
						if core.IsWhiteSpace(c) {
							skipBytes = []byte{}
							skipBytes = append(skipBytes, c)
							state = 1
						} else {
							im.stream = append(im.stream, c)
						}
					} else if state == 1 {
						skipBytes = append(skipBytes, c)
						if c == 'E' {
							state = 2
						} else {
							im.stream = append(im.stream, skipBytes...)
							skipBytes = []byte{} // Clear.
							// Need an extra check to decide if we fall back to state 0 or 1.
							if core.IsWhiteSpace(c) {
								state = 1
							} else {
								state = 0
							}
						}
					} else if state == 2 {
						skipBytes = append(skipBytes, c)
						if c == 'I' {
							state = 3
						} else {
							im.stream = append(im.stream, skipBytes...)
							skipBytes = []byte{} // Clear.
							state = 0
						}
					} else if state == 3 {
						skipBytes = append(skipBytes, c)
						if core.IsWhiteSpace(c) {
							// image data finished.
							if len(im.stream) > 100 {
								common.Log.Trace("Image stream (%d): % x ...", len(im.stream), im.stream[:100])
							} else {
								common.Log.Trace("Image stream (%d): % x", len(im.stream), im.stream)
							}
							// Exit point.
							return &im, nil
						} else {
							// Seems like "<ws>EI" was part of the data.
							im.stream = append(im.stream, skipBytes...)
							skipBytes = []byte{} // Clear.
							state = 0
						}
					}
				}
				// Never reached (exit point is at end of EI).
			}
		}
	}
}
