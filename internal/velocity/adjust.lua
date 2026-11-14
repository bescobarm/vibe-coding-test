-- Changes a hold by a signed amount without checking limits: reversal, expiry
-- and refund lower it, a capture moves it to the captured amount (D-053).
-- The authorization stays in the count windows (D-051).
--
-- KEYS[1]   hold:{card}:{authorization_id}
-- ARGV[1]   authorization_id
-- ARGV[2]   event_id, applied at most once (D-050)
-- ARGV[3]   signed amount
--
-- Returns 0 when nothing is held (no rule applied, or the hold expired),
-- 1 when adjusted, 2 when this event_id was already applied.

if redis.call('EXISTS', KEYS[1]) == 0 then
  return 0
end

if redis.call('HSETNX', KEYS[1], 'event:' .. ARGV[2], 1) == 0 then
  return 2
end

local delta = tonumber(ARGV[3])

for _, amounts in ipairs(cjson.decode(redis.call('HGET', KEYS[1], 'keys'))) do
  -- An entry already pruned from its window must not come back.
  if redis.call('HEXISTS', amounts, ARGV[1]) == 1 then
    if redis.call('HINCRBY', amounts, ARGV[1], delta) <= 0 then
      redis.call('HDEL', amounts, ARGV[1])
    end
  end
end

local held = tonumber(redis.call('HGET', KEYS[1], 'amount'))
redis.call('HSET', KEYS[1], 'amount', math.max(held + delta, 0))
return 1
