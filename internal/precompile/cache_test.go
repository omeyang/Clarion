package precompile

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_PutAndGet(t *testing.T) {
	cache := NewCache(t.TempDir())

	data := []byte("hello audio data")
	err := cache.Put("greeting", data)
	require.NoError(t, err)

	got, ok := cache.Get("greeting")
	require.True(t, ok)
	assert.Equal(t, data, got)
}

func TestCache_GetMissing(t *testing.T) {
	cache := NewCache(t.TempDir())

	got, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestCache_Has(t *testing.T) {
	cache := NewCache(t.TempDir())

	assert.False(t, cache.Has("key"))

	err := cache.Put("key", []byte("data"))
	require.NoError(t, err)

	assert.True(t, cache.Has("key"))
}

func TestCache_OverwriteExisting(t *testing.T) {
	cache := NewCache(t.TempDir())

	err := cache.Put("key", []byte("v1"))
	require.NoError(t, err)

	err = cache.Put("key", []byte("v2"))
	require.NoError(t, err)

	got, ok := cache.Get("key")
	require.True(t, ok)
	assert.Equal(t, []byte("v2"), got)
}

func TestCache_DifferentKeysGetDifferentValues(t *testing.T) {
	cache := NewCache(t.TempDir())

	require.NoError(t, cache.Put("a", []byte("data-a")))
	require.NoError(t, cache.Put("b", []byte("data-b")))

	gotA, ok := cache.Get("a")
	require.True(t, ok)
	assert.Equal(t, []byte("data-a"), gotA)

	gotB, ok := cache.Get("b")
	require.True(t, ok)
	assert.Equal(t, []byte("data-b"), gotB)
}

func TestCache_ConcurrentAccess(t *testing.T) {
	cache := NewCache(t.TempDir())

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key"
			data := []byte{byte(i)}
			_ = cache.Put(key, data)
			cache.Get(key)
			cache.Has(key)
		}(i)
	}
	wg.Wait()

	// The cache should still be usable after concurrent access.
	assert.True(t, cache.Has("key"))
}

func TestCache_NonExistentDirectory(t *testing.T) {
	dir := t.TempDir() + "/sub/dir"
	cache := NewCache(dir)

	err := cache.Put("key", []byte("data"))
	require.NoError(t, err)

	got, ok := cache.Get("key")
	require.True(t, ok)
	assert.Equal(t, []byte("data"), got)
}
