# MEXC Spot V3 REST 어댑터

Go 패키지는 `exchange/mexc`이며 현행 Spot V3 기본 주소 `https://api.mexc.com`을 사용합니다. 현재 단계는 계정 없이 호출하는 공개 REST입니다. private 인증·잔고·주문, 공통 Spot API, protobuf WebSocket은 후속 단계에서 추가하며 지원 매트릭스의 REST 완료 상태도 그때 전환합니다.

MEXC는 별도 sandbox를 제공하지 않으므로 자동 테스트는 로컬 mock 거래소만 사용합니다. 실제 공개 API 호출은 production 환경으로 향한다는 점을 운영 절차에서 구분해야 합니다.

## 지원 범위

| 영역 | 메서드 | API | IP weight |
|---|---|---|---:|
| 연결 확인 | `Ping` | `GET /api/v3/ping` | 1 |
| 서버 시각 | `ServerTime` | `GET /api/v3/time` | 1 |
| 기본 API 거래쌍 | `DefaultSymbols` | `GET /api/v3/defaultSymbols` | 1 |
| 전체·단일·복수 거래쌍 규칙 | `ExchangeInfo` | `GET /api/v3/exchangeInfo` | 10 |
| 호가 snapshot | `OrderBook` | `GET /api/v3/depth` | 1 |
| 최근 체결 | `RecentTrades` | `GET /api/v3/trades` | 5 |
| 합산 체결 | `AggregateTrades` | `GET /api/v3/aggTrades` | 1 |
| 캔들 | `Candles` | `GET /api/v3/klines` | 1 |
| 최근 평균가 | `AveragePrice` | `GET /api/v3/avgPrice` | 1 |
| 24시간 통계 | `Ticker24H` | `GET /api/v3/ticker/24hr` | 1 |
| 최근 가격 | `PriceTicker` | `GET /api/v3/ticker/price` | 1 |
| 최우선 호가 | `BookTicker` | `GET /api/v3/ticker/bookTicker` | 1 |

24시간 통계와 ticker 메서드는 이번 단계에서 단일 symbol을 필수로 받습니다. 전체 symbol 조회는 공식 weight가 더 크고 응답도 배열로 바뀌므로, 호출 실수로 IP quota와 메모리를 크게 소비하지 않게 별도 API로 열지 않았습니다.

`OrderBookRequest.Limit`은 생략하거나 1~5000, 체결·캔들 조회의 `Limit`은 생략하거나 1~1000입니다. 합산 체결의 `Start`와 `End`는 함께 지정해야 합니다. 캔들은 `1m`, `5m`, `15m`, `30m`, `60m`, `4h`, `1d`, `1W`, `1M`을 지원하고 시각은 Unix millisecond query로 변환합니다.

## 생성과 요청별 EIP 선택

```go
client, err := mexc.New(mexc.Config{
	Executor:             executor,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

book, err := client.OrderBook(
	ctx,
	mexc.OrderBookRequest{Symbol: "BTCUSDT", Limit: 1000},
	trade.WithEgressRoute("seoul-b"),
)
```

요청 옵션을 생략하면 `DefaultEgressRouteID`를 사용하고 `trade.WithEgressRoute`를 지정하면 해당 요청만 다른 route로 보냅니다. 실제 public IP 선택은 공통 전송 계층이 route에 연결된 secondary private IPv4로 소켓을 bind하여 수행합니다. MEXC의 public 제한은 IP 기준이므로 route마다 limiter 상태가 분리됩니다.

## 요청 제한과 차단 응답

공식 Spot V3 문서는 계정 인증이 없는 endpoint가 IP 기준이며 각 endpoint마다 독립된 500 weight/10초 제한을 가진다고 설명합니다. SDK는 다음 형식으로 route와 endpoint별 bucket을 분리합니다.

```text
mexc:route:<route>:public:<endpoint>:10seconds
```

`Config.EndpointQuota` 기본값은 500이며 더 보수적인 운영값으로 낮출 수 있습니다. endpoint 문서의 weight만큼 원자적으로 차감합니다. 서버가 HTTP 418 또는 429와 `Retry-After`를 반환하면 해당 route·endpoint bucket을 지정된 기간 동안 차단합니다. 429 이후 계속 호출하면 IP ban 기간이 길어질 수 있으므로 호출자가 별도 우회 재시도를 추가하면 안 됩니다.

다중 EIP 기능은 정상적인 public 트래픽 분산, 허용 IP 일치, 장애 격리를 위한 기능입니다. MEXC의 제한이나 이용 정책을 우회하는 용도로 사용하면 안 됩니다.

## 응답과 오류 계약

가격·수량·거래량은 `float64`로 바꾸지 않고 문자열로 보존합니다. 위치 기반 호가와 캔들은 필드 수를 검증하며 각 객체의 `Raw`에는 해당 JSON 원본을 보존합니다. 거래 ID처럼 응답에 숫자·문자열·`null`이 혼재하는 값은 `Scalar`로 원형을 유지합니다.

`DefaultSymbols`는 공식 문서 예시의 성공 code `200`과 실제 production 응답의 `0`을 모두 허용합니다. 다른 nonzero code, HTTP 오류, JSON 파싱 실패는 `trade.APIError`로 변환합니다. 인증·권한·잔고·주문 없음·요청 제한·거래소 장애 코드를 공통 category로 분류하고 MEXC 원본 code·message와 요청 ID를 함께 보존합니다.

## 공식 기준

- [MEXC Spot V3 API 문서](https://mexcdevelop.github.io/apidocs/spot_v3_en/)
- [MEXC API 안내](https://www.mexc.com/mexc-api)
