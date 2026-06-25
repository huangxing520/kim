package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/wire/pkt"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

const (
	LocationExpired = time.Hour * 48
	redisOpTimeout  = 3 * time.Second
)

type RedisStorage struct {
	cli *redis.Client
}

func NewRedisStorage(cli *redis.Client) kim.SessionStorage {
	return &RedisStorage{
		cli: cli,
	}
}

func redisContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOpTimeout)
}

func (r *RedisStorage) Add(session *pkt.Session) error {
	loc := kim.Location{
		ChannelId: session.ChannelId,
		GateId:    session.GateId,
	}
	locKey := KeyLocation(session.Account, "")
	snKey := KeySession(session.ChannelId)
	buf, err := proto.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	ctx, cancel := redisContext()
	defer cancel()
	pipe := r.cli.Pipeline()
	pipe.Set(ctx, locKey, loc.Bytes(), LocationExpired)
	pipe.Set(ctx, snKey, buf, LocationExpired)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (r *RedisStorage) Delete(account string, channelId string) error {
	locKey := KeyLocation(account, "")
	snKey := KeySession(channelId)
	ctx, cancel := redisContext()
	defer cancel()
	pipe := r.cli.Pipeline()
	pipe.Del(ctx, locKey)
	pipe.Del(ctx, snKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (r *RedisStorage) Get(channelId string) (*pkt.Session, error) {
	snKey := KeySession(channelId)
	ctx, cancel := redisContext()
	defer cancel()
	bts, err := r.cli.Get(ctx, snKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var session pkt.Session
	if err := proto.Unmarshal(bts, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &session, nil
}

func (r *RedisStorage) GetLocations(accounts ...string) ([]*kim.Location, error) {
	keys := KeyLocations(accounts...)
	ctx, cancel := redisContext()
	defer cancel()
	list, err := r.cli.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]*kim.Location, len(accounts))
	for i, l := range list {
		if l == nil {
			result[i] = nil
			continue
		}
		var loc kim.Location
		if err := loc.Unmarshal([]byte(l.(string))); err != nil {
			return nil, fmt.Errorf("unmarshal location for %s: %w", accounts[i], err)
		}
		result[i] = &loc
	}
	allNil := true
	for _, loc := range result {
		if loc != nil {
			allNil = false
			break
		}
	}
	if allNil {
		return nil, kim.ErrSessionNil
	}
	return result, nil
}

func (r *RedisStorage) GetLocation(account string, device string) (*kim.Location, error) {
	key := KeyLocation(account, device)
	ctx, cancel := redisContext()
	defer cancel()
	bts, err := r.cli.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var loc kim.Location
	if err := loc.Unmarshal(bts); err != nil {
		return nil, fmt.Errorf("unmarshal location: %w", err)
	}
	return &loc, nil
}

func (r *RedisStorage) RedisGet(key string) (string, error) {
	ctx, cancel := redisContext()
	defer cancel()
	result, err := r.cli.Get(ctx, key).Result()
	return result, err
}

func KeySession(channel string) string {
	return fmt.Sprintf("login:sn:%s", channel)
}

func KeyLocation(account, device string) string {
	if device == "" {
		return fmt.Sprintf("login:loc:%s", account)
	}
	return fmt.Sprintf("login:loc:%s:%s", account, device)
}

func KeyLocations(accounts ...string) []string {
	arr := make([]string, len(accounts))
	for i, account := range accounts {
		arr[i] = KeyLocation(account, "")
	}
	return arr
}
