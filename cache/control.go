package control

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

	Cache struct {
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

func New(pageCount int, hardMaxCached int, softMaxCached int, idleInterval time.Duration) (*Cache, error) {
	if softMaxCached >= hardMaxCached {
		return nil, errors.New("soft limit needs to be lower than hard limit")
	}

	cache := Cache{
		pageCount:     pageCount,
		cacheCount:    0,
		hardMaxCached: hardMaxCached,
		softMaxCached: softMaxCached,
		idleInterval:  idleInterval,
		pages:         make([]pageDetails, pageCount),
	}
	return &cache, nil
}

func (c *Cache) maintenance(now time.Time) []action {
	actions := []action{}
	hasOldestCachedPage := false
	var oldestCachedPage page
	var oldestAccess time.Time

	for i := 0; i < c.pageCount; i++ {
		if !isCached(c.pages[i].state) {
			continue
		}

		if !hasOldestCachedPage || oldestAccess.After(c.pages[i].lastAccess) {
			hasOldestCachedPage = true
			oldestCachedPage = page(i)
			oldestAccess = c.pages[i].lastAccess
		}

		if c.pages[i].state != cachedChanged {
			continue
		}

		if now.After(c.pages[i].lastWriteAccess.Add(c.idleInterval)) {
			actions = append(actions, action{
				actionType: startUpload,
				page:       page(i),
			})
			c.pages[i].state = cachedUploading
		}
	}

	// Return here if we already have something to do
	// or if we haven't reached our soft limit yet.
	if len(actions) > 0 || c.cacheCount < c.softMaxCached {
		return actions
	}

	switch c.pages[oldestCachedPage].state {
	case cachedUnchanged:
		actions = append(actions, action{
			actionType: deleteCache,
			page:       oldestCachedPage,
		})
		c.pages[oldestCachedPage].state = notCached
		c.cacheCount -= 1
	case cachedChanged:
		actions = append(actions, action{
			actionType: startUpload,
			page:       oldestCachedPage,
		})
		c.pages[oldestCachedPage].state = cachedUploading
	}

	return actions
}

func (c *Cache) prepareAccess(page page, isWrite bool, now time.Time) []action {
	actions := []action{}

	if !isCached(c.pages[page].state) && c.cacheCount >= c.hardMaxCached {
		// need to free up some space first
		actions = c.maintenance(now)
		actions = append(actions, action{
			actionType: waitAndRetry,
		})
		return actions
	}

	switch c.pages[page].state {
	case zero:
		actions = append(actions, action{
			actionType: zeroCache,
			page:       page,
		})
		c.pages[page].state = cachedChanged
		c.cacheCount += 1
	case notCached:
		actions = append(actions, action{
			actionType: download,
			page:       page,
		})
		if isWrite {
			c.pages[page].state = cachedChanged
		} else {
			c.pages[page].state = cachedUnchanged
		}
		c.cacheCount += 1
	case cachedUnchanged:
		if isWrite {
			c.pages[page].state = cachedChanged
		}
	case cachedChanged:
		// no changes
	case cachedUploading:
		if isWrite {
			actions = append(actions, action{
				actionType: cancelUpload,
				page:       page,
			})
			c.pages[page].state = cachedChanged
		}
	default:
		panic("unknown state")
	}

	c.pages[page].lastAccess = now
	if isWrite {
		c.pages[page].lastWriteAccess = now
	}

	return actions
}

func isCached(state state) bool {
	return state == cachedUnchanged || state == cachedChanged || state == cachedUploading
}
