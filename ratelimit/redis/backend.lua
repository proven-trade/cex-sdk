-- Every rule/events key is supplied in KEYS and shares a namespace hash slot.
-- Responses: {-1} unknown rule, {-2} invalid charge, {wait microseconds}.
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000000 + tonumber(clock[2])
local action = ARGV[1]

if action == 'set' then
    local oldWindow = tonumber(redis.call('HGET', KEYS[1], 'window'))
    local window = tonumber(ARGV[3])
    if oldWindow and oldWindow ~= window then
        local blocked = tonumber(redis.call('HGET', KEYS[1], 'blocked') or '0')
        redis.call('HSET', KEYS[1], 'blocked', math.max(blocked, now + window))
    end
    redis.call('HSET', KEYS[1], 'limit', ARGV[2], 'window', window)
    return {0}
end

local states = {}
-- Validate the entire request before mutating any quota dimension.
for i = 1, #KEYS, 2 do
    local fields = redis.call('HMGET', KEYS[i], 'limit', 'window', 'used', 'blocked')
    if not fields[1] then return {-1} end
    local state = {
        key = KEYS[i], events = KEYS[i+1], limit = tonumber(fields[1]),
        window = tonumber(fields[2]), used = tonumber(fields[3] or '0'),
        blocked = tonumber(fields[4] or '0')
    }
    if action == 'wait' then
        state.charge = tonumber(ARGV[(i+1)/2 + 1])
        if not state.charge or state.charge <= 0 or state.charge > state.limit then return {-2} end
    end
    states[#states+1] = state
end

local function units(member)
    return tonumber(string.match(member, ':(%d+)$'))
end

local function prune(state)
    local expired = redis.call('ZRANGEBYSCORE', state.events, '-inf', now - state.window)
    for _, member in ipairs(expired) do state.used = state.used - units(member) end
    if #expired > 0 then
        redis.call('ZREMRANGEBYSCORE', state.events, '-inf', now - state.window)
        redis.call('HSET', state.key, 'used', state.used)
    end
end

local function charge(state, amount)
    local sequence = redis.call('HINCRBY', state.key, 'sequence', 1)
    redis.call('ZADD', state.events, now, sequence .. ':' .. amount)
    state.used = state.used + amount
    redis.call('HSET', state.key, 'used', state.used)
end

for _, state in ipairs(states) do prune(state) end

if action == 'wait' then
    local waitUntil = now
    for _, state in ipairs(states) do
        waitUntil = math.max(waitUntil, state.blocked)
        local excess = state.used + state.charge - state.limit
        if excess > 0 then
            local events = redis.call('ZRANGE', state.events, 0, -1, 'WITHSCORES')
            for i = 1, #events, 2 do
                excess = excess - units(events[i])
                if excess <= 0 then
                    waitUntil = math.max(waitUntil, tonumber(events[i+1]) + state.window)
                    break
                end
            end
        end
    end
    if waitUntil > now then return {waitUntil - now} end
    for _, state in ipairs(states) do charge(state, state.charge) end
    return {0}
elseif action == 'observe' then
    local state = states[1]
    local used = tonumber(ARGV[2])
    if used > state.used then charge(state, used - state.used) end
    return {0}
elseif action == 'block' then
    for _, state in ipairs(states) do
        redis.call('HSET', state.key, 'blocked', math.max(state.blocked, now + tonumber(ARGV[2])))
    end
    return {0}
elseif action == 'snapshot' then
    local state = states[1]
    local first = redis.call('ZRANGE', state.events, 0, 0, 'WITHSCORES')
    return {0, state.limit, state.window, state.used, tonumber(first[2] or '0'), state.blocked}
end
return redis.error_reply('unsupported limiter operation')
