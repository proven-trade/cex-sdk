# Redis 공유 요청 제한기

`ratelimit/redis`는 여러 SDK 프로세스가 같은 quota를 사용하는 rolling-window backend입니다. Redis의 서버 시계와 Lua를 사용해 모든 차감 차원을 한 번에 검사·차감합니다. 한 차원이 막히면 다른 차원도 차감하지 않습니다. 네트워크 또는 저장소 오류는 호출자에게 반환하며 메모리 방식으로 전환해 요청을 보내지 않습니다.

```go
import (
    "time"
    "github.com/redis/go-redis/v9"
    "github.com/proven-trade/cex-sdk/v2/ratelimit"
    redislimit "github.com/proven-trade/cex-sdk/v2/ratelimit/redis"
)

redisClient := redis.NewClient(&redis.Options{
    Addr: "127.0.0.1:6379",
    ContextTimeoutEnabled: true,
    MaxRetries: -1,
})
defer redisClient.Close()

backend, err := redislimit.New(redislimit.Config{
    Client: redisClient,
    Namespace: "production-trading",
    OperationTimeout: 2*time.Second,
})
if err != nil { return err }
limiter, err := ratelimit.NewWithBackend(backend)
if err != nil { return err }
// 기존 exchange.ExecutorConfig{Sender: registry, Limiter: limiter}에 주입합니다.
// 각 거래소 클라이언트가 요청 전에 해당 quota 규칙을 등록합니다.
```

`ContextTimeoutEnabled: true`는 필수이며 생성 시 확인합니다. Redis 접속·인증·TLS는 주입하는 클라이언트에 설정합니다. SDK는 클라이언트를 닫지 않습니다. `MaxRetries: -1`은 불명확한 Redis 응답의 중복 차감을 줄이는 권장 설정입니다. Redis 요청을 재시도해 중복 차감이 발생하더라도 거래소 주문 자체를 재시도하지 않으며 quota를 보수적으로 소비합니다.

## 공유 범위와 저장 계약

- 같은 한도를 공유하는 프로세스는 같은 `Namespace`, 거래소 `AccountID`와 route ID를 사용합니다. 동일 공인 IP를 다른 route ID로 등록하면 기존 SDK의 route별 키가 다르므로 한도를 공유하지 않습니다.
- namespace와 규칙 이름을 SHA-256으로 변환한 키를 사용합니다. 모든 namespace 키는 같은 Cluster hash slot에 있어 다차원 Lua 연산을 수행할 수 있습니다. 하나의 namespace는 하나의 Redis shard에서 처리됩니다.
- Standalone·Cluster·Ring 클라이언트를 받을 수 있습니다. CI 통합 검증 대상은 Redis 7.4 standalone입니다.
- 동일 규칙의 재등록은 사용량·차단 상태를 초기화하지 않습니다. 한도 변경도 사용량을 유지합니다. window가 바뀌면 과거의 만료된 사용량을 복원할 수 없으므로 새 window 동안 보수적으로 차단합니다.
- `ObserveUsed`는 공유 사용량을 높일 때만 반영하고, `BlockFor`는 기존 차단 종료 시각을 앞당기지 않습니다.
- 시간 정밀도는 마이크로초이며 더 작은 duration은 올림합니다. 한도와 차감량은 `1..2^31-1` 범위입니다.
- 규칙 hash는 지속되며 사용량 이벤트는 다음 접근 시 만료 정리합니다. 사용하지 않는 규칙을 제거할 때는 모든 관련 프로세스가 멈춘 뒤 해당 namespace를 관리합니다. 실행 중 키 삭제·eviction·데이터 유실은 quota 이력을 없애므로 전용 Redis의 `noeviction`과 적절한 지속성 설정을 사용합니다. Redis failover의 데이터 유실까지 exactly-once로 보장하지는 않습니다.

Lua 원자성과 키 전달 규칙은 [Redis 공식 문서](https://redis.io/docs/latest/develop/programmability/eval-intro/)를 따릅니다.

## 통합 테스트

```sh
docker run --rm -p 127.0.0.1:6379:6379 redis:7.4-alpine
CEX_SDK_REDIS_ADDR=127.0.0.1:6379 go test -race ./ratelimit/...
```

테스트는 고유 namespace만 사용·정리하며 `FLUSHDB`를 실행하지 않습니다. 환경변수가 없으면 Redis 통합 테스트를 건너뛰고, CI의 race·coverage 작업에서는 실제 Redis service를 연결해 검사합니다.
