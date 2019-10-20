package control

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	_, err := New(1, 1, 1, 30*time.Second)
	assert.NotNil(t, err, "expected rejection of invalid parameters")
}

func TestMaintenanceA(t *testing.T) {
	cache, err := New(10, 6, 4, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	actions := cache.maintenance(now)
	assert.Empty(t, actions, "empty cache should require no maintenance")

	for i := 0; i < 3; i++ {
		cache.pages[i+1].lastWriteAccess = now
		cache.pages[i+1].state = cachedChanged
	}
	cache.cacheCount = 3
	actions = cache.maintenance(now)
	assert.Empty(t, actions, "recent writes should not trigger upload")

	actions = cache.maintenance(now.Add(time.Minute))
	assert.Equal(t, 3, len(actions), "expected three actions")
	for _, action := range actions {
		assert.Equal(t, startUpload, action.actionType, "expected upload action")
		assert.Equal(t, cachedUploading, cache.pages[action.page].state, "expected state change")
	}

	actions = cache.maintenance(now.Add(time.Minute))
	assert.Empty(t, actions, "should not trigger uploads again")
}

func TestMaintenanceB(t *testing.T) {
	cache, err := New(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	cache.pages[2].lastWriteAccess = now
	cache.pages[2].state = cachedUnchanged
	cache.cacheCount = 1

	actions := cache.maintenance(now)
	assert.Equal(t, 1, len(actions), "expected action when soft limit is hit")
	assert.Equal(t, deleteCache, actions[0].actionType, "expected delete action")
	assert.Equal(t, 0, cache.cacheCount, "expected cache count to be adjusted")
	assert.Equal(t, notCached, cache.pages[2].state, "expected state change")

	cache.pages[2].lastWriteAccess = now
	cache.pages[2].state = cachedChanged
	cache.cacheCount = 1

	actions = cache.maintenance(now)
	assert.Equal(t, 1, len(actions), "expected action when soft limit is hit")
	assert.Equal(t, startUpload, actions[0].actionType, "expected upload action")
	assert.Equal(t, cachedUploading, cache.pages[2].state, "expected state change")

	actions = cache.maintenance(now)
	assert.Empty(t, actions, "should not trigger upload again")
}

func TestPrepareAccessA(t *testing.T) {
	cache, err := New(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	cache.pages[2].state = zero
	actions := cache.prepareAccess(page(2), false, now)
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, zeroCache, actions[0].actionType)
	assert.Equal(t, cachedChanged, cache.pages[2].state)
	assert.Equal(t, 1, cache.cacheCount)

	cache.pages[1].state = notCached
	actions = cache.prepareAccess(page(1), false, now.Add(time.Second))
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, download, actions[0].actionType)
	assert.Equal(t, cachedUnchanged, cache.pages[1].state)
	assert.Equal(t, 2, cache.cacheCount)

	cache.pages[0].state = notCached
	actions = cache.prepareAccess(page(0), true, now.Add(2*time.Second))
	assert.Equal(t, 2, len(actions))
	assert.Equal(t, startUpload, actions[0].actionType)
	assert.Equal(t, cachedUploading, cache.pages[2].state)
	assert.Equal(t, 2, cache.cacheCount)
	assert.Equal(t, waitAndRetry, actions[1].actionType)

	cache.pages[2].state = cachedUnchanged
	actions = cache.prepareAccess(page(0), true, now.Add(3*time.Second))
	assert.Equal(t, 2, len(actions))
	assert.Equal(t, deleteCache, actions[0].actionType)
	assert.Equal(t, notCached, cache.pages[2].state)
	assert.Equal(t, 1, cache.cacheCount)
	assert.Equal(t, waitAndRetry, actions[1].actionType)

	actions = cache.prepareAccess(page(0), true, now.Add(4*time.Second))
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, download, actions[0].actionType)
	assert.Equal(t, cachedChanged, cache.pages[0].state)
	assert.Equal(t, 2, cache.cacheCount)
}

func TestPrepareAccessB(t *testing.T) {
	cache, err := New(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	cache.pages[2].state = cachedUnchanged
	cache.pages[2].lastAccess = now
	cache.pages[2].lastWriteAccess = now
	cache.cacheCount = 1

	actions := cache.prepareAccess(page(2), true, now)
	assert.Equal(t, 0, len(actions))
	assert.Equal(t, cachedChanged, cache.pages[2].state)

	actions = cache.prepareAccess(page(2), true, now)
	assert.Equal(t, 0, len(actions))

	cache.pages[2].state = cachedUploading
	actions = cache.prepareAccess(page(2), true, now)
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, cancelUpload, actions[0].actionType)
	assert.Equal(t, cachedChanged, cache.pages[2].state)
	assert.Equal(t, 1, cache.cacheCount)
}
