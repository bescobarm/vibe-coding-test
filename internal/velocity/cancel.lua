-- Cancels a reservation when a later stage declines (D-023). The authorization
-- was never approved, so it leaves every window, count included, and its hold
-- is deleted: the same state a rebuild would produce. Safe to repeat.
--
-- KEYS[1]   hold:{card}:{authorization_id}
-- ARGV[1]   authorization_id
--
-- Returns 0 when there was no hold, 1 when cancelled.

local keys = redis.call('HGET', KEYS[1], 'keys')
if not keys then
  return 0
end

for _, amounts in ipairs(cjson.decode(keys)) do
  -- "vel:{card}:rule:amt" -> "vel:{card}:rule:ts"
  local times = string.sub(amounts, 1, -4) .. 'ts'
  redis.call('HDEL', amounts, ARGV[1])
  redis.call('ZREM', times, ARGV[1])
end

redis.call('DEL', KEYS[1])
return 1
