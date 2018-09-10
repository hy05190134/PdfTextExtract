/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
	"../cmap"
	"../common"
	. "../core"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
)

const (
	medium int = iota
	bold
	roman
)

type FontMetrics struct {
	mFontName   string
	mFontFamily string
	mFirstChar  uint
	mLastChar   uint
	mDescent    float64
	mFontBbox   [4]float64
	//enum
	mFontWeight   int
	mCapHeight    float64
	mFlags        uint
	mXHeight      float64
	mItalicAngle  float64
	mAscent       float64
	mWidths       []uint
	mMissingWidth uint
	mLeading      uint
	mVscale       float64
	mHscale       float64
	mFontMatrix   [6]float64
}

type Font struct {
	mFontDictionary *PdfObjectDictionary
	mFontEncoding   string

	mPredefinedSimpleEncoding bool
	mPredefinedCmap           bool

	mCmap      *cmap.CMap
	mToCidCmap *cmap.CMap

	mSimpleEncodingTable    []uint
	mOwnSimpleEncodingTable bool

	mFontType string
	mBaseFont string

	mFontMetrics    FontMetrics
	mMultibyte      bool
	mFontDescriptor *PdfObjectDictionary

	mCidBegin *byte
	mCidLen   uint
}

func (font *Font) GetCmap() *cmap.CMap {
	return font.mCmap
}

func (font *Font) GetCidCmap() *cmap.CMap {
	return font.mToCidCmap
}

func (font *Font) GetSimpleEncodingTableFlag() bool {
	return font.mPredefinedSimpleEncoding
}

func (font *Font) GetmPredefinedCmap() bool {
	return font.mPredefinedCmap
}

func (font *Font) GetSimpleEncodingTable() []uint {
	return font.mSimpleEncodingTable
}

func (font *Font) loadFontDescriptor() {
	if font.mFontDescriptor != nil {
		font.mFontMetrics.mFontName = "unkown"
		if mFontName, ok := font.mFontDescriptor.Get("FontName").(*PdfObjectName); ok {
			font.mFontMetrics.mFontName = string(*mFontName)
		}

		font.mFontMetrics.mFlags = 0
		if mFlags, ok := font.mFontDescriptor.Get("Flags").(*PdfObjectInteger); ok {
			font.mFontMetrics.mFlags = uint(*mFlags)
		}

		font.mFontMetrics.mAscent = 0.0
		if mAscent, err := GetNumberAsFloat(font.mFontDescriptor.Get("Ascent")); err == nil {
			font.mFontMetrics.mAscent = mAscent
		}

		font.mFontMetrics.mDescent = 0.0
		if mDescent, err := GetNumberAsFloat(font.mFontDescriptor.Get("Descent")); err == nil {
			font.mFontMetrics.mDescent = mDescent
		}

		font.mFontMetrics.mItalicAngle = 0.0
		if mItalicAngle, err := GetNumberAsFloat(font.mFontDescriptor.Get("ItalicAngle")); err == nil {
			font.mFontMetrics.mItalicAngle = mItalicAngle
		}

		font.mFontMetrics.mXHeight = 0.0
		if mXHeight, err := GetNumberAsFloat(font.mFontDescriptor.Get("XHeight")); err == nil {
			font.mFontMetrics.mXHeight = mXHeight
		}

		font.mFontMetrics.mMissingWidth = 0
		if mMissingWidth, ok := font.mFontDescriptor.Get("MissingWidth").(*PdfObjectInteger); ok {
			font.mFontMetrics.mMissingWidth = uint(*mMissingWidth)
		}

		font.mFontMetrics.mLeading = 0
		if mLeading, ok := font.mFontDescriptor.Get("Leading").(*PdfObjectInteger); ok {
			font.mFontMetrics.mLeading = uint(*mLeading)
		}

		font.mFontMetrics.mCapHeight = 0.0
		if mCapHeight, err := GetNumberAsFloat(font.mFontDescriptor.Get("CapHeight")); err == nil {
			font.mFontMetrics.mCapHeight = mCapHeight
		}

		if mFontBboxArr, ok := font.mFontDescriptor.Get("FontBBox").(*PdfObjectArray); ok {
			common.Log.Trace("fontbbox size: %d", len(*mFontBboxArr))
			for i := 0; i < len(*mFontBboxArr); i++ {
				font.mFontMetrics.mFontBbox[i], _ = GetNumberAsFloat((*mFontBboxArr)[i])
			}
		} else {
			if mFontBboxArr, ok := font.mFontDictionary.Get("FontBBox").(*PdfObjectArray); ok {
				common.Log.Trace("fontbbox size: %d", len(*mFontBboxArr))
				for i := 0; i < len(*mFontBboxArr); i++ {
					font.mFontMetrics.mFontBbox[i], _ = GetNumberAsFloat((*mFontBboxArr)[i])
				}
			}
		}
	}
}

func (this *PdfReader) parsePredefinedCMap(font *Font, unicodeName string) error {

	//get charcode to cid map
	cmapToCidFilename := "resources/" + font.mFontEncoding
	streamData, err := ioutil.ReadFile(cmapToCidFilename)
	if err != nil {
		common.Log.Debug("read file %s failed, %s", cmapToCidFilename, err)
		return err
	}

	//common.Log.Debug("charcode_to_cid data: %s\n\n", streamData)

	mCmap, err := cmap.LoadCmapFromData(streamData)
	if err != nil {
		common.Log.Debug("load charcode_to_cid cmap from %s failed, err: %s", cmapToCidFilename, err)
		return err
	}
	font.mToCidCmap = mCmap

	//get cid to unicode map
	cidToUnicodeFilename := "resources/" + unicodeName
	streamData, err = ioutil.ReadFile(cidToUnicodeFilename)
	if err != nil {
		common.Log.Debug("read file %s failed, %s", cidToUnicodeFilename, err)
		return err
	}

	//common.Log.Debug("cid_to_unicode data: %s\n\n", streamData)

	mCmap, err = cmap.LoadCmapFromData(streamData)
	if err != nil {
		common.Log.Debug("load cid_to_unicode cmap from %s failed, err: %s", cidToUnicodeFilename, err)
		return err
	}
	font.mCmap = mCmap

	/*
		    for k, v := range font.mToCidCmap.GetCodeMap() {
				common.Log.Debug("chartocid, %d: %s", k, v)
			}

			for k, v := range font.mCmap.GetCodeMap() {
				common.Log.Debug("cidtounicode, %d, %s", k, v)
			}
	*/

	return nil
}

func (this *PdfReader) getFontEncoding(font *Font) error {

	//check if font has "ToUnicode" stream
	if toUnicodeObj := font.mFontDictionary.Get("ToUnicode"); toUnicodeObj != nil {
		obj, err := this.parser.Trace(toUnicodeObj)
		if err != nil {
			common.Log.Debug("Error: trace to object stream failed, err: %s", err)
			return err
		}

		toUnicodeStreamObj, ok := obj.(*PdfObjectStream)
		if !ok {
			return errors.New("toUnicode is not stream obj")
		}

		decodedData, err := DecodeStream(toUnicodeStreamObj)
		if err != nil {
			return err
		}

		common.Log.Trace("tounicode data: %s\n\n", decodedData)

		mCmap, err := cmap.LoadCmapFromData(decodedData)
		if err != nil {
			common.Log.Debug("load cmap failed, err: %s", err)
			return err
		}
		font.mCmap = mCmap
	}

	//encoding maybe a predefined name string or dict
	if encodingObject := font.mFontDictionary.Get("Encoding"); encodingObject != nil {
		encodingObjectName, ok := encodingObject.(*PdfObjectName)
		if ok {
			//common.Log.Debug("font encoding is encoding name: %s", *encodingObjectName)
			font.mFontEncoding = string(*encodingObjectName)
			if v, ok := mPdfPredefinedSimpleEncodings[font.mFontEncoding]; ok {
				font.mPredefinedSimpleEncoding = true
				font.mSimpleEncodingTable = v
			} else {
				if unicodeName, ok := mPdfCidToUnicode[font.mFontEncoding]; ok {
					if err := this.parsePredefinedCMap(font, unicodeName); err == nil {
						font.mPredefinedCmap = true
					}
				}
			}
		}

		encodingObjectDict, ok := encodingObject.(*PdfObjectDictionary)
		if ok {
			font.mPredefinedSimpleEncoding = true
			font.mOwnSimpleEncodingTable = true
			font.mSimpleEncodingTable = make([]uint, 256)

			sourceTable := StandardEncodingUtf8
			if baseEncodingObj, ok := encodingObjectDict.Get("BaseEncoding").(*PdfObjectName); ok {
				baseEncodingName := string(*baseEncodingObj)
				if v, ok := mPdfPredefinedSimpleEncodings[baseEncodingName]; ok {
					sourceTable = v
				}
			}

			for i := 0; i < 256; i++ {
				font.mSimpleEncodingTable[i] = sourceTable[i]
			}

			if differenctObjArray, ok := encodingObjectDict.Get("Differences").(*PdfObjectArray); ok {
				var replacements uint = 0
				for j := 0; j < len(*differenctObjArray); j++ {
					if objNumber, ok := (*differenctObjArray)[j].(*PdfObjectInteger); ok {
						replacements = uint(*objNumber)
						if replacements > 255 {
							replacements = 0
						}
					} else {
						//TODO: parse obj in differences array according to CharProcs
						if objName, ok := (*differenctObjArray)[j].(*PdfObjectName); ok {
							if val, ok := mPdfCharacterNames[string(*objName)]; ok {
								font.mSimpleEncodingTable[replacements] = val
								replacements++
								if replacements > 255 {
									replacements = 0
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func (this *PdfReader) getFontInfo(font *Font) error {
	// get FontType
	font.mFontType = "Type1"
	if mFontTypeName, ok := font.mFontDictionary.Get("Subtype").(*PdfObjectName); ok {
		mFontTypeStr := string(*mFontTypeName)
		if mFontTypeStr == "Type3" || mFontTypeStr == "Type0" || mFontTypeStr == "MMType1" || mFontTypeStr == "TrueType" {
			font.mFontType = mFontTypeStr
		}
	}

	// get FontDescriptor
	if mFontDescriptor := font.mFontDictionary.Get("FontDescriptor"); mFontDescriptor != nil {
		mFontDescriptorObj, err := this.parser.Trace(mFontDescriptor)
		if err != nil {
			common.Log.Debug("Error: trace font descriptor to indirect obj failed, err: %s", err)
			return err
		}

		if mFontDescriptorDict, ok := mFontDescriptorObj.(*PdfObjectDictionary); ok {
			font.mFontDescriptor = mFontDescriptorDict
		}
	}

	// get basefont
	font.mBaseFont = "unkown"
	if baseFontName, ok := font.mFontDictionary.Get("BaseFont").(*PdfObjectName); ok {
		font.mBaseFont = string(*baseFontName)
	}

	font.mFontMetrics.mWidths = make([]uint, 0, 256)

	if font.mFontType == "Type0" {
		font.mMultibyte = true
		if descendantFontsArr, ok := font.mFontDictionary.Get("DescendantFonts").(*PdfObjectArray); ok && len(*descendantFontsArr) > 0 {
			//only one value is allowed
			descendantFontObj, err := this.parser.Trace((*descendantFontsArr)[0])
			if err != nil {
				common.Log.Debug("Error: trace font descendantFont to direct obj failed, err: %s", err)
				return err
			}

			if descendantFontDict, ok := descendantFontObj.(*PdfObjectDictionary); ok {
				//handle Adobe-GB1, Adobe-CNS1, Adobe-Japan1, Adobe-Korea1 && other have handle
				if fontSystemInfo, ok := descendantFontDict.Get("CIDSystemInfo").(*PdfObjectDictionary); ok {
					var register *PdfObjectString
					if registryObj, err := this.parser.Trace(fontSystemInfo.Get("Registry")); err == nil {
						register = registryObj.(*PdfObjectString)
					}

					var ordering *PdfObjectString
					if orderingObj, err := this.parser.Trace(fontSystemInfo.Get("Ordering")); err == nil {
						ordering = orderingObj.(*PdfObjectString)
					}

					supplement := fontSystemInfo.Get("Supplement").(*PdfObjectInteger)

					registerOrdering := string(*register) + "-" + string(*ordering)
					registerOrderingSupple := registerOrdering + "-" + strconv.Itoa(int(*supplement))

					if registerOrdering == "Adobe-GB1" || registerOrdering == "Adobe-CNS1" ||
						registerOrdering == "Adobe-Japan1" || registerOrdering == "Adobe-Korea1" {
						font.mFontEncoding = registerOrderingSupple
						unicodeName := registerOrdering + "-UCS2"
						if !font.mPredefinedCmap {
							if err := this.parsePredefinedCMap(font, unicodeName); err == nil {
								font.mPredefinedCmap = true
							}
						}
					}
				}

				if fontDescriptorObj, err := this.parser.Trace(descendantFontDict.Get("FontDescriptor")); err == nil {
					if descriptorObjDict, ok := fontDescriptorObj.(*PdfObjectDictionary); ok {
						font.mFontDescriptor = descriptorObjDict
					}
				}

				font.mFontMetrics.mMissingWidth = uint(1000)
				if dwObj, ok := descendantFontDict.Get("DW").(*PdfObjectInteger); ok {
					font.mFontMetrics.mMissingWidth = uint(*dwObj)
				}

				if wObjArr, ok := descendantFontDict.Get("W").(*PdfObjectArray); ok {
					gotValues := uint(0)
					var firstValue, toRange uint
					for j := 0; j < len(*wObjArr); j++ {
						if subWidthArr, ok := (*wObjArr)[j].(*PdfObjectArray); ok && gotValues == 1 {
							if int(firstValue) > len(font.mFontMetrics.mWidths) {
								fillLen := int(firstValue) - len(font.mFontMetrics.mWidths)

								fillSlice := make([]uint, fillLen)
								for k := 0; k < fillLen; k++ {
									fillSlice[k] = font.mFontMetrics.mMissingWidth
								}
								font.mFontMetrics.mWidths = append(font.mFontMetrics.mWidths, fillSlice...)

								subWidthSlice := make([]uint, len(*subWidthArr))
								for k := 0; k < len(*subWidthArr); k++ {
									if numObj, ok := (*subWidthArr)[k].(*PdfObjectInteger); ok {
										subWidthSlice[k] = uint(*numObj)
									} else {
										subWidthSlice[k] = 0
									}
								}
								font.mFontMetrics.mWidths = append(font.mFontMetrics.mWidths, subWidthSlice...)
							}

							gotValues = 0
						} else if numInterObj, ok := (*wObjArr)[j].(*PdfObjectInteger); ok {
							gotValues++
							if gotValues == 1 {
								firstValue = uint(*numInterObj)
							} else if gotValues == 2 {
								toRange = uint(*numInterObj)
							} else if gotValues == 3 {
								gotValues = 0
								if toRange < firstValue {
									toRange = firstValue
								}

								if int(toRange) >= len(font.mFontMetrics.mWidths) {
									fillLen := int(toRange) + 1 - len(font.mFontMetrics.mWidths)
									fillSlice := make([]uint, fillLen)
									for k := 0; k < fillLen; k++ {
										fillSlice[k] = font.mFontMetrics.mMissingWidth
									}
									font.mFontMetrics.mWidths = append(font.mFontMetrics.mWidths, fillSlice...)
								}
								calcValue := uint(*numInterObj)
								for k := firstValue; k <= toRange; k++ {
									font.mFontMetrics.mWidths[k] = calcValue
								}
							}
						}
					}
				}

				font.loadFontDescriptor()
				//warning TODO: Those fonts can be vertical. PDF parser should support that feature
			}
		}
	} else if font.mFontType == "Type3" {
		font.mFontMetrics.mFirstChar = 0
		if mFirstChar, ok := font.mFontDictionary.Get("FirstChar").(*PdfObjectInteger); ok {
			font.mFontMetrics.mFirstChar = uint(*mFirstChar)
		}

		font.mFontMetrics.mLastChar = 0
		if mLastChar, ok := font.mFontDictionary.Get("LastChar").(*PdfObjectInteger); ok {
			font.mFontMetrics.mLastChar = uint(*mLastChar)
		}

		if font.mFontDescriptor != nil {
			font.loadFontDescriptor()
		} else {
			if mFontBboxArr, ok := font.mFontDictionary.Get("FontBBox").(*PdfObjectArray); ok {
				common.Log.Trace("fontbbox size: %d", len(*mFontBboxArr))
				for i := 0; i < len(*mFontBboxArr); i++ {
					font.mFontMetrics.mFontBbox[i], _ = GetNumberAsFloat((*mFontBboxArr)[i])
				}
			}
		}

		font.mFontMetrics.mAscent = font.mFontMetrics.mFontBbox[3]
		font.mFontMetrics.mDescent = font.mFontMetrics.mFontBbox[1]

		if mFontMatrix, ok := font.mFontDictionary.Get("FontMatrix").(*PdfObjectArray); ok {
			common.Log.Trace("font matrix size: %d", len(*mFontMatrix))
			if len(*mFontMatrix) == 6 {
				for i := 0; i < 6; i++ {
					font.mFontMetrics.mFontMatrix[i], _ = GetNumberAsFloat((*mFontMatrix)[i])
				}
			}

			font.mFontMetrics.mVscale = font.mFontMetrics.mFontMatrix[1] + font.mFontMetrics.mFontMatrix[3]
			font.mFontMetrics.mHscale = font.mFontMetrics.mFontMatrix[0] + font.mFontMetrics.mFontMatrix[2]
		}
	} else {
		if fm, ok := mPdfFontMetricsMap[font.mBaseFont]; ok {
			font.mFontMetrics = fm
		} else {
			font.mFontMetrics.mFirstChar = 0
			if mFirstChar, ok := font.mFontDictionary.Get("FirstChar").(*PdfObjectInteger); ok {
				font.mFontMetrics.mFirstChar = uint(*mFirstChar)
			}

			font.mFontMetrics.mLastChar = 255
			if mLastChar, ok := font.mFontDictionary.Get("LastChar").(*PdfObjectInteger); ok {
				font.mFontMetrics.mLastChar = uint(*mLastChar)
			}

			if font.mFontMetrics.mFirstChar > font.mFontMetrics.mLastChar {
				font.mFontMetrics.mLastChar = font.mFontMetrics.mFirstChar
			}

			if widthsArray, ok := font.mFontDictionary.Get("Widths").(*PdfObjectArray); ok {
				widthSlice := make([]uint, len(*widthsArray))
				for i := 0; i < len(*widthsArray); i++ {
					if v, ok := (*widthsArray)[i].(*PdfObjectInteger); ok {
						widthSlice[i] = uint(*v)
					}
				}

				font.mFontMetrics.mWidths = append(font.mFontMetrics.mWidths, widthSlice...)
			}

			font.loadFontDescriptor()
		}
	}

	return nil
}

type FontsByNames map[PdfObjectName]*Font

type PdfReader struct {
	parser        *PdfParser
	trailerDict   *PdfObjectDictionary
	root          *PdfObjectDictionary
	pages         *PdfObjectDictionary
	pageList      []*PdfIndirectObject
	pageResources []*PdfObjectDictionary

	mFonts          []*Font
	mFontsByIndexes map[uint]*Font
	mFontsForPages  []FontsByNames

	//PageList    []*PdfPage
	pageCount int
}

func NewPdfReader(rs io.ReadSeeker) (*PdfReader, error) {
	pdfReader := &PdfReader{}

	// Create the parser, loads the cross reference table and trailer.
	parser, err := NewParser(rs)
	if err != nil {
		return nil, err
	}
	pdfReader.parser = parser

	isEncrypted, err := pdfReader.parser.IsEncrypted()
	if err != nil {
		return nil, err
	}

	common.Log.Trace("this pdf encrypt: %v", isEncrypted)
	if isEncrypted {
		common.Log.Trace("encrypt info: %s", pdfReader.GetEncryptionMethod())
		if success, err := parser.Decrypt([]byte("")); err != nil {
			common.Log.Debug("error: decrypt failed, err: %s", err)
			return nil, err
		} else if !success {
			return nil, errors.New("decrypt use empty password failed")
		}
	}

	err = pdfReader.loadStructure()
	if err != nil {
		return nil, err
	}

	return pdfReader, nil
}

func (this *PdfReader) DumpFonts() {
	common.Log.Trace("fonts: %d", len(this.mFontsByIndexes))
	for index, f := range this.mFontsByIndexes {
		common.Log.Trace("index: %d, fonts: %s", index, f.mFontDictionary)
	}
}

func (this *PdfReader) ParseFonts() error {

	this.mFonts = []*Font{}
	this.mFontsForPages = []FontsByNames{}
	this.mFontsByIndexes = map[uint]*Font{}

	for i := 0; i < len(this.pageResources); i++ {
		fonts := make(FontsByNames)
		this.mFontsForPages = append(this.mFontsForPages, fonts)
		resDic := this.pageResources[i]
		if resDic == nil {
			continue
		}

		if obj, err := this.parser.Trace(resDic.Get("Font")); err == nil {
			fontsDict, ok := obj.(*PdfObjectDictionary)
			if !ok {
				common.Log.Debug("font obj is not dict, next page")
				continue
			}

			for fontName, fontValue := range fontsDict.Dict() {
				//fontValue maybe pdfObjectReference
				fontObj, err := this.traceToObject(fontValue)
				if err != nil {
					common.Log.Debug("Error: font trace to indirect obj failed, err: %s", err)
					return err
				}

				//common.Log.Debug("page: %d, fontName: %s\n", i, fontName)

				//fontValue is reference obj
				fontIndObj, ok := fontObj.(*PdfIndirectObject)
				if ok {
					refInd := fontIndObj.ObjectNumber
					font, exist := this.mFontsByIndexes[uint(refInd)]
					if exist {
						this.mFontsForPages[i][fontName] = font
					} else {
						font = new(Font)
						font.mFontDictionary, _ = fontIndObj.PdfObject.(*PdfObjectDictionary)
						this.mFontsByIndexes[uint(refInd)] = font

						//common.Log.Debug("font: %s", font.mFontDictionary)

						this.mFontsForPages[i][fontName] = font
						this.mFonts = append(this.mFonts, font)

						this.getFontEncoding(font)
						this.getFontInfo(font)
					}
					// fontValue is direct dictionary
				} else if fontObjDict, ok := fontObj.(*PdfObjectDictionary); ok {
					font := new(Font)
					font.mFontDictionary = fontObjDict

					this.mFontsForPages[i][fontName] = font
					this.mFonts = append(this.mFonts, font)

					this.getFontEncoding(font)
					this.getFontInfo(font)
				} else {
					return errors.New("unexpected font stream to parse")
				}
			}
		}
	}

	return nil
}

// Loads the structure of the pdf file: pages, outlines, etc.
func (this *PdfReader) loadStructure() error {
	if this.parser.GetCrypter() != nil && !this.parser.IsAuthenticated() {
		return errors.New("file need to be decrypted first")
	}

	// Pages.
	pagesRef, ok := this.parser.GetRootDict().Get("Pages").(*PdfObjectReference)
	if !ok {
		return errors.New("Pages in root should be a reference")
	}

	op, err := this.parser.LookupByReference(*pagesRef)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read pages")
		return err
	}

	ppages, ok := op.(*PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid, op: %p", ppages)
		return errors.New("Pages object invalid")
	}

	pages, ok := ppages.PdfObject.(*PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid (%s)", ppages)
		return errors.New("Pages object invalid")
	}

	pageCount, ok := pages.Get("Count").(*PdfObjectInteger)
	if !ok {
		common.Log.Debug("ERROR: Pages count object invalid")
		return errors.New("Pages count invalid")
	}

	this.root = this.parser.GetRootDict()
	this.pages = pages
	this.pageCount = int(*pageCount)
	this.pageList = []*PdfIndirectObject{}
	this.pageResources = []*PdfObjectDictionary{}

	traversedPageNodes := map[PdfObject]bool{}
	err = this.buildPageList(ppages, nil, nil, traversedPageNodes)
	if err != nil {
		return err
	}

	common.Log.Trace("pages, %d: %s", len(this.pageList), this.pageList)
	common.Log.Trace("resources, %d, %s", len(this.pageResources), this.pageResources)
	return nil
}

// Trace to object.  Keeps a list of already visited references to avoid circular references.
//
// Example circular reference.
// 1 0 obj << /Next 2 0 R >>
// 2 0 obj << /Next 1 0 R >>
//
func (this *PdfReader) traceToObjectWrapper(obj PdfObject, refList map[*PdfObjectReference]bool) (PdfObject, error) {
	// Keep a list of references to avoid circular references.

	ref, isRef := obj.(*PdfObjectReference)
	if isRef {
		// Make sure not already visited (circular ref).
		if _, alreadyTraversed := refList[ref]; alreadyTraversed {
			return nil, errors.New("Circular reference")
		}
		refList[ref] = true
		obj, err := this.parser.LookupByReference(*ref)
		if err != nil {
			return nil, err
		}
		return this.traceToObjectWrapper(obj, refList)
	}

	// Not a reference, an object.  Can be indirect or any direct pdf object (other than reference).
	return obj, nil
}

func (this *PdfReader) traceToObject(obj PdfObject) (PdfObject, error) {
	refList := map[*PdfObjectReference]bool{}
	return this.traceToObjectWrapper(obj, refList)
}

// Build the table of contents.
// tree, ex: Pages -> Pages -> Pages -> Page
// Traverse through the whole thing recursively.
func (this *PdfReader) buildPageList(node *PdfIndirectObject, parent *PdfIndirectObject,
	resource *PdfObjectDictionary, traversedPageNodes map[PdfObject]bool) error {

	if node == nil {
		return nil
	}

	if _, alreadyTraversed := traversedPageNodes[node]; alreadyTraversed {
		common.Log.Debug("Cyclic recursion, skipping")
		return nil
	}
	traversedPageNodes[node] = true

	nodeDict, ok := node.PdfObject.(*PdfObjectDictionary)
	if !ok {
		return errors.New("Node not a dictionary")
	}

	objType, ok := (*nodeDict).Get("Type").(*PdfObjectName)
	if !ok {
		return errors.New("Node missing Type (Required)")
	}
	common.Log.Trace("buildPageList node type: %s", *objType)

	// resources maybe reference obj
	if resourceObj, err := this.parser.Trace((*nodeDict).Get("Resources")); err == nil {
		if overrid, ok := resourceObj.(*PdfObjectDictionary); ok {
			resource = overrid
		}
	}

	if *objType != "Pages" && *objType != "Page" {
		common.Log.Debug("Error: Table of content containing non Page/Pages object! (%s)", objType)
		return errors.New("Table of content containing non Page/Pages object!")
	}

	// A Pages object.  Update the parent.
	if parent != nil {
		nodeDict.Set("Parent", parent)
	}

	if *objType == "Pages" {
		kidsArray, ok := (*nodeDict).Get("Kids").(*PdfObjectArray)
		if !ok {
			common.Log.Debug("Error: kids in pages is not array")
			return errors.New("kids in pages not array")
		}

		common.Log.Trace("Kids: %s, %d", kidsArray, len(*kidsArray))
		for i := 0; i < len(*kidsArray); i++ {
			obj, err := this.traceToObject((*kidsArray)[i])
			if err != nil {
				return err
			}
			child, ok := obj.(*PdfIndirectObject)
			if !ok {
				common.Log.Debug("kid not indirect object")
				return errors.New("kid not indiret object")
			}
			err = this.buildPageList(child, node, resource, traversedPageNodes)
			if err != nil {
				return err
			}
		}
	} else {
		if parent != nil {
			// Set the parent (in case missing or incorrect).
			nodeDict.Set("Parent", parent)
		}
		this.pageList = append(this.pageList, node)
		this.pageResources = append(this.pageResources, resource)

		return nil
	}

	return nil
}

// Returns a string containing some information about the encryption method used.
// Subject to changes.  May be better to return a standardized struct with information.
// But challenging due to the many different types supported.
func (this *PdfReader) GetEncryptionMethod() string {
	crypter := this.parser.GetCrypter()
	str := crypter.Filter + " - "

	if crypter.V == 0 {
		str += "Undocumented algorithm"
	} else if crypter.V == 1 {
		// RC4 or AES (bits: 40)
		str += "RC4: 40 bits"
	} else if crypter.V == 2 {
		str += fmt.Sprintf("RC4: %d bits", crypter.Length)
	} else if crypter.V == 3 {
		str += "Unpublished algorithm"
	} else if crypter.V == 4 {
		// Look at CF, StmF, StrF
		str += fmt.Sprintf("Stream filter: %s - String filter: %s", crypter.StreamFilter, crypter.StringFilter)
		str += "; Crypt filters:"
		for name, cf := range crypter.CryptFilters {
			str += fmt.Sprintf(" - %s: %s (%d)", name, cf.Cfm, cf.Length)
		}
	}
	perms := crypter.GetAccessPermissions()
	str += fmt.Sprintf(" - %#v", perms)

	return str
}

func (this *PdfReader) GetPageList() []*PdfIndirectObject {
	return this.pageList
}

func (this *PdfReader) GetParser() *PdfParser {
	return this.parser
}

func (this *PdfReader) GetFontsForPages() []FontsByNames {
	return this.mFontsForPages
}

func (this *PdfReader) GetPageResources() []*PdfObjectDictionary {
	return this.pageResources
}

func (this *PdfReader) GetTrailer() (*PdfObjectDictionary, error) {
	trailerDict := this.parser.GetTrailer()
	if trailerDict == nil {
		return nil, errors.New("Trailer missing")
	}

	return trailerDict, nil
}
