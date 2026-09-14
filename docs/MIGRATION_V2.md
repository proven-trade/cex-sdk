# v2 마이그레이션

v2는 개발 중이며 이 변경 자체가 릴리스 태그를 게시하지는 않습니다. 기존 `v1.0.1` 이용자는 v2 릴리스 후 명시적으로 경로를 전환해야 합니다.

## 모듈과 import

v2의 모듈 경로는 `github.com/proven-trade/cex-sdk/v2`입니다. v2 릴리스 후 다음 명령으로 설치합니다.

```sh
go get github.com/proven-trade/cex-sdk/v2@v2.0.0
go mod tidy
```

```go
// v1
trade "github.com/proven-trade/cex-sdk"
"github.com/proven-trade/cex-sdk/exchange/binance"

// v2
trade "github.com/proven-trade/cex-sdk/v2"
"github.com/proven-trade/cex-sdk/v2/exchange/binance"
```

애플리케이션의 import와 테스트·예제의 import를 함께 바꿉니다. Go의 [주요 버전 업데이트 규칙](https://go.dev/doc/modules/major-version)에 따라 v1과 v2는 별개 모듈이며 두 버전의 구조체 타입을 혼용할 수 없습니다. Go 1.25 이상을 지원하며 CI는 1.25·1.26·1.27을 검사합니다.

## 주문 계약

| 변경 | 이전 사용 코드에서 할 일 |
|---|---|
| 공통 주문의 `ClientOrderID` 필수 | 제출 전에 고유 ID를 생성·보관하고 결과가 불명확하면 같은 ID로 조회합니다. |
| 시장가 매수 금액은 `Order.QuoteAmount` | 주문금액을 `Order.Quantity`에서 읽던 코드를 바꿉니다. `Quantity`는 기준 자산 수량입니다. |
| `acknowledged` 상태 | 접수와 실제 미체결 상태를 구분합니다. `Order` 조회 또는 private stream으로 확정합니다. |
| `cancel_pending` 상태 | 취소 접수만으로 자산·주문 정리를 완료하지 않습니다. 최종 주문 상태를 확인합니다. |

시장가 매수는 `QuoteAmount`, 시장가 매도는 `Quantity`, 지정가는 `Price`와 `Quantity`를 지정합니다. `UNKNOWN_EXECUTION_STATE`를 받으면 주문을 다시 생성하지 말고 기존 사용자 주문 ID로 조정합니다. native API의 고유 계약은 각 거래소 문서를 따릅니다.

## 선택적 주문 사전 검증

기존 `NewUnifiedSpot`의 호출 방식은 그대로 사용할 수 있습니다. 모든 공통 Spot 어댑터에 사전 검증을 추가하려면 다음 래퍼를 사용합니다.

```go
checked, err := unified.NewValidatedSpot(spot, unified.ValidationConfig{
    DefaultEgressRouteID: "seoul-a",
    CacheTTL: time.Minute,
})
if err != nil { return err }

order, err := checked.PlaceOrder(ctx, request,
    trade.WithEgressRoute("seoul-b"),
    trade.WithTimeout(5*time.Second),
)
```

규칙 조회와 주문 모두 `seoul-b`에서 실행되며 5초는 두 작업을 합한 제한 시간입니다. 캐시는 route별로 분리하고 동시 갱신을 합칩니다. TTL이 지난 규칙의 갱신이 실패하면 주문을 제출하지 않습니다. 정책 변경을 알게 된 경우 `checked.InvalidateMarketRules()`로 캐시를 무효화합니다.

공유 조회를 시작한 호출자가 취소되거나 제한 시간을 넘기면, 아직 유효한 다른 호출자는 자신의 context로 마켓 규칙을 다시 조회할 수 있습니다. 이 재시도는 메타데이터 조회에만 적용하며 주문 제출은 호출자당 한 번입니다.

검증은 거래소가 제공하는 단위와 최소값에 한정됩니다. 빈 규칙, 동적 가격 밴드, 수수료, 실제 가용 잔고와 체결 가능성은 보장하지 않습니다. 수량을 자동으로 반올림하지 않으며 거래소가 최종 검증합니다. `Markets` 등 주문 이외 메서드는 원래 클라이언트에 위임합니다.

## 운영 설정

- REST 읽기 재시도는 기존 제한 정책을 유지합니다. 관측 추가로 주문 재시도가 생기지 않습니다. [REST 오류 관측](OBSERVABILITY.md)을 참고합니다.
- 다중 프로세스가 계정·송신 IP 한도를 공유하려면 [Redis backend](REDIS_LIMITER.md)를 설정합니다. 메모리 limiter는 프로세스 간 상태를 공유하지 않습니다.
- 공통 마켓 자산 코드는 대문자 ASCII 영문·숫자입니다. Gate.io·MEXC·HTX의 비ASCII·괄호 등 지원 문법 밖의 상품과 Crypto.com의 `@venue` 상품은 공통 목록에서 제외하며 native 목록에는 남습니다.
- 실제 계정·배포 송신 경로 검증 전 상품은 계속 `experimental`입니다. 로컬 공개 smoke 통과만으로 운영 지원 상태를 올리지 않습니다.
