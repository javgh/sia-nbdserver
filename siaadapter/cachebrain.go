package siaadapter

import (
	"errors"
	"time"
)

type (
	state int

	page int

	pageDetails struct {
		state           state
		lastAccess      time.Time
		lastWriteAccess time.Time
	}

	cacheBrain struct {
		pageCount     int
		cacheCount    int
		hardMaxCached int
		softMaxCached int
		idleInterval  time.Duration
		pages         []pageDetails
	}

	actionType int

	action struct {
		actionType actionType
		page       page
	}
)

const (
	zero state = iota
	notCached
	cachedUnchanged
	cachedChanged
	cachedUploading
)

const (
	zeroCache actionType = iota
	deleteCache
	download
	startUpload
	cancelUpload
	waitAndRetry
)

func newCacheBrain(pageCount int, hardMaxCached int, softMaxCached int,
	idleInterval time.Duration) (*cacheBrain, error) {
	if softMaxCached >= hardMaxCached {
		return nil, errors.New("soft limit needs to be lower than hard limit")
	}

	cacheBrain := cacheBrain{
		pageCount:     pageCount,
		cacheCount:    0,
		hardMaxCached: hardMaxCached,
		softMaxCached: softMaxCached,
		idleInterval:  idleInterval,
		pages:         make([]pageDetails, pageCount),
	}
	return &cacheBrain, nil
}

func (cb *cacheBrain) maintenance(now time.Time) []action {
	actions := []action{}
	hasOldestCachedPage := false
	var oldestCachedPage page
	var oldestAccess time.Time

	for i := 0; i < cb.pageCount; i++ {
		if !isCached(cb.pages[i].state) {
			continue
		}

		if !hasOldestCachedPage || oldestAccess.After(cb.pages[i].lastAccess) {
			hasOldestCachedPage = true
			oldestCachedPage = page(i)
			oldestAccess = cb.pages[i].lastAccess
		}

		if cb.pages[i].state != cachedChanged {
			continue
		}

		if now.After(cb.pages[i].lastWriteAccess.Add(cb.idleInterval)) {
			actions = append(actions, action{
				actionType: startUpload,
				page:       page(i),
			})
			cb.pages[i].state = cachedUploading
		}
	}

	// Return here if we already have something to do
	// or if we haven't reached our soft limit yet.
	if len(actions) > 0 || cb.cacheCount < cb.softMaxCached {
		return actions
	}

	switch cb.pages[oldestCachedPage].state {
	case cachedUnchanged:
		actions = append(actions, action{
			actionType: deleteCache,
			page:       oldestCachedPage,
		})
		cb.pages[oldestCachedPage].state = notCached
		cb.cacheCount -= 1
	case cachedChanged:
		actions = append(actions, action{
			actionType: startUpload,
			page:       oldestCachedPage,
		})
		cb.pages[oldestCachedPage].state = cachedUploading
	}

	return actions
}

func (cb *cacheBrain) prepareAccess(page page, isWrite bool, now time.Time) []action {
	actions := []action{}

	if !isCached(cb.pages[page].state) && cb.cacheCount >= cb.hardMaxCached {
		// need to free up some space first
		actions = cb.maintenance(now)
		actions = append(actions, action{
			actionType: waitAndRetry,
		})
		return actions
	}

	switch cb.pages[page].state {
	case zero:
		actions = append(actions, action{
			actionType: zeroCache,
			page:       page,
		})
		cb.pages[page].state = cachedChanged
		cb.cacheCount += 1
	case notCached:
		actions = append(actions, action{
			actionType: download,
			page:       page,
		})
		if isWrite {
			cb.pages[page].state = cachedChanged
		} else {
			cb.pages[page].state = cachedUnchanged
		}
		cb.cacheCount += 1
	case cachedUnchanged:
		if isWrite {
			cb.pages[page].state = cachedChanged
		}
	case cachedChanged:
		// no changes
	case cachedUploading:
		if isWrite {
			actions = append(actions, action{
				actionType: cancelUpload,
				page:       page,
			})
			cb.pages[page].state = cachedChanged
		}
	default:
		panic("unknown state")
	}

	cb.pages[page].lastAccess = now
	if isWrite {
		cb.pages[page].lastWriteAccess = now
	}

	return actions
}

func isCached(state state) bool {
	return state == cachedUnchanged || state == cachedChanged || state == cachedUploading
}
