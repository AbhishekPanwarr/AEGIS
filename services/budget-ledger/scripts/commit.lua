-- scripts/commit.lua
-- ARGV[1]=reservation_id

local r = redis.call("HGETALL", "reservation:"..ARGV[1])
if #r == 0 then return {"ERROR", "not found"} end

local state, amount, path_str
for i = 1, #r, 2 do
  if r[i] == "state" then state = r[i+1] end
  if r[i] == "amount" then amount = tonumber(r[i+1]) end
  if r[i] == "path" then path_str = r[i+1] end
end

if state ~= "HELD" then return {"ERROR", "not held"} end

local path = cjson.decode(path_str)
for _, node in ipairs(path) do
  redis.call("INCRBY", "budget:node:"..node..":committed", amount)
  redis.call("DECRBY", "budget:node:"..node..":reserved", amount)
end

redis.call("HSET", "reservation:"..ARGV[1], "state", "COMMITTED")
redis.call("ZREM", "reservations:by_expiry", ARGV[1])

return {"COMMITTED", ARGV[1]}
