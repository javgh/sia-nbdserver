package siaadapter

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node/api/client"

	"github.com/javgh/sia-nbdserver/config"
)

type (
	SiaAdapter struct {
		mutex      *sync.Mutex
		cache      *cache
		httpClient *client.Client
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
	defaultHardMaxCached = 32 //5
	defaultSoftMaxCached = 16 //3
	defaultIdleInterval  = 30 * time.Second
	waitInterval         = 5 * time.Second
	defaultDataPieces    = 10
	defaultParityPieces  = 20
	minimumRedundancy    = 2.5
)

var (
	siaDaemonAddress = "localhost:9980"
	siaPasswordFile  = config.PrependHomeDirectory(".sia/apipassword")
	siaPathPrefix    = "nbd"
)

func New(size uint64) (*SiaAdapter, error) {
	dataDirectory := config.PrependDataDirectory("")
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

	siaPassword, err := config.ReadPasswordFile(siaPasswordFile)
	if err != nil {
		return nil, err
	}

	httpClient := client.Client{
		Address:  siaDaemonAddress,
		Password: siaPassword,
	}

	uploadedPages, err := getUploadedPages(&httpClient, false)
	if err != nil {
		return nil, err
	}

	for _, page := range uploadedPages {
		cache.brain.pages[page].state = notCached
	}

	cachedPages := getCachedPages(int(pageCount))
	for _, page := range cachedPages {
		log.Printf("Cache for page %d found - assuming it may contain new data\n", page)
		cache.brain.pages[page].state = cachedChanged
	}

	siaAdapter := SiaAdapter{
		mutex:      &sync.Mutex{},
		cache:      &cache,
		httpClient: &httpClient,
	}

	go func() {
		for {
			time.Sleep(waitInterval)
			_ = siaAdapter.maintenance()
		}
	}()

	return &siaAdapter, nil
}

func (sa *SiaAdapter) ensureFileIsOpen(page page) error {
	if sa.cache.pages[page].file != nil {
		return nil
	}

	file, err := os.OpenFile(asCachePath(page), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	sa.cache.pages[page].file = file
	return nil
}

func (sa *SiaAdapter) ensureFileIsClosed(page page) error {
	if sa.cache.pages[page].file == nil {
		return nil
	}

	err := sa.cache.pages[page].file.Close()
	if err != nil {
		return err
	}

	sa.cache.pages[page].file = nil
	return nil
}

func (sa *SiaAdapter) handleActions(actions []action) (bool, error) {
	for _, action := range actions {
		switch action.actionType {
		case zeroCache:
			log.Printf("Initializing cache for page %d with zeros\n", action.page)

			err := sa.ensureFileIsOpen(action.page)
			if err != nil {
				return false, err
			}

			b := make([]byte, pageSize)
			_, err = sa.cache.pages[action.page].file.Write(b)
			if err != nil {
				return false, err
			}
		case deleteCache:
			log.Printf("Deleting cache for page %d\n", action.page)

			err := sa.ensureFileIsClosed(action.page)
			if err != nil {
				return false, err
			}

			cachePath := asCachePath(action.page)
			err = os.Remove(cachePath)
			if err != nil {
				return false, err
			}
		case download:
			log.Printf("Downloading page %d\n", action.page)

			siaPath, err := modules.NewSiaPath(asSiaPath(action.page))
			if err != nil {
				return false, err
			}

			cachePath := asCachePath(action.page)
			_, err = sa.httpClient.RenterDownloadFullGet(siaPath, cachePath, false)
			if err != nil {
				return false, err
			}
		case startUpload:
			log.Printf("Uploading page %d\n", action.page)

			siaPath, err := modules.NewSiaPath(asSiaPath(action.page))
			if err != nil {
				return false, err
			}

			cachePath := asCachePath(action.page)
			err = sa.httpClient.RenterUploadForcePost(
				cachePath, siaPath, defaultDataPieces, defaultParityPieces, true)
			if err != nil {
				return false, err
			}
		case postponeUpload:
			log.Printf("Postponing upload for page %d\n", action.page)

			siaPath, err := modules.NewSiaPath(asSiaPath(action.page))
			if err != nil {
				return false, err
			}

			err = sa.httpClient.RenterDeletePost(siaPath)
			if err != nil {
				return false, err
			}
		case waitAndRetry:
			return true, nil
		default:
			panic("unknown action")
		}
	}

	return false, nil
}

func (sa *SiaAdapter) maintenance() error {
	sa.mutex.Lock()
	defer sa.mutex.Unlock()

	actions := sa.cache.brain.maintenance(time.Now())
	_, err := sa.handleActions(actions)
	if err != nil {
		return err
	}

	anyUploading := false
	for i := 0; i < sa.cache.brain.pageCount; i++ {
		if sa.cache.brain.pages[i].state == cachedUploading {
			anyUploading = true
			break
		}
	}

	if !anyUploading {
		return nil
	}

	uploadedPages, err := getUploadedPages(sa.httpClient, true)
	if err != nil {
		return err
	}

	for _, page := range uploadedPages {
		if sa.cache.brain.pages[page].state == cachedUploading {
			log.Printf("Upload complete for page %d\n", page)
			sa.cache.brain.pages[page].state = cachedUnchanged
		}
	}

	return nil
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

		err := sa.ensureFileIsOpen(pageAccess.page)
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

		err := sa.ensureFileIsOpen(pageAccess.page)
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

func getUploadedPages(httpClient *client.Client, checkRedundancy bool) ([]page, error) {
	pages := []page{}

	renterFiles, err := httpClient.RenterFilesGet(false)
	if err != nil {
		return pages, err
	}

	for _, fileInfo := range renterFiles.Files {
		if !isRelevantSiaPath(fileInfo.SiaPath.String()) {
			continue
		}

		page, err := getPageFromSiaPath(fileInfo.SiaPath.String())
		if err != nil {
			return pages, err
		}

		uploadComplete := fileInfo.Available && fileInfo.Recoverable &&
			(!checkRedundancy || fileInfo.Redundancy >= minimumRedundancy)
		if uploadComplete {
			pages = append(pages, page)
		}
	}

	return pages, nil
}

func getCachedPages(pageCount int) []page {
	pages := []page{}

	for i := 0; i < pageCount; i++ {
		cachePath := asCachePath(page(i))

		if fileCanBeStated(cachePath) {
			pages = append(pages, page(i))
		}
	}

	return pages
}

func fileCanBeStated(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func asSiaPath(page page) string {
	return fmt.Sprintf("%s/page%d", siaPathPrefix, page)
}

func asCachePath(page page) string {
	return config.PrependDataDirectory(fmt.Sprintf("page%d", page))
}

func isRelevantSiaPath(siaPath string) bool {
	return strings.HasPrefix(siaPath, fmt.Sprintf("%s/page", siaPathPrefix))
}

func getPageFromSiaPath(siaPath string) (page, error) {
	var page page

	format := fmt.Sprintf("%s/page%%d", siaPathPrefix)
	_, err := fmt.Sscanf(siaPath, format, &page)
	if err != nil {
		return 0, err
	}

	return page, nil
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
