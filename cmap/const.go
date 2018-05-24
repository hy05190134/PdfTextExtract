/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package cmap

import "regexp"

const (
	cisSystemInfo       = "/CIDSystemInfo"
	begincodespacerange = "begincodespacerange"
	endcodespacerange   = "endcodespacerange"
	beginbfchar         = "beginbfchar"
	endbfchar           = "endbfchar"
	beginbfrange        = "beginbfrange"
	endbfrange          = "endbfrange"
	beginnotdefrange    = "beginnotdefrange"
	endnotdefrange      = "endnotdefrange"

	begincidrange = "begincidrange"
	endcidrange   = "endcidrange"

	begincidchar = "begincidchar"
	endcidchar   = "endcidchar"

	cmapname = "CMapName"
	cmaptype = "CMapType"
)

var reNumeric = regexp.MustCompile(`^[\+-.]*([0-9.]+)`)
