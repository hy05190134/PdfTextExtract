package core

import (
	"../common"
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var rePdfVersion = regexp.MustCompile(`%PDF-(\d)\.(\d)`)
var reStartXref = regexp.MustCompile(`startx?ref\s*(\d+)`)
var reXrefSubsection = regexp.MustCompile(`(\d+)\s+(\d+)$`)
var reXrefEntry = regexp.MustCompile(`(\d+)\s+(\d+)\s+([nf])$`)
var reReference = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+R`)
var reNumeric = regexp.MustCompile(`^[\+-.]*([0-9.]+)`)
var reExponential = regexp.MustCompile(`^[\+-.]*([0-9.]+)e[\+-.]*([0-9.]+)`)
var reIndirectObject = regexp.MustCompile(`(\d+)\s+(\d+)\s+obj`)

type PdfParser struct {
	majorVersion int
	minorVersion int

	rs io.ReadSeeker

	reader *bufio.Reader

	//store referenceData
	xrefs XrefTable

	//trailer dict
	trailerDict *PdfObjectDictionary

	getRoot bool

	getInfo bool

	//root dict
	rootDict *PdfObjectDictionary

	//info dict
	infoDict *PdfObjectDictionary

	crypter *PdfCrypt

	// Tracker for reference lookups when looking up Length entry of stream objects.
	// The Length entries of stream objects are a special case, as they can require recursive parsing, i.e. look up
	// the length reference (if not object) prior to reading the actual stream.  This has risks of endless looping.
	// Tracking is necessary to avoid recursive loops.
	streamLengthReferenceLookupInProgress map[int64]bool

	ObjCache ObjectCache // TODO: Unexport (v3).

	objstms ObjectStreams
}

// Skip over comments and spaces. Can handle multi-line comments.
func (parser *PdfParser) skipComments() error {
	if _, err := parser.skipSpaces(); err != nil {
		return err
	}

	isFirst := true
	for {
		bb, err := parser.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if isFirst && bb[0] != '%' {
			// Not a comment clearly.
			return nil
		} else {
			isFirst = false
		}
		if (bb[0] != '\r') && (bb[0] != '\n') {
			parser.reader.ReadByte()
		} else {
			break
		}
	}

	// Call recursively to handle multiline comments.
	return parser.skipComments()
}

// Skip over any spaces.
func (parser *PdfParser) skipSpaces() (int, error) {
	cnt := 0
	for {
		b, err := parser.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if IsWhiteSpace(b) {
			cnt++
		} else {
			parser.reader.UnreadByte()
			break
		}
	}

	return cnt, nil
}

// Return the closest object following offset from the xrefs table.
func (parser *PdfParser) xrefNextObjectOffset(offset int64) int64 {
	nextOffset := int64(0)
	for _, xref := range parser.xrefs {
		if xref.offset > offset && (xref.offset < nextOffset || nextOffset == 0) {
			nextOffset = xref.offset
		}
	}
	return nextOffset
}

// Get stream length, avoiding recursive loops.
// The input is the PdfObject that is to be traced to a direct object.
func (parser *PdfParser) traceStreamLength(lengthObj PdfObject) (PdfObject, error) {
	lengthRef, isRef := lengthObj.(*PdfObjectReference)
	if isRef {
		lookupInProgress, has := parser.streamLengthReferenceLookupInProgress[lengthRef.ObjectNumber]
		if has && lookupInProgress {
			common.Log.Debug("Stream Length reference unresolved (illegal)")
			return nil, errors.New("Illegal recursive loop")
		}
		// Mark lookup as in progress.
		parser.streamLengthReferenceLookupInProgress[lengthRef.ObjectNumber] = true
	}

	slo, err := parser.Trace(lengthObj)
	if err != nil {
		return nil, err
	}
	common.Log.Trace("Stream length: %s", slo)

	if isRef {
		// Mark as completed lookup
		parser.streamLengthReferenceLookupInProgress[lengthRef.ObjectNumber] = false
	}

	return slo, nil
}

// Parse an indirect object from the input stream. Can also be an object stream.
// Returns the indirect object (*PdfIndirectObject) or the stream object (*PdfObjectStream).
// TODO: Unexport (v3).
func (parser *PdfParser) ParseIndirectObject() (PdfObject, error) {
	indirect := PdfIndirectObject{}
	common.Log.Trace("Reading indirect obj")

	bb, err := parser.reader.Peek(20)
	if err != nil {
		common.Log.Debug("ERROR: Fail to read indirect obj")
		return &indirect, err
	}
	common.Log.Trace("indirect obj peek \"%s\"", string(bb))

	indices := reIndirectObject.FindStringSubmatchIndex(string(bb))
	if len(indices) < 6 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(bb))
		return &indirect, errors.New("Unable to detect indirect object signature")
	}
	parser.reader.Discard(indices[0]) // Take care of any small offset.
	common.Log.Trace("Offsets % d", indices)

	// Read the object header.
	hlen := indices[1] - indices[0]
	hb := make([]byte, hlen)
	_, err = io.ReadAtLeast(parser.reader, hb, hlen)
	if err != nil {
		common.Log.Debug("ERROR: unable to read - %s", err)
		return nil, err
	}
	common.Log.Trace("textline: %s", hb)

	result := reIndirectObject.FindStringSubmatch(string(hb))
	if len(result) < 3 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(hb))
		return &indirect, errors.New("Unable to detect indirect object signature")
	}

	on, _ := strconv.ParseInt(result[1], 10, 64)
	gn, _ := strconv.ParseInt(result[2], 10, 64)
	indirect.ObjectNumber = on
	indirect.GenerationNumber = gn

	for {
		ch, err := parser.reader.ReadByte()
		if err != nil {
			return &indirect, err
		}

		if IsWhiteSpace(ch) {
			continue
		}

		switch ch {
		case '%':
			{
				parser.reader.UnreadByte()
				parser.skipComments()
			}
		case '/', '(', '[', '<', 'n', 'f', 't':
			{
				parser.reader.UnreadByte()
				indirect.PdfObject, err = parser.parseObject()
				if err != nil {
					return &indirect, err
				}
				common.Log.Trace("Parsed inner object ... finished.")
			}
		case 'e':
			{
				line, err := parser.reader.ReadString('j')
				if err != nil {
					return &indirect, err
				}

				line = strings.TrimSpace(line)
				if line == "ndobj" {
					common.Log.Trace("Returning indirect!")
					return &indirect, nil
				}
			}
		case 's':
			{
				//stream
				bb := make([]byte, 5)
				n, err := parser.reader.Read(bb)
				if err != nil {
					return nil, err
				}
				common.Log.Trace("should read 5, actual read: %d", n)
				if string(bb[:5]) == "tream" {
					//it will skip the real byte when use skipspaces() and it will cause decrypt or decode fail
					parser.reader.ReadString('\n')
					dict, ok := indirect.PdfObject.(*PdfObjectDictionary)
					if !ok {
						return nil, errors.New("Stream object missing dictionary")
					}
					common.Log.Trace("Stream dict %s", dict)

					// Special stream length tracing function used to avoid endless recursive looping.
					slo, err := parser.traceStreamLength(dict.Get("Length"))
					if err != nil {
						common.Log.Debug("Fail to trace stream length: %v", err)
						return nil, err
					}
					common.Log.Trace("Stream length: %s", slo)

					pstreamLength, ok := slo.(*PdfObjectInteger)
					if !ok {
						return nil, errors.New("Stream length needs to be an integer")
					}
					streamLength := *pstreamLength
					if streamLength < 0 {
						return nil, errors.New("Stream needs to be longer than 0")
					}

					//TODO: we can delete the logic for effective
					// Validate the stream length based on the cross references.
					// Find next object with closest offset to current object and calculate
					// the expected stream length based on that.
					streamStartOffset := parser.GetFileOffset()
					nextObjectOffset := parser.xrefNextObjectOffset(streamStartOffset)

					if streamStartOffset+int64(streamLength) > nextObjectOffset && nextObjectOffset > streamStartOffset {
						common.Log.Debug("Expected ending at %d", streamStartOffset+int64(streamLength))
						common.Log.Debug("Next object starting at %d", nextObjectOffset)
						// endstream + "\n" endobj + "\n" (17)
						newLength := nextObjectOffset - streamStartOffset - 17
						if newLength < 0 {
							return nil, errors.New("Invalid stream length, going past boundaries")
						}

						common.Log.Debug("Attempting a length correction to %d...", newLength)
						streamLength = PdfObjectInteger(newLength)
						dict.Set("Length", &streamLength)
					}

					common.Log.Trace("stream length: %d", streamLength)

					stream := make([]byte, streamLength)
					_, err = parser.ReadAtLeast(stream, int(streamLength))
					if err != nil {
						common.Log.Debug("Error stream (%d): %X, err: %v", len(stream), stream, err)
						return nil, err
					}

					streamobj := PdfObjectStream{}
					streamobj.Stream = stream
					streamobj.PdfObjectDictionary = indirect.PdfObject.(*PdfObjectDictionary)
					streamobj.ObjectNumber = indirect.ObjectNumber
					streamobj.GenerationNumber = indirect.GenerationNumber

					parser.skipSpaces()
					parser.reader.Discard(9) // endstream
					parser.skipSpaces()
					return &streamobj, nil
				} else {
					common.Log.Debug("Error: wrong object with s start")
					return &indirect, errors.New("wrong object with s start")
				}
			}
		default:
			{
				parser.reader.UnreadByte()
				indirect.PdfObject, err = parser.parseObject()
				return &indirect, err
			}
		}
	}

	return &indirect, nil
}

//read compressed xref table
func (parser *PdfParser) readXrefStream(xs *PdfObjectStream) error {
	sizeObj, ok := xs.PdfObjectDictionary.Get("Size").(*PdfObjectInteger)
	if !ok {
		common.Log.Debug("Error: missing Size from xref stm")
		return errors.New("missing Size from xref stm")
	}

	// Sanity check to avoid DoS attacks. Maximum number of indirect objects on 32 bit system.
	if int64(*sizeObj) > 8388607 {
		common.Log.Debug("Error: xref Size exceeded limit, over 8388607 (%d)", *sizeObj)
		return errors.New("range check error")
	}

	wObj := xs.PdfObjectDictionary.Get("W")
	wArr, ok := wObj.(*PdfObjectArray)
	if !ok {
		return errors.New("invalid W in xref stream")
	}

	wLen := len(*wArr)
	if wLen != 3 {
		common.Log.Debug("Error: unsupported xref stm (len(W) != 3 - %d)", wLen)
		return errors.New("unsupported xref stm len(W) != 3")
	}

	// get b0 b1 b2
	var b []int64
	for i := 0; i < 3; i++ {
		w, ok := (*wArr)[i].(PdfObject)
		if !ok {
			return errors.New("invalid W")
		}
		wVal, ok := w.(*PdfObjectInteger)
		if !ok {
			return errors.New("invalid w integer object type")
		}

		b = append(b, int64(*wVal))
	}

	ds, err := DecodeStream(xs)
	if err != nil {
		common.Log.Debug("ERROR: Unable to decode stream: %v", err)
		return err
	}

	s0 := int(b[0])
	s1 := int(b[0] + b[1])
	s2 := int(b[0] + b[1] + b[2])
	deltab := int(b[0] + b[1] + b[2])

	if s0 < 0 || s1 < 0 || s2 < 0 {
		common.Log.Debug("Error W value < 0 (%d,%d,%d)", s0, s1, s2)
		return errors.New("Range check error")
	}
	if deltab == 0 {
		common.Log.Debug("No xref objects in stream (deltab == 0)")
		return nil
	}

	// Calculate expected entries.
	dsLen := len(ds)
	entries := dsLen / deltab

	// Get the object indices.
	objCount := 0
	indexObj := xs.PdfObjectDictionary.Get("Index")
	// Table 17 (7.5.8.2 Cross-Reference Stream Dictionary)
	// (Optional) An array containing a pair of integers for each
	// subsection in this section. The first integer shall be the first
	// object number in the subsection; the second integer shall be the
	// number of entries in the subsection.
	// The array shall be sorted in ascending order by object number.
	// Subsections cannot overlap; an object number may have at most
	// one entry in a section.
	// Default value: [0 Size].
	indexList := []int{}
	if indexObj != nil {
		common.Log.Trace("Index: %b", indexObj)
		indicesArray, ok := indexObj.(*PdfObjectArray)
		if !ok {
			common.Log.Debug("Invalid Index object (should be an array)")
			return errors.New("Invalid Index object")
		}

		// Expect indLen to be a multiple of 2.
		if len(*indicesArray)%2 != 0 {
			common.Log.Debug("Warning: failed to load xref stm index not multiple of 2.")
			return errors.New("Range check error")
		}

		objCount = 0

		indices, err := indicesArray.ToIntegerArray()
		if err != nil {
			common.Log.Debug("Error getting index array as integers: %v", err)
			return err
		}

		for i := 0; i < len(indices); i += 2 {
			// add the indices to the list..

			startIdx := indices[i]
			numObjs := indices[i+1]
			for j := 0; j < numObjs; j++ {
				indexList = append(indexList, startIdx+j)
			}
			objCount += numObjs
		}
	} else {
		// If no Index, then assume [0 Size]
		for i := 0; i < int(*sizeObj); i++ {
			indexList = append(indexList, i)
		}
		objCount = int(*sizeObj)
	}

	//maybe no index obj
	if entries == objCount+1 {
		// For compatibility, expand the object count.
		common.Log.Debug("BAD file: allowing compatibility (append one object to xref stm)")
		indexList = append(indexList, objCount)
		objCount++
	}

	if entries != objCount {
		// If mismatch -> error (already allowing mismatch of 1 if Index not specified).
		common.Log.Debug("ERROR: xref stm: num entries != len(indices) (%d != %d)", entries, objCount)
		return errors.New("Xref stm num entries != len(indices)")
	}

	common.Log.Trace("Objects count %d, Indices: % d", objCount, indexList)

	// Convert byte array to a larger integer, little-endian.
	convertBytes := func(v []byte) int64 {
		var tmp int64 = 0
		for i := 0; i < len(v); i++ {
			tmp += int64(v[i]) * (1 << uint(8*(len(v)-i-1)))
		}
		return tmp
	}

	common.Log.Trace("Decoded stream length: %d", len(ds))
	objIndex := 0
	for i := 0; i < dsLen; i += deltab {
		err := checkBounds(dsLen, i, i+s0)
		if err != nil {
			common.Log.Debug("Invalid slice range: %v", err)
			return err
		}
		p1 := ds[i : i+s0]

		err = checkBounds(dsLen, i+s0, i+s1)
		if err != nil {
			common.Log.Debug("Invalid slice range: %v", err)
			return err
		}
		p2 := ds[i+s0 : i+s1]

		err = checkBounds(dsLen, i+s1, i+s2)
		if err != nil {
			common.Log.Debug("Invalid slice range: %v", err)
			return err
		}
		p3 := ds[i+s1 : i+s2]

		ftype := convertBytes(p1)
		n2 := convertBytes(p2)
		n3 := convertBytes(p3)

		if b[0] == 0 {
			// If first entry in W is 0, then default to to type 1.
			// (uncompressed object via offset).
			ftype = 1
		}

		if objIndex >= objCount {
			common.Log.Debug("XRef stream - Trying to access index out of bounds - breaking")
			break
		}
		objNum := indexList[objIndex]
		objIndex++

		common.Log.Trace("%d. p1: % x", objNum, p1)
		common.Log.Trace("%d. p2: % x", objNum, p2)
		common.Log.Trace("%d. p3: % x", objNum, p3)

		common.Log.Trace("%d. xref: %d %d %d", objNum, ftype, n2, n3)
		if ftype == 0 {
			common.Log.Trace("- Free object - can probably ignore")
		} else if ftype == 1 {
			common.Log.Trace("- In use - uncompressed via offset %b", p2)
			// Object type 1: Objects that are in use but are not
			// compressed, i.e. defined by an offset (normal entry)
			if xr, ok := parser.xrefs[objNum]; !ok || int(n3) > xr.generation {
				// Only overload if not already loaded!
				// or has a newer generation number. (should not happen)
				obj := XrefObject{objectNumber: objNum,
					xtype: XREF_TABLE_ENTRY, offset: n2, generation: int(n3)}
				parser.xrefs[objNum] = obj
			}
		} else if ftype == 2 {
			// Object type 2: Compressed object.
			common.Log.Trace("- In use - compressed object")
			if _, ok := parser.xrefs[objNum]; !ok {
				obj := XrefObject{objectNumber: objNum,
					xtype: XREF_OBJECT_STREAM, osObjNumber: int(n2), osObjIndex: int(n3)}
				parser.xrefs[objNum] = obj
				common.Log.Trace("entry: %s", parser.xrefs[objNum])
			}
		} else {
			common.Log.Debug("ERROR: --------INVALID TYPE XrefStm invalid?-------")
			// Continue, we do not define anything -> null object.
			// 7.5.8.3:
			//
			// In PDF 1.5 through PDF 1.7, only types 0, 1, and 2 are
			// allowed. Any other value shall be interpreted as a
			// reference to the null object, thus permitting new entry
			// types to be defined in the future.
			continue
		}
	}

	return nil
}

// read xref table
func (parser *PdfParser) readXrefTable(prevLine string) error {
	curObjIdx := -1
	objCount := 0
	insideSubsection := false

	//ref^M34 45 ^M111 000 n
	if len(prevLine) > 3 {
		splitStrs := strings.Split(prevLine, "\r")
		for _, s := range splitStrs {
			s = strings.TrimSpace(s)
			result1 := reXrefSubsection.FindStringSubmatch(s)
			if len(result1) == 3 {
				// Match
				first, _ := strconv.Atoi(result1[1])
				second, _ := strconv.Atoi(result1[2])
				curObjIdx = first
				objCount = second
				insideSubsection = true
				common.Log.Trace("xref subsection: first object: %d objects: %d", curObjIdx, objCount)
				continue
			}

			result2 := reXrefEntry.FindStringSubmatch(s)
			if len(result2) == 4 {
				if !insideSubsection {
					common.Log.Debug("Error: Xref invalid format!")
					return errors.New("Xref invalid format")
				}

				first, _ := strconv.ParseInt(result2[1], 10, 64)
				gen, _ := strconv.Atoi(result2[2])
				third := result2[3]

				if strings.ToLower(third) == "n" && first > 1 {
					if x, ok := parser.xrefs[curObjIdx]; !ok || gen > x.generation {
						obj := XrefObject{
							objectNumber: curObjIdx,
							xtype:        XREF_TABLE_ENTRY,
							offset:       first,
							generation:   gen}
						parser.xrefs[curObjIdx] = obj
					}
				}
				curObjIdx++
			}
		}
	}

	for {
		line, err := parser.reader.ReadString('\n')
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "trailer") {
			common.Log.Trace("found trailer, %s", line)
			// sometimes get "trailer<<"
			if len(line) > 9 {
				offset := parser.GetFileOffset()
				parser.SetFileOffset(offset - int64(len(line)) + 6)
			}

			parser.skipSpaces()
			break
		}

		//like 34 56^M110000 000 n
		splitStrs := strings.Split(line, "\r")
		for _, s := range splitStrs {
			s = strings.TrimSpace(s)
			result1 := reXrefSubsection.FindStringSubmatch(s)
			if len(result1) == 3 {
				// Match
				first, _ := strconv.Atoi(result1[1])
				second, _ := strconv.Atoi(result1[2])
				curObjIdx = first
				objCount = second
				insideSubsection = true
				common.Log.Trace("xref subsection: first object: %d objects: %d", curObjIdx, objCount)
				continue
			}

			result2 := reXrefEntry.FindStringSubmatch(s)
			if len(result2) == 4 {
				if !insideSubsection {
					common.Log.Debug("Error: Xref invalid format!")
					return errors.New("Xref invalid format")
				}

				first, _ := strconv.ParseInt(result2[1], 10, 64)
				gen, _ := strconv.Atoi(result2[2])
				third := result2[3]

				if strings.ToLower(third) == "n" && first > 1 {
					// Object in use in the file!  Load it.
					// Ignore free objects ('f').
					//
					// Some malformed writers mark the offset as 0 to
					// indicate that the object is free, and still mark as 'n'
					// Fairly safe to assume is free if offset is 0.
					//
					// Some malformed writers even seem to have values such as
					// 1.. Assume null object for those also. That is referring
					// to within the PDF version in the header clearly.
					//
					// Load if not existing or higher generation number than previous.
					// Usually should not happen, lower generation numbers
					// would be marked as free.  But can still happen!
					if x, ok := parser.xrefs[curObjIdx]; !ok || gen > x.generation {
						obj := XrefObject{
							objectNumber: curObjIdx,
							xtype:        XREF_TABLE_ENTRY,
							offset:       first,
							generation:   gen}
						parser.xrefs[curObjIdx] = obj
					}
				}

				curObjIdx++
				continue
			}

			if strings.Compare(s, "%%EOF") == 0 {
				common.Log.Debug("ERROR: end of file - trailer not found - error!")
				return errors.New("End of file - trailer not found")
			}
		}
	}

	return nil
}

// Numeric objects.
// Section 7.3.3.
// Integer or Float.
//
// An integer shall be written as one or more decimal digits optionally
// preceded by a sign. The value shall be interpreted as a signed
// decimal integer and shall be converted to an integer object.
//
// A real value shall be written as one or more decimal digits with an
// optional sign and a leading, trailing, or embedded PERIOD (2Eh)
// (decimal point). The value shall be interpreted as a real number
// and shall be converted to a real object.
//
// Regarding exponential numbers: 7.3.3 Numeric Objects:
// A conforming writer shall not use the PostScript syntax for numbers
// with non-decimal radices (such as 16#FFFE) or in exponential format
// (such as 6.02E23).
// Nonetheless, we sometimes get numbers with exponential format, so
// we will support it in the reader (no confusion with other types, so
// no compromise).
func (parser *PdfParser) parseNumber() (PdfObject, error) {
	isFloat := false
	allowSigns := true
	var r bytes.Buffer
	for {
		common.Log.Trace("Parsing number \"%s\"", r.String())
		bb, err := parser.reader.Peek(1)
		if err == io.EOF {
			// GH: EOF handling.  Handle EOF like end of line.  Can happen with
			// encoded object streams that the object is at the end.
			// In other cases, we will get the EOF error elsewhere at any rate.
			break // Handle like EOF
		}
		if err != nil {
			common.Log.Debug("ERROR %s", err)
			return nil, err
		}
		if allowSigns && (bb[0] == '-' || bb[0] == '+') {
			// Only appear in the beginning, otherwise serves as a delimiter.
			b, _ := parser.reader.ReadByte()
			r.WriteByte(b)
			allowSigns = false // Only allowed in beginning, and after e (exponential).
		} else if IsDecimalDigit(bb[0]) {
			b, _ := parser.reader.ReadByte()
			r.WriteByte(b)
		} else if bb[0] == '.' {
			b, _ := parser.reader.ReadByte()
			r.WriteByte(b)
			isFloat = true
		} else if bb[0] == 'e' {
			// Exponential number format.
			b, _ := parser.reader.ReadByte()
			r.WriteByte(b)
			isFloat = true
			allowSigns = true
		} else {
			break
		}
	}

	if isFloat {
		fVal, err := strconv.ParseFloat(r.String(), 64)
		o := PdfObjectFloat(fVal)
		return &o, err
	} else {
		intVal, err := strconv.ParseInt(r.String(), 10, 64)
		o := PdfObjectInteger(intVal)
		return &o, err
	}
}

// Parse a name starting with '/'.
func (parser *PdfParser) parseName() (PdfObjectName, error) {

	var ch byte
	var err error
	var r bytes.Buffer

	for {
		ch, err = parser.reader.ReadByte()
		if err == io.EOF {
			return "", errors.New("read until end of file")
		}

		if ch == '/' {
			break
		}
	}

	jump := false
	for {
		if jump {
			parser.reader.UnreadByte()
			break
		}

		ch, err = parser.reader.ReadByte()
		if err == io.EOF {
			return "", errors.New("read until end of file")
		}

		if isSpace := IsWhiteSpace(ch); isSpace {
			break
		}

		switch ch {
		case '/', '{', '}', '[', ']', '(', ')', '<', '>', '%':
			{
				jump = true
			}
		case '#':
			// like /A#42 = AB
			{
				firstByte, err := parser.reader.ReadByte()
				if err != nil {
					return PdfObjectName(r.String()), err
				}
				secondByte, _ := parser.reader.ReadByte()
				if err != nil {
					return PdfObjectName(r.String()), err
				}

				hexcode := []byte{firstByte, secondByte}
				code, err := hex.DecodeString(string(hexcode[:]))
				if err != nil {
					return PdfObjectName(r.String()), err
				}
				r.Write(code)
			}
		default:
			r.WriteByte(ch)
		}
	}

	return PdfObjectName(r.String()), nil
}

// Parse reference to an indirect object.
func parseReference(refStr string) (PdfObjectReference, error) {
	objref := PdfObjectReference{}

	result := reReference.FindStringSubmatch(string(refStr))
	if len(result) < 3 {
		common.Log.Debug("Error parsing reference")
		return objref, errors.New("Unable to parse reference")
	}

	objNum, _ := strconv.Atoi(result[1])
	genNum, _ := strconv.Atoi(result[2])
	objref.ObjectNumber = int64(objNum)
	objref.GenerationNumber = int64(genNum)

	return objref, nil
}

// Starts with '<' ends with '>'.
// Currently not converting the hex codes to characters.
func (parser *PdfParser) parseHexString() (PdfObjectString, error) {
	// jump '<'
	parser.reader.ReadByte()

	var r bytes.Buffer
	for {
		b, err := parser.reader.ReadByte()
		if err != nil {
			return PdfObjectString(""), err
		}

		if b == '>' {
			break
		}

		if !IsWhiteSpace(b) {
			r.WriteByte(b)
		}
	}

	if r.Len()%2 == 1 {
		common.Log.Debug("no valid hex, append 0")
		r.WriteRune('0')
	}

	buf, _ := hex.DecodeString(r.String())
	return PdfObjectString(buf), nil
}

// A string starts with '(' and ends with ')'.
func (parser *PdfParser) parseString() (PdfObjectString, error) {
	// jump the '('
	parser.reader.ReadByte()

	var r bytes.Buffer
	parenthesesDepth := 1
	for {
		bb, err := parser.reader.ReadByte()
		if err != nil {
			return PdfObjectString(r.String()), err
		}

		if bb == '\\' { // Escape sequence.
			b, err := parser.reader.ReadByte()
			if err != nil {
				return PdfObjectString(r.String()), err
			}

			// Octal '\ddd' number (base 8).
			if IsOctalDigit(b) {
				bb, err := parser.reader.Peek(2)
				if err != nil {
					return PdfObjectString(r.String()), err
				}

				numeric := []byte{}
				numeric = append(numeric, b)
				for _, val := range bb {
					if IsOctalDigit(val) {
						numeric = append(numeric, val)
					} else {
						break
					}
				}
				parser.reader.Discard(len(numeric) - 1)
				common.Log.Trace("Numeric string \"%s\"", numeric)

				code, err := strconv.ParseUint(string(numeric), 8, 32)
				if err != nil {
					return PdfObjectString(r.String()), err
				}
				r.WriteByte(byte(code))
				continue
			}

			switch b {
			case 'n':
				r.WriteRune('\n')
			case 'r':
				r.WriteRune('\r')
			case 't':
				r.WriteRune('\t')
			case 'b':
				r.WriteRune('\b')
			case 'f':
				r.WriteRune('\f')
			case '(':
				r.WriteRune('(')
			case ')':
				r.WriteRune(')')
			case '\\':
				r.WriteRune('\\')
			}

			continue
		} else if bb == '(' {
			//handle parentheses like (())
			parenthesesDepth++
		} else if bb == ')' {
			parenthesesDepth--
			if parenthesesDepth == 0 {
				break
			}
		}

		r.WriteByte(bb)
	}

	return PdfObjectString(r.String()), nil
}

// Starts with '[' ends with ']'.  Can contain any kinds of direct objects.
func (parser *PdfParser) parseArray() (PdfObjectArray, error) {
	arr := make(PdfObjectArray, 0)

	//skip '['
	parser.reader.ReadByte()

	for {
		ch, err := parser.reader.ReadByte()
		if err != nil {
			return arr, err
		}

		if IsWhiteSpace(ch) {
			continue
		}

		if ch == ']' {
			break
		}

		parser.reader.UnreadByte()
		obj, err := parser.parseObject()
		if err != nil {
			return arr, err
		}
		arr = append(arr, obj)
	}

	return arr, nil
}

// Parse null object.
func (parser *PdfParser) parseNull() (PdfObjectNull, error) {
	_, err := parser.reader.Discard(4)
	return PdfObjectNull{}, err
}

// Parse bool object.
func (parser *PdfParser) parseBool() (PdfObjectBool, error) {
	bb := make([]byte, 5)

	n, err := parser.reader.Read(bb)
	if err != nil {
		return PdfObjectBool(false), err
	}

	common.Log.Trace("buffer size: 5, read: %d", n)

	if n >= 4 && string(bb[:4]) == "true" {
		parser.reader.UnreadByte()
		return PdfObjectBool(true), nil
	}

	if n >= 5 && string(bb[:5]) == "false" {
		return PdfObjectBool(false), nil
	}

	return PdfObjectBool(false), errors.New("Unexpected boolean string")
}

// Detect the signature at the current file position and parse
// the corresponding object.
func (parser *PdfParser) parseObject() (PdfObject, error) {

	common.Log.Trace("Read direct object")
	bb, err := parser.reader.Peek(2)
	if err != nil {
		return nil, errors.New("Object parsing error, no content to peek")
	}

	ch := bb[0]

	switch ch {
	case '/':
		{
			name, err := parser.parseName()
			common.Log.Trace("->Name: '%s'", name)
			return &name, err
		}
	case '<':
		{
			if bb[1] == '<' {
				common.Log.Trace("->Dict!")
				dict, err := parser.ParseDict()
				return dict, err
			} else {
				common.Log.Trace("->Hex string")
				str, err := parser.parseHexString()
				return &str, err
			}
		}
	case '(':
		{
			common.Log.Trace("->String!")
			str, err := parser.parseString()
			return &str, err
		}
	case 'f', 't':
		{
			common.Log.Trace("->boolean")
			b, err := parser.parseBool()
			return &b, err
		}
	case '[':
		{
			common.Log.Trace("->Array!")
			arr, err := parser.parseArray()
			return &arr, err
		}
	case 'n':
		{
			common.Log.Trace("->Null")
			null, err := parser.parseNull()
			return &null, err
		}
	case '+', '-', '.', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		{
			common.Log.Trace("->Number or ref?")
			// Reference or number?
			// Let's peek farther to find out.
			bb, _ = parser.reader.Peek(15)
			peekStr := string(bb)
			common.Log.Trace("Peek str: %s", peekStr)

			// Match reference.
			result1 := reReference.FindStringSubmatch(peekStr)
			if len(result1) > 1 {
				bb, _ = parser.reader.ReadBytes('R')
				common.Log.Trace("-> !Ref: '%s'", string(bb[:]))
				ref, err := parseReference(string(bb))
				return &ref, err
			}

			result2 := reNumeric.FindStringSubmatch(peekStr)
			if len(result2) > 1 {
				// Number object.
				common.Log.Trace("-> Number!")
				num, err := parser.parseNumber()
				return num, err
			}

			result2 = reExponential.FindStringSubmatch(peekStr)
			if len(result2) > 1 {
				// Number object (exponential)
				common.Log.Trace("-> Exponential Number!")
				common.Log.Trace("%s", result2)
				num, err := parser.parseNumber()
				return num, err
			}

			common.Log.Debug("ERROR Unknown (peek \"%s\")", peekStr)
		}
	}

	common.Log.Debug("xxx: ", ch)
	return nil, errors.New("Object parsing error - unexpected pattern")
}

// Reads and parses a PDF dictionary object enclosed with '<<' and '>>'
// TODO: Unexport (v3).
func (parser *PdfParser) ParseDict() (*PdfObjectDictionary, error) {
	common.Log.Trace("Reading PDF Dict!")

	dict := MakeDict()

	// Pass the '<<'
	c, _ := parser.reader.ReadByte()
	if c != '<' {
		return nil, errors.New("Invalid dict")
	}
	c, _ = parser.reader.ReadByte()
	if c != '<' {
		return nil, errors.New("Invalid dict")
	}

	readingKey := true
	readingValue := false
	var currCh byte = 0
	var prevCh byte = 0
	var err error

	var keyName PdfObjectName
	for {
		prevCh = currCh
		currCh, err = parser.reader.ReadByte()
		if err != nil {
			return nil, err
		}

		if prevCh == '>' && currCh == '>' {
			common.Log.Trace("EOF dictionary")
			break
		}

		// comment
		if currCh == '%' {
			// why break with the comments
			parser.reader.UnreadByte()
			parser.skipComments()
		} else if currCh == '/' && readingKey {
			common.Log.Trace("Parse the name!")

			parser.reader.UnreadByte()
			keyName, err = parser.parseName()
			if err != nil {
				common.Log.Debug("ERROR Returning name err %s", err)
				return nil, err
			}
			common.Log.Trace("Key: %s", keyName)
			readingKey = false
			readingValue = true

			// Some writers have a bug where the null is appended without
			// space.  For example "\Boundsnull"
			if len(keyName) > 4 && keyName[len(keyName)-4:] == "null" {
				newKey := keyName[0 : len(keyName)-4]
				common.Log.Debug("Taking care of null bug (%s)", keyName)
				common.Log.Debug("New key \"%s\" = null", newKey)
				dict.Set(newKey, MakeNull())
				readingKey = true
				readingValue = false
			}
		} else if readingValue && !IsWhiteSpace(currCh) {
			parser.reader.UnreadByte()
			val, err := parser.parseObject()
			if err != nil {
				return nil, err
			}

			if val != nil {
				readingKey = true
				readingValue = false
				dict.Set(keyName, val)
				common.Log.Trace("dict[%s] = %s", keyName, val.String())
			}
		}
	}

	return dict, nil
}

func findXrefPosition(list []int64, val int64) bool {
	find := false
	for i := 0; i < len(list); i++ {
		if val == list[i] {
			find = true
			break
		}
	}

	return find
}

// Parse the pdf version from the beginning of the file.
// Returns the major and minor parts of the version.
// E.g. for "PDF-1.7" would return 1 and 7.
func (parser *PdfParser) parsePdfVersion() (int, int, error) {
	parser.rs.Seek(0, io.SeekStart)

	b := make([]byte, 20)
	parser.rs.Read(b)

	result1 := rePdfVersion.FindStringSubmatch(string(b))
	if len(result1) < 3 {
		common.Log.Debug("Failed recovery - unable to find version")
		return 0, 0, nil
	}

	majorVersion, err := strconv.Atoi(result1[1])
	if err != nil {
		return 0, 0, err
	}

	minorVersion, err := strconv.Atoi(result1[2])
	if err != nil {
		return 0, 0, err
	}

	common.Log.Trace("Pdf version %d.%d", majorVersion, minorVersion)

	return int(majorVersion), int(minorVersion), nil
}

// IsEncrypted checks if the document is encrypted. A bool flag is returned indicating the result.
// First time when called, will check if the Encrypt dictionary is accessible through the trailer dictionary.
// If encrypted, prepares a crypt datastructure which can be used to authenticate and decrypt the document.
// On failure, an error is returned.
func (parser *PdfParser) IsEncrypted() (bool, error) {
	if parser.crypter != nil {
		return true, nil
	}

	if parser.trailerDict != nil {
		common.Log.Trace("Checking encryption dictionary!")
		encDictRef, isEncrypted := parser.trailerDict.Get("Encrypt").(*PdfObjectReference)
		if isEncrypted {
			common.Log.Trace("Is encrypted!")
			common.Log.Trace("0: Look up ref %q", encDictRef)
			encObj, err := parser.LookupByReference(*encDictRef)
			common.Log.Trace("1: %q", encObj)
			if err != nil {
				return false, err
			}

			encIndObj, ok := encObj.(*PdfIndirectObject)
			if !ok {
				common.Log.Debug("Encryption object not an indirect object")
				return false, errors.New("Type check error")
			}
			encDict, ok := encIndObj.PdfObject.(*PdfObjectDictionary)

			common.Log.Trace("2: %q", encDict)
			if !ok {
				return false, errors.New("Trailer Encrypt object non dictionary")
			}
			crypter, err := PdfCryptMakeNew(parser, encDict, parser.trailerDict)
			if err != nil {
				return false, err
			}

			parser.crypter = &crypter
			common.Log.Trace("Crypter object %b", crypter)
			return true, nil
		}
	}
	return false, nil
}

// Decrypt attempts to decrypt the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful, false otherwise.
// An error is returned when there is a problem with decrypting.
func (parser *PdfParser) Decrypt(password []byte) (bool, error) {
	// Also build the encryption/decryption key.
	if parser.crypter == nil {
		return false, errors.New("Check encryption first")
	}

	authenticated, err := parser.crypter.authenticate(password)
	if err != nil {
		return false, err
	}

	if !authenticated {
		authenticated, err = parser.crypter.authenticate([]byte(""))
	}

	return authenticated, err
}

func (parser *PdfParser) IsAuthenticated() bool {
	return parser.crypter.Authenticated
}

// NewParser creates a new parser for a PDF file via ReadSeeker. Loads the cross reference stream and trailer.
// An error is returned on failure.
func NewParser(rs io.ReadSeeker) (*PdfParser, error) {
	parser := &PdfParser{}

	parser.rs = rs
	parser.ObjCache = make(ObjectCache)
	parser.streamLengthReferenceLookupInProgress = map[int64]bool{}

	// Start by reading the xrefs (from bottom).
	err := parser.readReferenceData()
	if err != nil {
		common.Log.Debug("ERROR: Failed to load xref table! %s", err)
		return nil, err
	}

	common.Log.Trace("Trailer: %s", parser.trailerDict)

	if len(parser.xrefs) == 0 {
		return nil, errors.New("Empty XREF table - Invalid")
	}

	majorVersion, minorVersion, err := parser.parsePdfVersion()
	if err != nil {
		common.Log.Error("Unable to parse version: %v", err)
		return nil, err
	}
	parser.majorVersion = majorVersion
	parser.minorVersion = minorVersion

	return parser, nil
}

func (parser *PdfParser) GetRootDict() *PdfObjectDictionary {
	return parser.rootDict
}

func (parser *PdfParser) GetCrypter() *PdfCrypt {
	return parser.crypter
}

// GetTrailer returns the PDFs trailer dictionary. The trailer dictionary is typically the starting point for a PDF,
// referencing other key objects that are important in the document structure.
func (parser *PdfParser) GetTrailer() *PdfObjectDictionary {
	return parser.trailerDict
}

func (parser *PdfParser) readReferenceData() error {
	// use to store multi xref table offsets
	startXrefPositions := []int64{}
	parser.xrefs = make(XrefTable)
	parser.objstms = make(ObjectStreams)

	numBytes := 32
	b := make([]byte, numBytes)

	//first find "startxref" back from the EOF
	if _, err := parser.rs.Seek(-32, io.SeekEnd); err != nil {
		common.Log.Debug("Error: can't seek back %d from file eof, err: %v", numBytes, err)
		return err
	}

	if _, err := parser.rs.Read(b); err != nil {
		common.Log.Debug("Failed to reading while looking for startxref: %v", err)
		return err
	}

	result := reStartXref.FindStringSubmatch(string(b))
	if len(result) < 2 {
		common.Log.Debug("Error: startxref not found!")
		return errors.New("Startxref not found")
	} else if len(result) > 2 {
		common.Log.Debug("Error: multiple startxref (%s)!", b)
		return errors.New("Multiple startxref entries?")
	}

	xrefOffset, _ := strconv.ParseInt(result[1], 10, 64)
	common.Log.Trace("xref start at %d", xrefOffset)
	startXrefPositions = append(startXrefPositions, xrefOffset)

	// protect to recurse parse xref
	backward_compatibility := false
	//parse the xref
	for {
		if _, err := parser.rs.Seek(xrefOffset, io.SeekStart); err != nil {
			common.Log.Debug("Error: can't seek to the xref data, err: %v", err)
			return err
		}
		parser.reader = bufio.NewReader(parser.rs)

		ch, err := parser.reader.ReadByte()
		if err != nil {
			common.Log.Debug("Error: read char failed, err: %v", err)
			return err
		}

		if ch == 'x' {
			//specific data for xref table
			common.Log.Trace("Standard xref section table!")
			// read first line
			line, err := parser.reader.ReadString('\n')
			line = strings.TrimSpace(line)

			if len(line) < 3 || !strings.HasPrefix(line, "ref") {
				common.Log.Debug("Error: invalid xref keyword")
				return errors.New("Invalid xref keyword")
			}

			//parse xref table
			if err := parser.readXrefTable(line); err != nil {
				common.Log.Debug("Error: parse xref table failed, err: %v", err)
				return err
			}

			//parse the trailer dict
			dict, err := parser.ParseDict()
			if err != nil {
				common.Log.Debug("Error: parse trailer dict failed, err: %v", err)
				return err
			}

			if parser.trailerDict == nil {
				parser.trailerDict = dict
			}

			//get root dict
			if !parser.getRoot {
				rootObj, err := parser.Trace(dict.Get("Root"))
				if err != nil {
					common.Log.Debug("Error: failed to load root element, err: %s", err)
					return err
				}

				rootDict, ok := rootObj.(*PdfObjectDictionary)
				if !ok {
					common.Log.Debug("Error: root element has no dict")
				} else {
					parser.getRoot = true
					parser.rootDict = rootDict
					//upate the trailerDict who has root
					parser.trailerDict = dict
				}
			}

			// Check the XrefStm object also from the trailer.
			if xrefStm := dict.Get("XRefStm"); xrefStm != nil {
				xrefStmObj, ok := xrefStm.(*PdfObjectInteger)
				if !ok {
					return errors.New("XRefStm != int")
				} else {
					xrefOffset = int64(*xrefStmObj)
					backward_compatibility = true
				}
			} else if xrefPrev := dict.Get("Prev"); xrefPrev != nil {
				xrefPrevObj, ok := xrefPrev.(*PdfObjectInteger)
				if !ok {
					common.Log.Debug("Invalid Prev reference: Not a *PdfObjectInteger (%T)", xrefPrevObj)
					return errors.New("prev not a PdfObjectInteger")
				} else {
					xrefOffset = int64(*xrefPrevObj)
				}
			}

			found := findXrefPosition(startXrefPositions, xrefOffset)
			if found {
				common.Log.Trace("no more xref offset to handle")
				break
			}

			// continue to handle xrefOffset
			startXrefPositions = append(startXrefPositions, xrefOffset)
		} else {
			// compact data for xref table
			common.Log.Trace("xref points to an object. Probably xref object")
			parser.reader.UnreadByte()

			// try to parse object to PdfObjectStream
			xrefObj, err := parser.ParseIndirectObject()
			if err != nil {
				common.Log.Debug("Error: failed to read xref stream object, err: %v", err)
				return errors.New("failed to read xref stream object")
			}

			common.Log.Trace("XRefStm object: %s", xrefObj)
			xs, ok := xrefObj.(*PdfObjectStream)
			if !ok {
				common.Log.Debug("Error: XRefStm pointing to non-stream object!")
				return errors.New("XRefStm pointing to a non-stream object")
			}

			var prev PdfObject
			if backward_compatibility {
				if prev = parser.trailerDict.Get("Prev"); prev != nil {
					xrefPrevObj, ok := prev.(*PdfObjectInteger)
					if !ok {
						common.Log.Debug("Invalid Prev reference: Not a *PdfObjectInteger (%T)", xrefPrevObj)
						return nil
					} else {
						xrefOffset = int64(*xrefPrevObj)
					}
				}
			} else {
				if prev = xs.PdfObjectDictionary.Get("Prev"); prev != nil {
					xrefPrevObj, ok := prev.(*PdfObjectInteger)
					if !ok {
						common.Log.Debug("Invalid Prev reference: Not a *PdfObjectInteger (%T)", xrefPrevObj)
						return nil
					} else {
						xrefOffset = int64(*xrefPrevObj)
					}
				}
			}

			//parse xref table
			if err := parser.readXrefStream(xs); err != nil {
				common.Log.Debug("Error: parse xref stream failed, err: %v", err)
				return err
			}

			//get root dict
			if !parser.getRoot {
				rootObj, err := parser.Trace(xs.PdfObjectDictionary.Get("Root"))
				if err != nil {
					common.Log.Debug("Error: failed to load root element, err: %s", err)
					return err
				}

				rootDict, ok := rootObj.(*PdfObjectDictionary)
				if !ok {
					common.Log.Debug("Error: root element has no dict")
				} else {
					parser.getRoot = true
					parser.rootDict = rootDict
					// update trailer who has root
					parser.trailerDict = xs.PdfObjectDictionary
				}
			}

			found := findXrefPosition(startXrefPositions, xrefOffset)
			if found {
				common.Log.Trace("no more xref offset to handle")
				break
			}

			// continue to handle xrefOffset
			startXrefPositions = append(startXrefPositions, xrefOffset)
		}
	}

	return nil
}
