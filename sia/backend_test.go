package sia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeterminePages(t *testing.T) {
	pageAccesses := determinePages(3, 5)
	assert.Equal(t, 1, len(pageAccesses))
	expectedPageAccess := pageAccess{
		page:      0,
		offset:    3,
		length:    5,
		sliceLow:  0,
		sliceHigh: 5,
	}
	assert.Equal(t, expectedPageAccess, pageAccesses[0])

	pageAccesses = determinePages(60000000, 10000000)
	assert.Equal(t, 2, len(pageAccesses))
	expectedFirstPageAccess := pageAccess{
		page:      0,
		offset:    60000000,
		length:    7108864,
		sliceLow:  0,
		sliceHigh: 7108864,
	}
	assert.Equal(t, expectedFirstPageAccess, pageAccesses[0])
	expectedSecondPageAccess := pageAccess{
		page:      1,
		offset:    0,
		length:    2891136,
		sliceLow:  7108864,
		sliceHigh: 10000000,
	}
	assert.Equal(t, expectedSecondPageAccess, pageAccesses[1])

	pageAccesses = determinePages(2*pageSize-1, pageSize+2)
	assert.Equal(t, 3, len(pageAccesses))
	expectedFirstPageAccess = pageAccess{
		page:      1,
		offset:    pageSize - 1,
		length:    1,
		sliceLow:  0,
		sliceHigh: 1,
	}
	assert.Equal(t, expectedFirstPageAccess, pageAccesses[0])
	expectedSecondPageAccess = pageAccess{
		page:      2,
		offset:    0,
		length:    pageSize,
		sliceLow:  1,
		sliceHigh: 1 + pageSize,
	}
	assert.Equal(t, expectedSecondPageAccess, pageAccesses[1])
	expectedThirdPageAccess := pageAccess{
		page:      3,
		offset:    0,
		length:    1,
		sliceLow:  1 + pageSize,
		sliceHigh: 1 + pageSize + 1,
	}
	assert.Equal(t, expectedThirdPageAccess, pageAccesses[2])
}
