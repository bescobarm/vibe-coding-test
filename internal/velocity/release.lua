-- Frees part of a hold (reversal, expiry). The authorization stays in the
-- count windows: only its amount goes down (D-051).
--
-- KEYS[1]   hold:{card}:{authorization_id}
-- ARGV[1]   authorization_id
-- ARGV[2]   event_id, applied at most once (D-050)
-- ARGV[3]   amount to release
--
-- Returns 0 when nothing is held (no rule applied, or the hold expired),
-- 1 when released, 2 when this event_id was already applied.

if redis.call('EXISTS', KEYS[1]) == 0 then
  return 0
end

if redis.call('HSETNX', KEYS[1], 'event:' .. ARGV[2], 1) == 0 then
  return 2
end

local held = tonumber(redis.call('HGET', KEYS[1], 'amount'))
local left = math.max(held - tonumber(ARGV[3]), 0)

for _, amounts in ipairs(cjson.decode(redis.call('HGET', KEYS[1], 'keys'))) do
  -- An entry already pruned from its window must not come back.
  if redis.call('HEXISTS', amounts, ARGV[1]) == 1 then
    if left > 0 then
      redis.call('HSET', amounts, ARGV[1], left)
    else
      redis.call('HDEL', amounts, ARGV[1])
    end
  end
end

redis.call('HSET', KEYS[1], 'amount', left)
return 1
