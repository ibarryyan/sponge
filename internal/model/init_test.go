package model

import (
	"context"
	"testing"
	"time"

	"github.com/zhufuyi/sponge/configs"
	"github.com/zhufuyi/sponge/internal/config"
	"github.com/zhufuyi/sponge/pkg/utils"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestGetDB(t *testing.T) {
	err := config.Init(configs.Path("serverNameExample.yml"))
	if err != nil {
		panic(err)
	}

	config.Get().App.EnableTrace = true
	config.Get().Database.Mysql.EnableLog = true

	time.Sleep(time.Millisecond * 10)
	err = CloseDB()
	assert.NoError(t, err)

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		gdb := GetDB()
		assert.NotNil(t, gdb)
		cancel()
	})
}

func TestInitMysqlError(t *testing.T) {
	err := config.Init(configs.Path("serverNameExample.yml"))
	if err != nil {
		panic(err)
	}

	// change config error test
	config.Get().Database.Mysql.Dsn = "root:123456@(127.0.0.1:3306)/test"

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		_ = CloseDB()
		InitMysql()
		assert.NotNil(t, db)
		cancel()
	})
}

func TestInitPostgresqlError(t *testing.T) {
	_ = config.Init(configs.Path("serverNameExample.yml"))

	// change config error test
	config.Get().Database.Postgresql.Dsn = "root:123456@(127.0.0.1:5432)/test"

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		_ = CloseDB()
		InitPostgresql()
		assert.NotNil(t, db)
		cancel()
	})
}

func TestInitSqliteError(t *testing.T) {
	_ = config.Init(configs.Path("serverNameExample.yml"))
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		InitSqlite()
		assert.NotNil(t, db)
		cancel()
	})
}

func TestCloseDB(t *testing.T) {
	defer func() { recover() }()
	db = &gorm.DB{}
	err := CloseDB()
	assert.NoError(t, err)
}

func TestInitRedis(t *testing.T) {
	defer func() {
		if e := recover(); e != nil {
			t.Log("ignore connect redis error info")
		}
	}()

	err := config.Init(configs.Path("serverNameExample.yml"))
	if err != nil {
		panic(err)
	}

	InitRedis()
	cli := GetRedisCli()
	assert.NotNil(t, cli)
	time.Sleep(time.Millisecond * 10)
	err = CloseRedis()
	assert.NoError(t, err)

	// change config error test
	config.Get().App.EnableTrace = true
	config.Get().Redis.Dsn = "default:123456@127.0.0.1:6379/0"
	redisCli = nil
	_ = CloseRedis()
	_ = GetRedisCli()
}

func TestTableName(t *testing.T) {
	t.Log(new(UserExample).TableName())
}

func TestGetCacheType(t *testing.T) {
	InitCache("memory")
	ct := GetCacheType()
	assert.NotNil(t, ct)

	err := config.Init(configs.Path("serverNameExample.yml"))
	if err != nil {
		panic(err)
	}

	cacheType = nil
	defer func() { recover() }()
	ct = GetCacheType()
	assert.NotNil(t, ct)

	defer func() { recover() }()
	InitCache("redis")
	ct = GetCacheType()
	assert.NotNil(t, ct)
}
