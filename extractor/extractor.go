/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import "../model"

// Extractor stores and offers functionality for extracting content from PDF pages.
type Extractor struct {
	contents     string
	fontNamesMap model.FontsByNames
}

// New returns an Extractor instance for extracting content from the input PDF page.
func New(contents string, f model.FontsByNames) *Extractor {
	e := &Extractor{}
	e.contents = contents
	e.fontNamesMap = f

	return e
}
