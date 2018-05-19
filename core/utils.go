package core

import (
	"errors"
	//"fmt"
	//"sort"
	//"github.com/unidoc/unidoc/common"
)

// Check slice range to make sure within bounds for accessing:
//    slice[a:b] where sliceLen=len(slice).
func checkBounds(sliceLen, a, b int) error {
	if a < 0 || a > sliceLen {
		return errors.New("Slice index a out of bounds")
	}
	if b < a {
		return errors.New("Invalid slice index b < a")
	}
	if b > sliceLen {
		return errors.New("Slice index b out of bounds")
	}

	return nil
}

func absInt(x int) int {
	if x < 0 {
		return -x
	} else {
		return x
	}
}
