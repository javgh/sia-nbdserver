package siaadapter

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

type (
	SiaAdapter struct {
		mutex *sync.Mutex
		cache *cache
	}

	pageAccess struct {
		page      page
		offset    int64
		length    int
		sliceLow  int
		sliceHigh int
	}

	pageIODetails struct {
		file *os.File
	}

	cache struct {
		brain     *cacheBrain
		pageCount int
		pages     []pageIODetails
	}
)

const (
	pageSize             = 64 * 1024 * 1024
	defaultHardMaxCached = 32
	defaultSoftMaxCached = 16
	defaultIdleInterval  = 30 * time.Second
	waitInterval         = 5 * time.Second
)

func New(size uint64) (*SiaAdapter, error) {
	dataDirectory := prependDataDirectory("")
	log.Printf("Storing cache in %s", dataDirectory)
	err := os.MkdirAll(dataDirectory, 0700)
	if err != nil {
		return nil, err
	}

	pageCount := size / pageSize
	if size%pageSize > 0 {
		pageCount += 1
	}

	cacheBrain, err :=
		newCacheBrain(int(pageCount), defaultHardMaxCached, defaultSoftMaxCached, defaultIdleInterval)
	if err != nil {
		return nil, err
	}

	cache := cache{
		brain:     cacheBrain,
		pageCount: int(pageCount),
		pages:     make([]pageIODetails, pageCount),
	}

	siaAdapter := SiaAdapter{
		mutex: &sync.Mutex{},
		cache: &cache,
	}
	return &siaAdapter, nil
}

func (sa *SiaAdapter) ensureFileAccess(page page) error {
	if sa.cache.pages[page].file != nil {
		return nil
	}

	name := prependDataDirectory(fmt.Sprintf("page%d", page))
	file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil
	}

	sa.cache.pages[page].file = file
	return nil
}

func (sa *SiaAdapter) handleActions(actions []action) (bool, error) {
	for _, action := range actions {
		switch action.actionType {
		case zeroCache:
			log.Printf("Initializing cache for page %d with zeros\n", action.page)

			err := sa.ensureFileAccess(action.page)
			if err != nil {
				return false, err
			}

			b := make([]byte, pageSize)
			_, err = sa.cache.pages[action.page].file.Write(b)
			if err != nil {
				return false, err
			}
		case waitAndRetry:
			return true, nil
		default:
			log.Printf("unimplemented action %d on page %d\n", action.actionType, action.page)
		}
	}

	return false, nil
}

func (sa *SiaAdapter) ReadAt(b []byte, offset int64) (int, error) {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	n := 0
	for _, pageAccess := range determinePages(offset, len(b)) {
		for {
			actions := sa.cache.brain.prepareAccess(pageAccess.page, false, time.Now())
			retry, err := sa.handleActions(actions)
			if err != nil {
				return n, err
			}

			if !retry {
				break
			} else {
				sa.mutex.Unlock()
				time.Sleep(waitInterval)
				sa.mutex.Lock()
			}
		}

		err := sa.ensureFileAccess(pageAccess.page)
		if err != nil {
			return n, err
		}

		partialN, err := sa.cache.pages[pageAccess.page].file.ReadAt(
			b[pageAccess.sliceLow:pageAccess.sliceHigh], pageAccess.offset)
		n += partialN
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (sa *SiaAdapter) WriteAt(b []byte, offset int64) (int, error) {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	n := 0
	for _, pageAccess := range determinePages(offset, len(b)) {
		for {
			actions := sa.cache.brain.prepareAccess(pageAccess.page, true, time.Now())
			retry, err := sa.handleActions(actions)
			if err != nil {
				return n, err
			}

			if !retry {
				break
			} else {
				sa.mutex.Unlock()
				time.Sleep(waitInterval)
				sa.mutex.Lock()
			}
		}

		err := sa.ensureFileAccess(pageAccess.page)
		if err != nil {
			return n, err
		}

		partialN, err := sa.cache.pages[pageAccess.page].file.WriteAt(
			b[pageAccess.sliceLow:pageAccess.sliceHigh], pageAccess.offset)
		n += partialN
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (sa *SiaAdapter) Close() error {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	return nil
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

func prependDataDirectory(path string) string {
	if os.Getenv("XDG_DATA_HOME") != "" {
		return filepath.Join(os.Getenv("XDG_DATA_HOME"), "sia-nbdserver", path)
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	return filepath.Join(currentUser.HomeDir, ".local/share/sia-nbdserver", path)
}
