package precompile

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Cache 是预编译音频数据的线程安全文件系统缓存。
// 键通过哈希生成配置目录下的文件名。
type Cache struct {
	dir string
	mu  sync.RWMutex
}

// NewCache 创建在 dir 下存储文件的 Cache。目录不存在时自动创建。
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// Get 返回键对应的缓存音频数据，缓存未命中时返回 (nil, false)。
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.keyPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put 在给定键下存储音频数据。如需创建缓存目录。
func (c *Cache) Put(key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o750); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	path := c.keyPath(key)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}

// Has 当键存在于缓存中时返回 true。
func (c *Cache) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.keyPath(key)
	_, err := os.Stat(path)
	return err == nil
}

func (c *Cache) keyPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	name := fmt.Sprintf("%x.bin", hash[:16])
	return filepath.Join(c.dir, name)
}
