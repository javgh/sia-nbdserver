package siaadapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	_, err := newCacheBrain(1, 1, 1, 30*time.Second)
	assert.NotNil(t, err, "expected rejection of invalid parameters")
}

func TestMaintenanceA(t *testing.T) {
	cacheBrain, err := newCacheBrain(10, 6, 4, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	actions := cacheBrain.maintenance(now)
	assert.Empty(t, actions, "empty cache should require no maintenance")

	for i := 0; i < 3; i++ {
		cacheBrain.pages[i+1].lastAccess = now
		cacheBrain.pages[i+1].state = cachedChanged
	}
	cacheBrain.cacheCount = 3
	actions = cacheBrain.maintenance(now)
	assert.Empty(t, actions, "recent writes should not trigger upload")

	actions = cacheBrain.maintenance(now.Add(time.Minute))
	assert.Equal(t, 3, len(actions), "expected three actions")
	for _, action := range actions {
		assert.Equal(t, startUpload, action.actionType, "expected upload action")
		assert.Equal(t, cachedUploading, cacheBrain.pages[action.page].state, "expected state change")
	}

	actions = cacheBrain.maintenance(now.Add(time.Minute))
	assert.Empty(t, actions, "should not trigger uploads again")
}

func TestMaintenanceB(t *testing.T) {
	cacheBrain, err := newCacheBrain(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	cacheBrain.pages[2].lastAccess = now
	cacheBrain.pages[2].state = cachedUnchanged
	cacheBrain.cacheCount = 1

	actions := cacheBrain.maintenance(now)
	assert.Equal(t, 1, len(actions), "expected action when soft limit is hit")
	assert.Equal(t, deleteCache, actions[0].actionType, "expected delete action")
	assert.Equal(t, 0, cacheBrain.cacheCount, "expected cache count to be adjusted")
	assert.Equal(t, notCached, cacheBrain.pages[2].state, "expected state change")

	cacheBrain.pages[2].lastAccess = now
	cacheBrain.pages[2].state = cachedChanged
	cacheBrain.cacheCount = 1

	actions = cacheBrain.maintenance(now)
	assert.Equal(t, 1, len(actions), "expected action when soft limit is hit")
	assert.Equal(t, startUpload, actions[0].actionType, "expected upload action")
	assert.Equal(t, cachedUploading, cacheBrain.pages[2].state, "expected state change")

	actions = cacheBrain.maintenance(now)
	assert.Empty(t, actions, "should not trigger upload again")
}

func TestMaintenanceC(t *testing.T) {
	cacheBrain, err := newCacheBrain(10, 6, 4, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for i := 0; i < 5; i++ {
		cacheBrain.pages[i+1].lastAccess = now
		cacheBrain.pages[i+1].state = cachedChanged
	}
	cacheBrain.cacheCount = 5

	actions := cacheBrain.maintenance(now.Add(time.Minute))
	assert.Equal(t, 3, len(actions), "expected only three actions because of upload limit")
	for _, action := range actions {
		assert.Equal(t, startUpload, action.actionType, "expected upload action")
		assert.Equal(t, cachedUploading, cacheBrain.pages[action.page].state, "expected state change")
	}

	cacheBrain.pages[actions[0].page].state = cachedChanged
	actions = cacheBrain.maintenance(now.Add(time.Minute))
	assert.Equal(t, 1, len(actions), "expected additional action after upload completes")
	assert.Equal(t, startUpload, actions[0].actionType, "expected upload action")

	actions = cacheBrain.maintenance(now.Add(time.Minute))
	assert.Empty(t, actions, "should not trigger uploads again")
}

func TestPrepareAccessA(t *testing.T) {
	cacheBrain, err := newCacheBrain(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	cacheBrain.pages[2].state = zero
	actions := cacheBrain.prepareAccess(page(2), false, now)
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, zeroCache, actions[0].actionType)
	assert.Equal(t, cachedChanged, cacheBrain.pages[2].state)
	assert.Equal(t, 1, cacheBrain.cacheCount)

	cacheBrain.pages[1].state = notCached
	actions = cacheBrain.prepareAccess(page(1), false, now.Add(time.Second))
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, download, actions[0].actionType)
	assert.Equal(t, cachedUnchanged, cacheBrain.pages[1].state)
	assert.Equal(t, 2, cacheBrain.cacheCount)

	cacheBrain.pages[0].state = notCached
	actions = cacheBrain.prepareAccess(page(0), true, now.Add(2*time.Second))
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, waitAndRetry, actions[0].actionType)

	actions = cacheBrain.maintenance(now.Add(3 * time.Second))
	assert.Equal(t, 2, len(actions))
	assert.Equal(t, startUpload, actions[0].actionType)
	assert.Equal(t, cachedUploading, cacheBrain.pages[2].state)
	assert.Equal(t, deleteCache, actions[1].actionType)
	assert.Equal(t, notCached, cacheBrain.pages[1].state)
	assert.Equal(t, 1, cacheBrain.cacheCount)

	cacheBrain.pages[2].state = cachedUnchanged
	actions = cacheBrain.prepareAccess(page(0), true, now.Add(4*time.Second))
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, download, actions[0].actionType)
	assert.Equal(t, cachedChanged, cacheBrain.pages[0].state)
	assert.Equal(t, 2, cacheBrain.cacheCount)
}

func TestPrepareAccessB(t *testing.T) {
	cacheBrain, err := newCacheBrain(3, 2, 1, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	cacheBrain.pages[2].state = cachedUnchanged
	cacheBrain.pages[2].lastAccess = now
	cacheBrain.cacheCount = 1

	actions := cacheBrain.prepareAccess(page(2), true, now)
	assert.Equal(t, 0, len(actions))
	assert.Equal(t, cachedChanged, cacheBrain.pages[2].state)

	actions = cacheBrain.prepareAccess(page(2), true, now)
	assert.Equal(t, 0, len(actions))

	cacheBrain.pages[2].state = cachedUploading
	actions = cacheBrain.prepareAccess(page(2), true, now)
	assert.Equal(t, 1, len(actions))
	assert.Equal(t, postponeUpload, actions[0].actionType)
	assert.Equal(t, cachedChanged, cacheBrain.pages[2].state)
	assert.Equal(t, 1, cacheBrain.cacheCount)
}
