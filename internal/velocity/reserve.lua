-- Atomic check-and-reserve for one card (D-003). Redis runs the whole script
-- before any other command, so two requests can never both see the same budget.
--
-- KEYS[1]       hold:{card}:{authorization_id}
-- KEYS[2i]      sorted set: authorization_id -> time in ms, for rule i
-- KEYS[2i+1]    hash: authorization_id -> amount, for rule i
-- ARGV[1..4]    authorization_id, amount, now ms, hold ttl ms
-- ARGV[4+4i-3..4+4i]  rule i: window start ms, max count (-1: none), max amount (-1: none), ttl ms
--
-- Returns {1, 0, 0, 0} when reserved (or already reserved by this
-- authorization_id), or {0, i, count, spent} for the first rule i that is full.

local id = ARGV[1]
local amount = tonumber(ARGV[2])
local now = ARGV[3]
local rules = (#KEYS - 1) / 2

-- A retry after the decision could not be stored: the amount is already held.
if redis.call('EXISTS', KEYS[1]) == 1 then
  return {1, 0, 0, 0}
end

for i = 1, rules do
  local times, amounts = KEYS[2 * i], KEYS[2 * i + 1]
  local a = 4 + 4 * (i - 1)
  local start = ARGV[a + 1]
  local maxCount = tonumber(ARGV[a + 2])
  local maxAmount = tonumber(ARGV[a + 3])

  local expired = redis.call('ZRANGEBYSCORE', times, '-inf', '(' .. start)
  for _, old in ipairs(expired) do
    redis.call('HDEL', amounts, old)
  end
  redis.call('ZREMRANGEBYSCORE', times, '-inf', '(' .. start)

  local count = redis.call('ZCARD', times)
  local spent = 0
  for _, v in ipairs(redis.call('HVALS', amounts)) do
    spent = spent + tonumber(v)
  end

  local overCount = maxCount >= 0 and count + 1 > maxCount
  local overAmount = maxAmount >= 0 and spent + amount > maxAmount
  if overCount or overAmount then
    return {0, i, count, spent}
  end
end

local held = {}
for i = 1, rules do
  local times, amounts = KEYS[2 * i], KEYS[2 * i + 1]
  local ttl = ARGV[4 + 4 * i]
  redis.call('ZADD', times, now, id)
  redis.call('HSET', amounts, id, amount)
  redis.call('PEXPIRE', times, ttl)
  redis.call('PEXPIRE', amounts, ttl)
  held[i] = amounts
end

redis.call('HSET', KEYS[1], 'amount', amount, 'keys', cjson.encode(held))
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return {1, 0, 0, 0}
