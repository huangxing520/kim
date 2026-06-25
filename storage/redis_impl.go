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
)

type RedisStorage struct {
	cli *redis.Client
}

func NewRedisStorage(cli *redis.Client) kim.SessionStorage {
	return &RedisStorage{
		cli: cli,
	}
}

func (r *RedisStorage) Add(session *pkt.Session) error {
	loc := kim.Location{
		ChannelId: session.ChannelId,
		GateId:    session.GateId,
	}
	locKey := KeyLocation(session.Account, "")
	snKey := KeySession(session.ChannelId)
	buf, _ := proto.Marshal(session)
	pipe := r.cli.Pipeline()
	pipe.Set(context.Background(), locKey, loc.Bytes(), LocationExpired)
	pipe.Set(context.Background(), snKey, buf, LocationExpired)
	_, err := pipe.Exec(context.Background())
	if err != nil {
		return err
	}
	return nil
}

func (r *RedisStorage) Delete(account string, channelId string) error {
	locKey := KeyLocation(account, "")
	snKey := KeySession(channelId)
	pipe := r.cli.Pipeline()
	pipe.Del(context.Background(), locKey)
	pipe.Del(context.Background(), snKey)
	_, err := pipe.Exec(context.Background())
	if err != nil {
		return err
	}
	return nil
}

func (r *RedisStorage) Get(channelId string) (*pkt.Session, error) {
	snKey := KeySession(channelId)
	bts, err := r.cli.Get(context.Background(), snKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var session pkt.Session
	_ = proto.Unmarshal(bts, &session)
	return &session, nil
}

func (r *RedisStorage) GetLocations(accounts ...string) ([]*kim.Location, error) {
	keys := KeyLocations(accounts...)
	list, err := r.cli.MGet(context.Background(), keys...).Result()
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
		_ = loc.Unmarshal([]byte(l.(string)))
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
	bts, err := r.cli.Get(context.Background(), key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var loc kim.Location
	_ = loc.Unmarshal(bts)
	return &loc, nil
}

func (r *RedisStorage) RedisGet(key string) (string, error) {
	result, err := r.cli.Get(context.Background(), key).Result()
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
