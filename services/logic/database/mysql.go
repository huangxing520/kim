// 文件：mysql.go
// 职责：MySQL 数据库初始化——通过 GORM 建立 MySQL 连接，配置连接池参数和命名策略。
//
// 常量：
//   - defaultMaxOpenConns / defaultMaxIdleConns / defaultConnMaxLifetime / defaultConnMaxIdleTime：连接池配置
//
// 方法：
//   - InitDb(driver, dsn) → 初始化 GORM 数据库连接（支持 MySQL，配置连接池和命名策略）

package database

import (

	// just init

	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	// "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// 数据库连接池默认配置
const (
	defaultMaxOpenConns    = 100              // 新加的：最大连接数
	defaultMaxIdleConns    = 20               // 新加的：最大空闲连接数
	defaultConnMaxLifetime = time.Hour        // 新加的：连接最大存活时间
	defaultConnMaxIdleTime = time.Minute * 10 // 新加的：连接最大空闲时间
)

// InitMysqlDb init mysql database
func InitDb(driver string, dsn string) (*gorm.DB, error) {
	// dsn := "user:pass@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local"

	defaultLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold: 200 * time.Millisecond,
		LogLevel:      logger.Warn,
		Colorful:      true,
	})

	var dialector gorm.Dialector
	if driver == "mysql" {
		dialector = mysql.Open(dsn)
	}
	// else if driver == "sqlite" {
	// dialector = sqlite.Open(dsn)
	// }

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: defaultLogger,
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   "t_",                              // table name prefix, table for `User` would be `t_users`
			SingularTable: true,                              // use singular table name, table for `User` would be `user` with this option enabled
			NameReplacer:  strings.NewReplacer("CID", "Cid"), // use name replacer to change struct/field name before convert it to db name
		}})
	if err != nil {
		return nil, err
	}

	// 【修复#11】新加的：配置数据库连接池参数
	// 原代码未设置连接池，高并发场景下会导致连接耗尽或频繁建连
	sqlDB, err := db.DB() // 新加的：获取底层 *sql.DB
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(defaultMaxOpenConns)    // 新加的：设置最大连接数
	sqlDB.SetMaxIdleConns(defaultMaxIdleConns)    // 新加的：设置最大空闲连接数
	sqlDB.SetConnMaxLifetime(defaultConnMaxLifetime) // 新加的：设置连接最大存活时间
	sqlDB.SetConnMaxIdleTime(defaultConnMaxIdleTime) // 新加的：设置连接最大空闲时间

	return db, nil
}
