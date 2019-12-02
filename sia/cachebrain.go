package sia

import (
	"errors"
	"sort"
	"time"
)

type (
	state int

	page int

	pageDetails struct {
		state      state
		lastAccess time.Time
	}

	lastAccessDetails struct {
		lastAccess time.Time
		page       page
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
	postponeUpload
	openFile
	closeFile
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
	accesses := []lastAccessDetails{}

	uploadingCount := 0
	for i := 0; i < cb.pageCount; i++ {
		if !isCached(cb.pages[i].state) {
			continue
		}

		if cb.pages[i].state == cachedUploading {
			uploadingCount += 1
		}

		accesses = append(accesses, lastAccessDetails{
			lastAccess: cb.pages[i].lastAccess,
			page:       page(i),
		})
	}

	// sort cached pages by oldest to newest access
	sort.Slice(accesses, func(i, j int) bool {
		return accesses[i].lastAccess.Before(accesses[j].lastAccess)
	})

	for i, access := range accesses {
		// Define recent activity as being in the youngest 1/3 of the cache.
		hasRecentActivity := i > ((cb.softMaxCached * 2) / 3)
		isIdle := now.After(access.lastAccess.Add(cb.idleInterval))
		softLimitReached := cb.cacheCount >= cb.softMaxCached

		switch cb.pages[access.page].state {
		case cachedUnchanged:
			if softLimitReached && !hasRecentActivity {
				actions = append(actions, action{
					actionType: closeFile,
					page:       access.page,
				})
				actions = append(actions, action{
					actionType: deleteCache,
					page:       access.page,
				})
				cb.pages[access.page].state = notCached
				cb.cacheCount -= 1
			}
		case cachedChanged:
			if (softLimitReached && !hasRecentActivity) || isIdle {
				actions = append(actions, action{
					actionType: startUpload,
					page:       access.page,
				})
				cb.pages[access.page].state = cachedUploading
				uploadingCount += 1
			}
		}
	}

	return actions
}

func (cb *cacheBrain) prepareAccess(page page, isWrite bool, now time.Time) []action {
	actions := []action{}

	if !isCached(cb.pages[page].state) && cb.cacheCount >= cb.hardMaxCached {
		// wait for maintenance to free up some space first
		actions = append(actions, action{
			actionType: waitAndRetry,
		})
		return actions
	}

	switch cb.pages[page].state {
	case zero:
		actions = append(actions, action{
			actionType: openFile,
			page:       page,
		})
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
		actions = append(actions, action{
			actionType: openFile,
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
				actionType: postponeUpload,
				page:       page,
			})
			cb.pages[page].state = cachedChanged
		}
	default:
		panic("unknown state")
	}

	cb.pages[page].lastAccess = now
	return actions
}

func (cb *cacheBrain) prepareShutdown(thorough bool) []action {
	actions := []action{}

	for i := 0; i < cb.pageCount; i++ {
		switch cb.pages[i].state {
		case cachedUnchanged:
			actions = append(actions, action{
				actionType: closeFile,
				page:       page(i),
			})
			actions = append(actions, action{
				actionType: deleteCache,
				page:       page(i),
			})
			cb.pages[i].state = notCached
			cb.cacheCount -= 1
		case cachedChanged:
			if thorough {
				actions = append(actions, action{
					actionType: startUpload,
					page:       page(i),
				})
				cb.pages[i].state = cachedUploading
			}
		case cachedUploading:
			if !thorough {
				actions = append(actions, action{
					actionType: postponeUpload,
					page:       page(i),
				})
				cb.pages[i].state = cachedChanged
			}
		}
	}

	if thorough && cb.cacheCount > 0 {
		actions = append(actions, action{
			actionType: waitAndRetry,
		})
	}

	return actions
}

func isCached(state state) bool {
	return state == cachedUnchanged || state == cachedChanged || state == cachedUploading
}
