package config

import (
	"context"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type baseConfig struct {
	lock     sync.Mutex
	items    map[common.ConfigKey]common.ConfigItem
	fallback common.ConfigStore
}

func NewBaseConfig(fallback common.ConfigStore) *baseConfig {
	return &baseConfig{
		items:    make(map[common.ConfigKey]common.ConfigItem),
		fallback: fallback,
	}
}

var _ common.ConfigStore = (*baseConfig)(nil)

func (c *baseConfig) Get(key common.ConfigKey) common.ConfigItem {
	if item, found := c.doGet(key); found {
		return item
	}

	return c.fallback.Get(key)
}

func (c *baseConfig) Add(item common.ConfigItem) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.items[item.Key()] = item
}

func (c *baseConfig) doGet(key common.ConfigKey) (common.ConfigItem, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if item, ok := c.items[key]; ok {
		return item, true
	}

	return nil, false
}

func (c *baseConfig) Update(ctx context.Context) {
	c.fallback.Update(ctx)
}

type staticConfigItem struct {
	key   common.ConfigKey
	value string
}

func NewStaticValue(key common.ConfigKey, value string) *staticConfigItem {
	return &staticConfigItem{key: key, value: value}
}

var _ common.ConfigItem = (*staticConfigItem)(nil)

func (ci *staticConfigItem) Key() common.ConfigKey {
	return ci.key
}

func (ci *staticConfigItem) Value() string {
	return ci.value
}
