/*
 * PDF to text: Extract all text for each page of a pdf file.
 *
 * Run as: go run pdf_extract_text.go input.pdf
 */

package main

import (
	common "./common"
	. "./core"
	. "./extractor"
	pdf "./model"
	"fmt"
	"io/ioutil"
	"os"
	//"runtime"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: go run pdf_extract_text.go input.pdf\n")
		os.Exit(1)
	}

	// For debugging.
	common.SetLogger(common.NewConsoleLogger(common.LogLevelDebug))

	inputPath := os.Args[1]

	/*
		    m := new(runtime.MemStats)
			runtime.GC()
			runtime.ReadMemStats(m)
			fmt.Printf("before load, heap memory: %d, head in use: %d\n", m.HeapAlloc, m.HeapInuse)
	*/
	text, err := ExtractPdfFile(inputPath)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}

	fmt.Println(text)
	fmt.Println("--------------")

	fi, err := os.Open(inputPath)
	if err != nil {
		panic(err)
	}
	defer fi.Close()
	fd, err := ioutil.ReadAll(fi)
	content := string(fd)
	text, err = ExtractPdfContent(content)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}

	fmt.Println(text)
	/*
		runtime.GC()
		runtime.ReadMemStats(m)
		fmt.Printf("after load, heap memory: %d, head in use: %d\n", m.HeapAlloc, m.HeapInuse)
	*/
}

type ContentPair struct {
	s     *PdfObjectStream
	index int
}

func parseText(this *pdf.PdfReader) (string, error) {
	text := ""
	pageList := this.GetPageList()
	parser := this.GetParser()
	mFontsForPages := this.GetFontsForPages()

	contentStreamChan := make(chan ContentPair, 10)

	go func() {
		for i := 2; i < len(pageList); i++ {
			if pageObjDict, ok := pageList[i].PdfObject.(*PdfObjectDictionary); ok {
				if contentsArray, ok := pageObjDict.Get("Contents").(*PdfObjectArray); ok {
					for j := 0; j < len(*contentsArray); j++ {
						contentObj, err := parser.Trace((*contentsArray)[j])
						if err != nil {
							common.Log.Debug("Error: trace content to obj failed, err: %s", err)
							continue
						}
						if contentStmObj, ok := contentObj.(*PdfObjectStream); ok {
							contentStreamChan <- ContentPair{contentStmObj, i}
						}
					}
				} else if contentObj, err := parser.Trace(pageObjDict.Get("Contents")); err == nil {
					if contentStmObj, ok := contentObj.(*PdfObjectStream); ok {
						contentStreamChan <- ContentPair{contentStmObj, i}
					}
				}
			}
			break
		}
		close(contentStreamChan)
	}()

	for {
		if pair, ok := <-contentStreamChan; ok {
			streamData, err := DecodeStream(pair.s)
			if err != nil {
				return "", err
			}

			//common.Log.Debug("stream data: %s", streamData)

			e := New(string(streamData), mFontsForPages[pair.index])
			s, _ := e.ExtractText()
			text += s
			text += "\n\n"
		} else {
			break
		}
	}

	return text, nil
}

// outputPdfText prints out contents of PDF file to stdout.
func ExtractPdfContent(content string) (string, error) {

	f := strings.NewReader(content)

	pdfReader, err := pdf.NewPdfReader(f)

	if err != nil {
		fmt.Printf("parser pdf failed, err: %s\n", err)
		return "", err
	}

	err = pdfReader.ParseFonts()
	if err != nil {
		fmt.Printf("parse fonts err: %s\n", err)
		return "", err
	}

	text, err := parseText(pdfReader)

	return text, err
}

// outputPdfText prints out contents of PDF file to stdout.
func ExtractPdfFile(inputPath string) (string, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return "", err
	}

	defer f.Close()

	pdfReader, err := pdf.NewPdfReader(f)

	if err != nil {
		fmt.Printf("parser pdf failed, err: %s\n", err)
		return "", err
	}

	err = pdfReader.ParseFonts()
	if err != nil {
		fmt.Printf("parse fonts err: %s\n", err)
		return "", err
	}

	text, err := parseText(pdfReader)

	return text, err
}
