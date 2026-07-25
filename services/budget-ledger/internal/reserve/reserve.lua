-- scripts/reserve.lua
-- ARGV[1]=idempotency_key ARGV[2]=amount ARGV[3]=reservation_id
-- ARGV[4]=ttl_seconds ARGV[5]=expires_at_unix ARGV[6..]=node path, leaf to root

local idem_key = "idempotency:" .. ARGV[1]
local existing = redis.call("GET", idem_key)
if existing then return {"EXISTS", existing} end

local amount = tonumber(ARGV[2])
local reservation_id = ARGV[3]
local ttl = tonumber(ARGV[4])
local expires_at = ARGV[5]
local path = {}
for i = 6, #ARGV do table.insert(path, ARGV[i]) end

-- PASS 1: check headroom at every node -- no mutation yet
for _, node in ipairs(path) do
  local cap = tonumber(redis.call("GET", "budget:node:"..node..":cap") or "0")
  local committed = tonumber(redis.call("GET", "budget:node:"..node..":committed") or "0")
  local reserved = tonumber(redis.call("GET", "budget:node:"..node..":reserved") or "0")
  if committed + reserved + amount > cap then
    return {"DENY", node, cap, committed, reserved}
  end
end

-- PASS 2: all nodes have room -- apply the hold at every node
for _, node in ipairs(path) do
  redis.call("INCRBY", "budget:node:"..node..":reserved", amount)
end

redis.call("HSET", "reservation:"..reservation_id,
  "path", cjson.encode(path), "amount", amount, "state", "HELD",
  "idempotency_key", ARGV[1])
redis.call("EXPIRE", "reservation:"..reservation_id, ttl)
redis.call("ZADD", "reservations:by_expiry", expires_at, reservation_id)
redis.call("SET", idem_key, reservation_id, "EX", ttl + 60)

return {"HELD", reservation_id}
