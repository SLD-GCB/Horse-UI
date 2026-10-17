package store

import (
	"fmt"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"horseui/back-machine/internal/model"
)

// Open 打开（或创建）SQLite 数据库。
func Open(dataDir string) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		filepath.ToSlash(filepath.Join(dataDir, "horseui.db")),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// SQLite 是单写数据库：限制为单连接可避免多连接争抢写锁。
	// 面板并发很低，收益（消除写等待、杜绝 SQLITE_BUSY）远大于代价。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	return db, nil
}

// Migrate 建表/升级表结构。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Admin{},
		&model.Setting{},
		&model.Inbound{},
		&model.Client{},
		&model.ClientInbound{},
		&model.ClientStat{},
		&model.Outbound{},
		&model.Certificate{},
		&model.AuditLog{},
	)
}
