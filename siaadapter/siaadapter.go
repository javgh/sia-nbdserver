package siaadapter

import (
	"fmt"
)

type (
	SiaAdapter struct{}

	pageAccess struct {
		page      page
		offset    int64
		length    int
		sliceLow  int
		sliceHigh int
	}
)

const (
	pageSize = 64 * 1024 * 1024
)

func New() *SiaAdapter {
	siaAdapter := SiaAdapter{}
	return &siaAdapter
}

func (sa *SiaAdapter) ReadAt(b []byte, offset int64) (int, error) {
	//fmt.Println("in ReadAt:", len(b), offset)
	for _, pageAccess := range determinePages(offset, len(b)) {
		fmt.Printf("%dr ", pageAccess.page)
	}
	return len(b), nil
}

func (sa *SiaAdapter) WriteAt(b []byte, offset int64) (int, error) {
	//fmt.Println("in WriteAt:", len(b), offset)
	for _, pageAccess := range determinePages(offset, len(b)) {
		fmt.Printf("%dw ", pageAccess.page)
	}
	return len(b), nil
}

func determinePages(offset int64, length int) []pageAccess {
	pageAccesses := []pageAccess{}

	slicePos := 0
	for length > 0 {
		page := page(offset / pageSize)
		pageOffset := offset % pageSize
		remainingPageLength := pageSize - pageOffset
		accessLength := min(int(remainingPageLength), length)

		pageAccesses = append(pageAccesses, pageAccess{
			page:      page,
			offset:    pageOffset,
			length:    accessLength,
			sliceLow:  slicePos,
			sliceHigh: slicePos + accessLength,
		})

		offset += int64(accessLength)
		length -= accessLength
		slicePos += accessLength
	}

	return pageAccesses
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
