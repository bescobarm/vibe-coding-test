-- Raises a hold (incremental authorization, D-027) only if every amount
-- window still has room. The count does not change: it is the same authorization.
--
-- KEYS[1]       hold:{card}:{authorization_id}
-- KEYS[2i]      sorted set: authorization_id -> time in ms, for rule i
-- KEYS[2i+1]    hash: authorization_id -> amount, for rule i
-- ARGV[1..5]    authorization_id, event_id, amount, now ms, hold ttl ms
-- ARGV[5+3i-2..5+3i]  rule i: window start ms, max amount, ttl ms
--
-- Returns {1, 0, 0, 0} when raised (or this event_id was already applied),
-- or {0, i, count, spent} for the first rule i without room.

local id = ARGV[1]
local event = 'event:' .. ARGV[2]
local amount = tonumber(ARGV[3])
local now = ARGV[4]
local rules = (#KEYS - 1) / 2

if redis.call('HEXISTS', KEYS[1], event) == 1 then
  return {1, 0, 0, 0}
end

for i = 1, rules do
  local times, amounts = KEYS[2 * i], KEYS[2 * i + 1]
  local a = 5 + 3 * (i - 1)
  local start = ARGV[a + 1]
  local maxAmount = tonumber(ARGV[a + 2])

  local expired = redis.call('ZRANGEBYSCORE', times, '-inf', '(' .. start)
  for _, old in ipairs(expired) do
    redis.call('HDEL', amounts, old)
  end
  redis.call('ZREMRANGEBYSCORE', times, '-inf', '(' .. start)

  local spent = 0
  for _, v in ipairs(redis.call('HVALS', amounts)) do
    spent = spent + tonumber(v)
  end

  if spent + amount > maxAmount then
    return {0, i, redis.call('ZCARD', times), spent}
  end
end

local held = {}
local existing = redis.call('HGET', KEYS[1], 'keys')
if existing then
  for _, k in ipairs(cjson.decode(existing)) do
    held[k] = true
  end
end

for i = 1, rules do
  local times, amounts = KEYS[2 * i], KEYS[2 * i + 1]
  local ttl = ARGV[5 + 3 * i]
  -- The original authorization may be outside this window (e.g. yesterday's
  -- hotel check-in): then only the increment counts here.
  redis.call('HINCRBY', amounts, id, amount)
  redis.call('ZADD', times, 'NX', now, id)
  redis.call('PEXPIRE', times, ttl)
  redis.call('PEXPIRE', amounts, ttl)
  held[amounts] = true
end

local keys = {}
for k in pairs(held) do
  table.insert(keys, k)
end

redis.call('HSET', KEYS[1], event, 1, 'keys', cjson.encode(keys))
redis.call('HINCRBY', KEYS[1], 'amount', amount)
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {1, 0, 0, 0}
