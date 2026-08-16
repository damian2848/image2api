package service

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConcurrencyService is a Redis-backed, self-healing concurrency limiter shared
// by the per-user gate (画图台 + API key) and the per-account upstream gate.
//
// Each slot is a member of a sorted set keyed by the subject (user/account),
// scored with its expiry time. Acquire prunes expired members first, so a slot
// whose Release was lost (crash / missed defer) auto-frees after the TTL — the
// count can never leak forever. It's intentionally lossy-tolerant: if Redis is
// unavailable it FAILS OPEN (allows the work) rather than blocking generation.
type ConcurrencyService struct {
	redis *redis.Client
	// ttl is the max lifetime of a slot — the longest a generation can run
	// (video ~3min) plus head-room, after which a stuck slot self-heals.
	ttl int
}

func NewConcurrencyService(rdb *redis.Client) *ConcurrencyService {
	return &ConcurrencyService{redis: rdb, ttl: 900} // 15 min
}

// acquireScript: KEYS[1]=set, ARGV[1]=max (0=unlimited), ARGV[2]=ttl secs,
// ARGV[3]=token. Prunes expired members, then admits the token iff under max.
// Returns 1 on success, 0 when full.
var acquireScript = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local n = redis.call('ZCARD', KEYS[1])
local max = tonumber(ARGV[1])
if max > 0 and n >= max then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[3])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
return 1
`)

// Acquire takes one slot under `key` (capped at max; 0 = unlimited), tagged with
// `token`. Returns true if admitted. Fail-open when Redis is down/unset.
func (c *ConcurrencyService) Acquire(ctx context.Context, key string, max int, token string) bool {
	if c == nil || c.redis == nil {
		return true
	}
	res, err := acquireScript.Run(ctx, c.redis, []string{key}, max, c.ttl, token).Int()
	if err != nil {
		return true // fail open — never block a generation on Redis trouble
	}
	return res == 1
}

// AcquireWait waits for a slot instead of rejecting a burst. It is used by
// provider admission control, where queueing a submit is preferable to sending
// another request into an already saturated upstream. Redis failures still
// fail open through Acquire.
func (c *ConcurrencyService) AcquireWait(ctx context.Context, key string, max int, token string, pollEvery time.Duration) error {
	if pollEvery <= 0 {
		pollEvery = 100 * time.Millisecond
	}
	for {
		if c.Acquire(ctx, key, max, token) {
			return nil
		}
		timer := time.NewTimer(pollEvery)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// AcquireWaitDynamic is AcquireWait with a limit that is re-read on every
// admission attempt. Adaptive provider gates use it so already queued workers
// immediately honor a concurrency decrease after an overload response.
func (c *ConcurrencyService) AcquireWaitDynamic(ctx context.Context, key, token string, pollEvery time.Duration, limit func() int) (int, error) {
	if pollEvery <= 0 {
		pollEvery = 100 * time.Millisecond
	}
	for {
		current := 1
		if limit != nil {
			current = limit()
		}
		if current < 1 {
			current = 1
		}
		if c.Acquire(ctx, key, current, token) {
			return current, nil
		}
		timer := time.NewTimer(pollEvery)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

// Release frees the slot held by `token` under `key`. Safe to call even if the
// slot already expired.
func (c *ConcurrencyService) Release(ctx context.Context, key, token string) {
	if c == nil || c.redis == nil {
		return
	}
	_ = c.redis.ZRem(ctx, key, token).Err()
}

// pauseScript extends a shared cooldown without allowing a shorter concurrent
// pause to reduce an already longer one. KEYS[1]=key, ARGV[1]=duration ms.
var pauseScript = redis.NewScript(`
local requested = tonumber(ARGV[1])
local current = redis.call('PTTL', KEYS[1])
if current < requested then
  redis.call('SET', KEYS[1], '1', 'PX', requested)
end
return 1
`)

// Pause starts or extends a provider-wide cooldown. It is best-effort: an
// unavailable Redis must not turn an upstream error into a local outage.
func (c *ConcurrencyService) Pause(ctx context.Context, key string, duration time.Duration) {
	if c == nil || c.redis == nil || duration <= 0 {
		return
	}
	_ = pauseScript.Run(ctx, c.redis, []string{key}, duration.Milliseconds()).Err()
}

// PauseRemaining returns the current shared cooldown without waiting.
func (c *ConcurrencyService) PauseRemaining(ctx context.Context, key string) time.Duration {
	if c == nil || c.redis == nil {
		return 0
	}
	remaining, err := c.redis.PTTL(ctx, key).Result()
	if err != nil || remaining <= 0 {
		return 0
	}
	return remaining
}

// WaitWhilePaused blocks until a shared cooldown expires. It rechecks the TTL
// after every wake because another overload response may extend the pause.
func (c *ConcurrencyService) WaitWhilePaused(ctx context.Context, key string) error {
	for {
		remaining := c.PauseRemaining(ctx, key)
		if remaining <= 0 {
			return nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// adaptiveLimitScript returns a bounded distributed limit, initializing it on
// first use. The state expires after an idle period so a deployment or a long
// quiet interval returns to the conservative initial value.
var adaptiveLimitScript = redis.NewScript(`
local initial = tonumber(ARGV[1])
local minimum = tonumber(ARGV[2])
local maximum = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local current = tonumber(redis.call('GET', KEYS[1]))
if not current then current = initial end
if current < minimum then current = minimum end
if current > maximum then current = maximum end
redis.call('SET', KEYS[1], current, 'PX', ttl)
return current
`)

// adaptiveSuccessScript implements additive recovery: every N accepted submits
// increases the bucket limit by one, up to the configured maximum.
var adaptiveSuccessScript = redis.NewScript(`
local initial = tonumber(ARGV[1])
local minimum = tonumber(ARGV[2])
local maximum = tonumber(ARGV[3])
local threshold = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])
local current = tonumber(redis.call('GET', KEYS[1]))
if not current then current = initial end
if current < minimum then current = minimum end
if current > maximum then current = maximum end
local successes = redis.call('INCR', KEYS[2])
redis.call('PEXPIRE', KEYS[2], ttl)
if successes >= threshold then
  redis.call('DEL', KEYS[2])
  if current < maximum then current = current + 1 end
end
redis.call('SET', KEYS[1], current, 'PX', ttl)
return current
`)

// adaptiveOverloadScript applies multiplicative decrease and counts overloads
// in a short fixed window. The caller uses the count to decide whether this is
// an isolated response or enough correlated pressure to open a circuit.
var adaptiveOverloadScript = redis.NewScript(`
local initial = tonumber(ARGV[1])
local minimum = tonumber(ARGV[2])
local maximum = tonumber(ARGV[3])
local window = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])
local current = tonumber(redis.call('GET', KEYS[1]))
if not current then current = initial end
if current < minimum then current = minimum end
if current > maximum then current = maximum end
current = math.floor(current / 2)
if current < minimum then current = minimum end
redis.call('SET', KEYS[1], current, 'PX', ttl)
redis.call('DEL', KEYS[2])
local overloads = redis.call('INCR', KEYS[3])
if overloads == 1 then redis.call('PEXPIRE', KEYS[3], window) end
return {current, overloads}
`)

func normalizeAdaptiveBounds(initial, minimum, maximum int) (int, int, int) {
	if minimum < 1 {
		minimum = 1
	}
	if maximum < minimum {
		maximum = minimum
	}
	if initial < minimum {
		initial = minimum
	}
	if initial > maximum {
		initial = maximum
	}
	return initial, minimum, maximum
}

// AdaptiveLimit returns the current shared limit for a provider bucket.
func (c *ConcurrencyService) AdaptiveLimit(ctx context.Context, key string, initial, minimum, maximum int, ttl time.Duration) int {
	initial, minimum, maximum = normalizeAdaptiveBounds(initial, minimum, maximum)
	if c == nil || c.redis == nil {
		return initial
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	limit, err := adaptiveLimitScript.Run(ctx, c.redis, []string{key}, initial, minimum, maximum, ttl.Milliseconds()).Int()
	if err != nil {
		return initial
	}
	return limit
}

// RecordAdaptiveSuccess records an accepted submit and performs additive
// recovery after successesPerIncrease consecutive accepts.
func (c *ConcurrencyService) RecordAdaptiveSuccess(ctx context.Context, limitKey, successKey string, initial, minimum, maximum, successesPerIncrease int, ttl time.Duration) int {
	initial, minimum, maximum = normalizeAdaptiveBounds(initial, minimum, maximum)
	if successesPerIncrease < 1 {
		successesPerIncrease = 1
	}
	if c == nil || c.redis == nil {
		return initial
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	limit, err := adaptiveSuccessScript.Run(ctx, c.redis, []string{limitKey, successKey}, initial, minimum, maximum, successesPerIncrease, ttl.Milliseconds()).Int()
	if err != nil {
		return initial
	}
	return limit
}

// RecordAdaptiveOverload halves the current bucket limit and returns both the
// new limit and the number of overloads observed inside the current window.
func (c *ConcurrencyService) RecordAdaptiveOverload(ctx context.Context, limitKey, successKey, overloadKey string, initial, minimum, maximum int, window, ttl time.Duration) (int, int) {
	initial, minimum, maximum = normalizeAdaptiveBounds(initial, minimum, maximum)
	if c == nil || c.redis == nil {
		return initial, 1
	}
	if window <= 0 {
		window = 10 * time.Second
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	values, err := adaptiveOverloadScript.Run(ctx, c.redis, []string{limitKey, successKey, overloadKey}, initial, minimum, maximum, window.Milliseconds(), ttl.Milliseconds()).Int64Slice()
	if err != nil || len(values) != 2 {
		return initial, 1
	}
	return int(values[0]), int(values[1])
}

// Count returns the live (non-expired) slot count under `key` — for display.
func (c *ConcurrencyService) Count(ctx context.Context, key string) int {
	if c == nil || c.redis == nil {
		return 0
	}
	now := time.Now().Unix()
	_ = c.redis.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now, 10)).Err()
	n, err := c.redis.ZCard(ctx, key).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

// CountUsers returns live concurrency for many users in one round-trip
// (group_id display etc. don't need this, but the user list does). Keyed by the
// raw subject id passed in.
func (c *ConcurrencyService) CountMany(ctx context.Context, prefix string, ids []string) map[string]int {
	out := make(map[string]int, len(ids))
	if c == nil || c.redis == nil || len(ids) == 0 {
		return out
	}
	pipe := c.redis.Pipeline()
	cmds := make(map[string]*redis.IntCmd, len(ids))
	for _, id := range ids {
		cmds[id] = pipe.ZCard(ctx, prefix+id)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return out
	}
	for id, cmd := range cmds {
		if n, err := cmd.Result(); err == nil && n > 0 {
			out[id] = int(n)
		}
	}
	return out
}
