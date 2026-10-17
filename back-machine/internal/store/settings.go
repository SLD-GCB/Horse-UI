package store

import (
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"horseui/back-machine/internal/model"
)

// Settings 带缓存的键值配置读写。
type Settings struct {
	db *gorm.DB
	mu sync.RWMutex
	m  map[string]string
}

// NewSettings 加载全部配置到内存缓存。
func NewSettings(db *gorm.DB) (*Settings, error) {
	s := &Settings{db: db, m: make(map[string]string)}
	var rows []model.Setting
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		s.m[r.Key] = r.Value
	}
	return s, nil
}

// Get 读取配置，不存在返回空串。
func (s *Settings) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[key]
}

// GetDefault 读取配置，不存在返回默认值。
func (s *Settings) GetDefault(key, def string) string {
	if v := s.Get(key); v != "" {
		return v
	}
	return def
}

// Set 写入配置并持久化；值未变化时跳过写库。
func (s *Settings) Set(key, val string) error {
	s.mu.RLock()
	cur, ok := s.m[key]
	s.mu.RUnlock()
	if ok && cur == val {
		return nil
	}
	row := model.Setting{Key: key, Value: val}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	s.mu.Lock()
	s.m[key] = val
	s.mu.Unlock()
	return nil
}

// SetMany 在单次语句中批量写入多个配置，跳过值未变化的项。
func (s *Settings) SetMany(kv map[string]string) error {
	rows := make([]model.Setting, 0, len(kv))
	s.mu.RLock()
	for k, v := range kv {
		if cur, ok := s.m[k]; ok && cur == v {
			continue
		}
		rows = append(rows, model.Setting{Key: k, Value: v})
	}
	s.mu.RUnlock()
	if len(rows) == 0 {
		return nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&rows).Error; err != nil {
		return err
	}
	s.mu.Lock()
	for _, r := range rows {
		s.m[r.Key] = r.Value
	}
	s.mu.Unlock()
	return nil
}

// SetDefault 仅在配置缺失时写入默认值。
func (s *Settings) SetDefault(key, val string) error {
	if s.Get(key) != "" {
		return nil
	}
	return s.Set(key, val)
}

// All 返回配置快照。
func (s *Settings) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}
