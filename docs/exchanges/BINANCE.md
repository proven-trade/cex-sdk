# Binance Spot REST 어댑터

`exchange/binance` 패키지의 `*Client`는 Binance Spot REST API를 요청별 송신 경로 선택과 함께 제공합니다. 기본 REST 주소는 `https://api.binance.com`입니다. 공개 시세, 주문 생성·정정·취소·조회, 주문 리스트(OCO·OTO·OTOCO·OPO·OPOCO), 스마트 주문 라우팅(SOR), 계정·수수료·주문 count 조회까지 `*Client`의 공개 메서드 49개를 이 문서의 표에 모두 정리했습니다.

WebSocket stream, User Data Stream과 로컬 오더북은 [Binance Spot WebSocket](BINANCE_WEBSOCKET.md), USDⓈ-M Futures는 [Binance USDⓈ-M Futures](BINANCE_USDM.md) 문서를 참고합니다.

## 패키지와 전제조건

private API는 `credential.Material`의 다음 값을 사용합니다.

| 필드 | Binance 값 |
|---|---|
| `APIKey` | API Key (`X-MBX-APIKEY` 헤더로 전송) |
| `SecretKey` | HMAC SHA-256 Secret 원문 |

HMAC API Key만 지원합니다. RSA·Ed25519 서명, SBE 응답, FIX API는 지원하지 않습니다.

- `credential.Descriptor.Exchange`는 `model.ExchangeBinance`여야 합니다.
- `credential.Descriptor.AccountID`에는 Binance 계정 또는 제한을 공유하는 계정 식별자를 넣습니다. Binance는 미체결 주문 count를 (서브)계정 단위로 추적하고 모든 IP와 API Key가 공유합니다. 같은 계정의 API Key는 같은 `AccountID`를 사용해야 SDK가 주문 count를 한곳에서 계산합니다.
- 자격증명의 `AllowedEgressRouteIDs` 밖 route는 Secret 조회 전에 차단되며 `trade.ErrAuthorization`을 반환합니다. API Key에 거래소 쪽 IP 제한을 걸었다면 허용 route의 공인 IP를 모두 등록해야 합니다.
- `credential.Descriptor.Permissions`는 SDK 안의 사전 검사입니다. 필요한 권한이 없으면 Secret 조회 전에 `trade.ErrAuthorization`을 반환합니다. 주문 메서드는 거래소 쪽 API Key에도 거래 권한이 켜져 있어야 합니다.
- 공개 API만 쓰면 `Credentials`와 `CredentialProvider`를 생략할 수 있습니다. 이 클라이언트로 private 메서드를 호출하면 네트워크 요청 없이 `trade.ErrAuthentication`을 반환합니다.

## 설정

```go
client, err := binance.New(binance.Config{
	Executor: executor,
	Credentials: &credential.Descriptor{
		AccountID: "binance-main",
		Exchange:  model.ExchangeBinance,
		SecretRef: "secret/binance/main",
		Permissions: []credential.Permission{
			credential.PermissionRead,
			credential.PermissionTrade,
		},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
	},
	CredentialProvider:   provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

// 옵션이 없으면 seoul-a, 이 호출 한 건만 seoul-b를 사용합니다.
ticker, err := client.TickerPrice(
	ctx,
	binance.TickerPriceRequest{Symbol: "BTCUSDT"},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
fmt.Println(ticker.Price)
```

| `Config` 필드 | 설명 | 기본값 |
|---|---|---|
| `Executor` | 공통 실행기(`*exchange.Executor`)입니다. 필수입니다. | |
| `Credentials` | private API용 비밀이 아닌 메타데이터입니다. `CredentialProvider`와 함께 지정해야 하며 `Exchange`는 `model.ExchangeBinance`여야 합니다. | |
| `CredentialProvider` | `SecretRef`로 API Key와 Secret을 조회합니다. | |
| `DefaultEgressRouteID` | 요청에 `trade.WithEgressRoute`가 없을 때 쓰는 송신 경로입니다. 필수이며 비어 있으면 `New`가 `trade.ErrMissingEgressRoute`로 거부합니다. | |
| `BaseURL` | REST 기본 주소입니다. host가 있어야 하고 userinfo, query, fragment는 허용하지 않습니다. | `https://api.binance.com` |
| `AllowInsecureHTTP` | `http` 주소를 허용합니다. 로컬 테스트 서버에만 사용합니다. | `false` |
| `RequestTimeout` | 요청 한 건의 전체 timeout입니다. limiter 대기와 읽기 재시도를 포함하며 `trade.WithTimeout`으로 요청마다 덮어씁니다. 음수는 거부합니다. | 10초 |
| `ReceiveWindow` | 서명 요청의 `recvWindow`입니다. 1ms~60,000ms 범위의 ms 단위 값이어야 합니다. | 5초 |
| `Now` | 시계 주입입니다. 테스트용입니다. | `time.Now` |

모든 메서드는 마지막 인자로 `trade.RequestOption`을 받습니다. `trade.WithEgressRoute`는 요청 한 건의 송신 경로, `trade.WithTimeout`은 요청 한 건의 timeout을 정합니다.

공식 문서는 공개 시세 전용 주소 `https://data-api.binance.vision`을 안내합니다. 공식 FAQ는 이 주소가 인증 없이 `aggTrades`, `avgPrice`, `depth`, `exchangeInfo`, `klines`, `ping`, `ticker`, `ticker/24hr`, `ticker/bookTicker`, `ticker/price`, `time`, `trades`, `uiKlines`만 제공한다고 나열합니다. 이 주소는 `Config.BaseURL`로 지정하며 실제 응답은 이 문서에서 확인하지 않았습니다. 이 주소를 쓰는 클라이언트로는 목록에 없는 경로(`historicalTrades`, `historicalBlockTrades`, `executionRules`, `ticker/tradingDay`, `referencePrice`)와 private API를 호출하면 안 되므로, 그런 호출은 기본 주소를 쓰는 별도 클라이언트로 보냅니다.

## 지원 범위

| 영역 | 메서드 | 권한 |
|---|---|---|
| 연결·시간 | `Ping`, `ServerTime` | 공개 |
| 거래 규칙 | `ExchangeInfo`, `ExecutionRules` | 공개 |
| 호가·체결 | `OrderBook`, `RecentTrades`, `HistoricalTrades`, `HistoricalBlockTrades`, `AggregateTrades` | 공개 |
| 캔들 | `Klines`, `UIKlines` | 공개 |
| 가격·통계 | `AveragePrice`, `TickerPrice`, `TickerPrices`, `BookTicker`, `BookTickers`, `Ticker24Hour`, `TradingDayTicker`, `RollingTicker`, `ReferencePrice`, `ReferencePriceCalculation` | 공개 |
| 주문 생성·정정·취소 | `NewOrder`, `TestOrder`, `CancelOrder`, `CancelOpenOrders`, `CancelReplaceOrder`, `AmendOrderKeepPriority` | `credential.PermissionTrade` |
| 주문 조회 | `QueryOrder`, `OpenOrders`, `AllOrders` | `credential.PermissionRead` |
| 주문 리스트 생성·취소 | `NewOCOOrderList`, `NewOTOOrderList`, `NewOTOCOOrderList`, `NewOPOOrderList`, `NewOPOCOOrderList`, `CancelOrderList` | `credential.PermissionTrade` |
| 주문 리스트 조회 | `QueryOrderList`, `AllOrderLists`, `OpenOrderLists` | `credential.PermissionRead` |
| SOR | `SOROrder`, `TestSOROrder` | `credential.PermissionTrade` |
| 계정 | `Account`, `AccountCommission`, `MyTrades`, `MyAllocations`, `MyPreventedMatches`, `OrderAmendments`, `OrderRateLimits`, `MyFilters` | `credential.PermissionRead` |

권한 열의 값은 다음 뜻입니다.

| 권한 | 의미 |
|---|---|
| 공개 | 자격증명 없이 호출합니다. 서명, `X-MBX-APIKEY`, Secret 조회와 권한 검사를 모두 하지 않으며 `OperationRead`로 실행합니다. |
| `credential.PermissionRead` | 공식 `USER_DATA` 엔드포인트입니다. 서명하고 `OperationRead`로 실행합니다. |
| `credential.PermissionTrade` | 공식 `TRADE` 엔드포인트입니다. 서명하고 `OperationMutation`으로 실행하며 자동 재시도하지 않습니다. 테스트 주문도 포함합니다. |

공통 Spot API가 필요하면 `binance.NewUnifiedSpot(client)`로 [공통 Spot API](../UNIFIED_SPOT.md) 어댑터를 만듭니다. 이 문서의 표는 native `*Client` 메서드를 기준으로 합니다.

## 인증과 시간

SIGNED 요청은 URL 인코딩한 최종 파라미터에 HMAC SHA-256을 적용합니다. 실행 순서는 다음과 같습니다.

1. 자격증명의 route와 권한을 검사합니다.
2. IP weight와 계정 주문 count 제한을 확보합니다.
3. Secret을 조회합니다. `APIKey`나 `SecretKey`가 비어 있으면 `trade.ErrAuthentication`입니다.
4. 보정된 현재 시간(`timestamp`)과 `recvWindow`를 넣습니다.
5. 최종 query에 서명하고 `signature`를 마지막에 붙인 뒤 `X-MBX-APIKEY` 헤더와 함께 선택한 송신 경로로 전송합니다.

POST·PUT·DELETE 요청도 본문 없이 query string으로 파라미터와 서명을 보냅니다. 요청이 끝나면 조회한 Secret 값을 파기합니다.

`ServerTime`은 요청 왕복 중간 시점을 기준으로 로컬 clock offset을 갱신합니다. offset은 `ServerTime`을 호출할 때만 갱신되므로 서명 요청 전에 한 번 호출하고 주기적으로 다시 호출합니다. `-1021`(timestamp가 `recvWindow` 밖)이 발생해도 SDK가 offset을 자동 보정하거나 재시도하지 않고 `VALIDATION`으로 반환합니다.

## 요청 제한

초기 안전값은 현재 공식 `exchangeInfo`의 `rateLimits`입니다.

| 제한 | 초기값 | 범위 | limiter key |
|---|---:|---|---|
| `REQUEST_WEIGHT` | 6,000/분 | 송신 경로 | `binance:route:<route>:request_weight:1minute` |
| `RAW_REQUESTS` | 300,000/5분 | 송신 경로 | `binance:route:<route>:raw_requests:5minute` |
| `ORDERS` | 100/10초 | 계정(`AccountID`) | `binance:account:<account>:orders:10second` |
| `ORDERS` | 200,000/일 | 계정(`AccountID`) | `binance:account:<account>:orders:1day` |

- 모든 요청은 `RAW_REQUESTS` 1과 메서드별 weight를 소비합니다. 이 문서의 표에서 "주문 count"가 0보다 큰 메서드만 `ORDERS`를 그 값만큼 소비합니다. 취소, 정정, 테스트 주문과 조회는 `ORDERS`를 소비하지 않고, 공개 API는 `AccountID`가 필요 없습니다.
- `ExchangeInfo` 응답의 `REQUEST_WEIGHT`, `RAW_REQUESTS`, `ORDERS` 규칙으로 값을 동적으로 갱신합니다. 다른 종류의 규칙은 무시합니다.
- 응답의 `X-MBX-USED-WEIGHT-<n><S|M|H|D>`는 송신 경로 bucket에, `X-MBX-ORDER-COUNT-<n><S|M|H|D>`는 계정 bucket에 반영합니다. 서버 값이 로컬 값보다 클 때만 올리며 HTTP 4xx·5xx 응답도 반영합니다.
- 서버는 주문이 처음 체결될 때(부분 체결 포함) 주문 count를 줄이고, 취소와 만료로는 줄이지 않습니다([공식 규칙](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/order_count_decrement.md)). SDK의 로컬 bucket은 체결로 줄이지 않고 창이 지나야 비워지므로, 체결이 잦은 전략에서는 서버 한도보다 먼저 로컬 limiter가 대기시킬 수 있습니다. 현재 서버 값은 `OrderRateLimits`로 확인합니다.
- HTTP 429·418을 받으면 해당 요청의 모든 limiter key를 `Retry-After`(초 또는 HTTP 날짜, 없으면 1초) 동안 막습니다. 반복 위반하면 IP 차단(HTTP 418)이 2분에서 3일까지 늘어납니다. SDK는 429·418을 자동 재시도하지 않습니다.
- weight 한도는 API Key가 아니라 IP 기준입니다. SDK는 송신 경로별 bucket을 쓰므로 여러 route가 같은 공인 IP를 공유하면 서로의 사용량을 알지 못하고 응답 헤더로만 보정됩니다. route와 공인 IP를 1:1로 구성합니다.
- `LocalOrderBook`의 REST snapshot은 기본 limit 5,000으로 `OrderBook`을 호출하므로 snapshot 한 번에 weight 250을 사용합니다.

## 시세 API

모두 인증이 필요 없는 공개 API(Binance 보안 구분 `NONE`)입니다. API Key와 `credential.Permission`을 요구하지 않습니다.

| 메서드 | API | 권한 | weight |
|---|---|---|---|
| `Ping` | `GET /api/v3/ping` | 공개 | 1 |
| `ServerTime` | `GET /api/v3/time` | 공개 | 1 |
| `ExchangeInfo` | `GET /api/v3/exchangeInfo` | 공개 | 20 |
| `ExecutionRules` | `GET /api/v3/executionRules` | 공개 | 단일 심볼 2, `Symbols` 심볼당 2(최대 40), `SymbolStatus`·전체 40 |
| `OrderBook` | `GET /api/v3/depth` | 공개 | limit 1~100(기본) 5, 101~500 25, 501~1,000 50, 1,001~5,000 250 |
| `RecentTrades` | `GET /api/v3/trades` | 공개 | 25 |
| `HistoricalTrades` | `GET /api/v3/historicalTrades` | 공개 | 25 |
| `HistoricalBlockTrades` | `GET /api/v3/historicalBlockTrades` | 공개 | 25 |
| `AggregateTrades` | `GET /api/v3/aggTrades` | 공개 | 4 |
| `Klines` | `GET /api/v3/klines` | 공개 | 2 |
| `UIKlines` | `GET /api/v3/uiKlines` | 공개 | 2 |
| `AveragePrice` | `GET /api/v3/avgPrice` | 공개 | 2 |
| `TickerPrice` | `GET /api/v3/ticker/price` | 공개 | 2 |
| `TickerPrices` | `GET /api/v3/ticker/price` | 공개 | 단일 심볼 2, 복수·전체 4 |
| `BookTicker` | `GET /api/v3/ticker/bookTicker` | 공개 | 2 |
| `BookTickers` | `GET /api/v3/ticker/bookTicker` | 공개 | 단일 심볼 2, 복수·전체 4 |
| `Ticker24Hour` | `GET /api/v3/ticker/24hr` | 공개 | 단일 심볼 2, `Symbols` 1~20개 2, 21~100개 40, 전체 80 |
| `TradingDayTicker` | `GET /api/v3/ticker/tradingDay` | 공개 | 심볼당 4, 50개 초과 200 |
| `RollingTicker` | `GET /api/v3/ticker` | 공개 | 심볼당 4, 50개 초과 200 |
| `ReferencePrice` | `GET /api/v3/referencePrice` | 공개 | 2 |
| `ReferencePriceCalculation` | `GET /api/v3/referencePrice/calculation` | 공개 | 2 |

- `HistoricalTrades`는 공식 CHANGELOG에서 보안 구분이 `MARKET_DATA`에서 `NONE`으로 바뀌어 API Key를 보내지 않습니다. `HistoricalBlockTrades`는 `FromID`가 필수입니다.
- `ExchangeInfo`는 `Symbol` 하나만 지정할 수 있습니다. 비우면 서버 기본값에 따라 `SPOT`, `MARGIN`, `LEVERAGED` 권한 중 하나라도 가진 상품을 반환합니다. 응답의 `rateLimits`는 요청 제한 규칙을 갱신하는 데 쓰입니다.
- `OrderBook`의 `Limit`은 최대 5,000이고 0이면 파라미터를 보내지 않아 서버 기본값 100(weight 5)을 씁니다. `RecentTrades`, `AggregateTrades`, `HistoricalTrades`, `HistoricalBlockTrades`, `Klines`, `UIKlines`의 `Limit`은 최대 1,000이고 0이면 서버 기본값 500입니다.
- `Klines`와 `UIKlines`는 `KlineInterval`(`1s`~`1M`)과 선택적인 `TimeZone`을 받습니다. `TimeZone`을 지정하면 캔들 구간을 그 시간대로 해석하지만 `StartTime`·`EndTime`은 항상 UTC로 해석됩니다. `TradingDayTicker`도 `TimeZone`을 받습니다.
- `Ticker24Hour`, `TickerPrices`, `BookTickers`는 `Symbol`, `Symbols`, `AllSymbols` 중 정확히 하나가 필요합니다. 전체 조회는 weight가 크므로 `AllSymbols: true`를 명시해야 합니다. `TradingDayTicker`와 `RollingTicker`는 전체 조회가 없고 `Symbol` 또는 `Symbols` 중 하나가 필요합니다.
- `Symbols`는 최대 100개이며 중복할 수 없고 `["BTCUSDT","BNBUSDT"]` 형태의 JSON 배열로 전송합니다.
- `Ticker24Hour`, `TradingDayTicker`, `RollingTicker`는 응답 상세 수준 `Type`(`FULL`, `MINI`)을 받습니다.
- `Ticker24Hour`, `TradingDayTicker`, `RollingTicker`, `TickerPrices`, `BookTickers`, `ExecutionRules`, `ReferencePriceCalculation`은 `SymbolStatus`(`TRADING`, `HALT`, `BREAK`, `CANCEL_ONLY`) 필터를 받습니다. 공식 문서 기준으로 `TickerPrices`, `BookTickers`, `TradingDayTicker`는 단일 심볼의 상태가 맞지 않으면 `-1220`을 반환하고 복수·전체 조회는 맞지 않는 심볼을 응답에서 제외합니다.
- `RollingTicker.WindowSize`는 `1m`~`59m`, `1h`~`23h`, `1d`~`7d` 중 하나이며 비우면 서버 기본값 `1d`를 씁니다.
- `ExecutionRules`는 `Symbol`, `Symbols`, `SymbolStatus` 중 하나만 지정할 수 있고 모두 비우면 전체 상품을 조회합니다.
- `ReferencePrice`는 기준가격이 비어 있으면 `Price`가 nil이고, 한 번도 설정된 적이 없으면 거래소가 `-2043`으로 거부합니다.
- `TickerPrice`, `BookTicker`, `AveragePrice`는 단일 심볼 호출이고, `TickerPrices`, `BookTickers`, `Ticker24Hour`, `TradingDayTicker`, `RollingTicker`는 심볼 하나를 조회해도 원소가 하나인 목록을 반환합니다.

## 주문 API

SDK 권한 `credential.PermissionRead`는 Binance `USER_DATA`, `credential.PermissionTrade`는 `TRADE` 보안 구분에 해당합니다. "주문 count"는 `ORDERS` 제한에서 소비하는 미체결 주문 수입니다.

| 메서드 | API | 권한 | weight | 주문 count |
|---|---|---|---|---|
| `NewOrder` | `POST /api/v3/order` | `credential.PermissionTrade` | 1 | 1 |
| `TestOrder` | `POST /api/v3/order/test` | `credential.PermissionTrade` | 1, `ComputeCommissionRates` 20 | 0 |
| `QueryOrder` | `GET /api/v3/order` | `credential.PermissionRead` | 4 | 0 |
| `CancelOrder` | `DELETE /api/v3/order` | `credential.PermissionTrade` | 1 | 0 |
| `CancelOpenOrders` | `DELETE /api/v3/openOrders` | `credential.PermissionTrade` | 1 | 0 |
| `CancelReplaceOrder` | `POST /api/v3/order/cancelReplace` | `credential.PermissionTrade` | 1 | 1 |
| `AmendOrderKeepPriority` | `PUT /api/v3/order/amend/keepPriority` | `credential.PermissionTrade` | 4 | 0 |
| `OpenOrders` | `GET /api/v3/openOrders` | `credential.PermissionRead` | 단일 심볼 6, 전체 80 | 0 |
| `AllOrders` | `GET /api/v3/allOrders` | `credential.PermissionRead` | 20 | 0 |

주문 종류는 `LIMIT`, `MARKET`, `STOP_LOSS`, `STOP_LOSS_LIMIT`, `TAKE_PROFIT`, `TAKE_PROFIT_LIMIT`, `LIMIT_MAKER`입니다. SDK가 전송 전에 검사하는 종류별 필수 입력은 다음과 같습니다.

| 주문 종류 | 필수 | 허용하지 않는 입력 |
|---|---|---|
| `LIMIT` | `TimeInForce`, `Quantity`, `Price` 또는 `PegPriceType` | |
| `MARKET` | `Quantity` 또는 `QuoteOrderQuantity` 중 정확히 하나 | `Price`, `TimeInForce`, peg, iceberg |
| `LIMIT_MAKER` | `Quantity`, `Price` 또는 `PegPriceType` | `TimeInForce` |
| `STOP_LOSS`, `TAKE_PROFIT` | `Quantity`, `StopPrice` 또는 `TrailingDelta` | |
| `STOP_LOSS_LIMIT`, `TAKE_PROFIT_LIMIT` | `TimeInForce`, `Quantity`, `Price` 또는 `PegPriceType`, `StopPrice` 또는 `TrailingDelta` | |

- 가격과 수량은 양의 decimal 문자열로 입력하며 SDK가 자동 반올림하지 않습니다. `ExchangeInfo`의 `PRICE_FILTER`, `LOT_SIZE`, `NOTIONAL` 같은 필터를 적용한 값을 전달해야 합니다.
- `ClientOrderID`(`newClientOrderId`)는 `^[.A-Z:/a-z0-9_-]{1,36}$` 형식이며 SDK가 자동 생성하지 않습니다. 생략하면 서버가 임의 값을 만들므로 불명확한 결과를 조회할 수 없습니다. 항상 직접 지정합니다.
- `StrategyType`은 1,000,000 이상이고 `ResponseType`은 `ACK`, `RESULT`, `FULL`입니다. 서버 기본값은 `MARKET`·`LIMIT`이 `FULL`, 나머지가 `ACK`입니다.
- peg 주문은 `PegPriceType`(`PRIMARY_PEG`, `MARKET_PEG`)을 지정하고 `PegOffsetType`(`PRICE_LEVEL`)과 `PegOffsetValue`(0~100)를 둘 다 지정하거나 둘 다 생략합니다.
- 단일 주문의 `IcebergQuantity`는 `TimeInForceGTC`가 필요합니다. `LIMIT_MAKER`는 `TimeInForce`를 받지 않으므로 `LIMIT_MAKER` iceberg 단일 주문은 만들 수 없습니다. 주문 리스트의 `LIMIT_MAKER` leg는 iceberg를 허용합니다.
- `QueryOrder`, `CancelOrder`, `AmendOrderKeepPriority`는 `OrderID` 또는 `OriginalClientOrderID`가 하나 이상 필요합니다. 공식 문서는 취소(단일 취소와 `CancelReplaceOrder`)에서 `orderId`만 보낼 때 가장 빠르다고 안내합니다.
- `CancelOrder`의 `Restriction`은 `ONLY_NEW`, `ONLY_PARTIALLY_FILLED`입니다. 상태가 맞지 않으면 거래소가 `-2011`(`Order was not canceled due to cancel restrictions.`)로 거부합니다. `NewClientOrderID`는 취소 요청 자체의 식별자입니다.
- `OpenOrders`는 `Symbol` 또는 `AllSymbols: true` 중 하나가 필요하고 둘을 함께 쓸 수 없습니다. `AllOrders`는 `Symbol`이 필수이고 `OrderID`를 주면 그 ID 이상의 주문을 반환합니다. `StartTime`·`EndTime` 간격은 24시간 이하, `Limit`은 최대 1,000입니다.
- `TestOrder`는 주문을 매칭 엔진에 보내지 않고 서명, `recvWindow`, 필터만 검증합니다. 주문 count를 쓰지 않지만 `TRADE` 엔드포인트이므로 `credential.PermissionTrade`가 필요하고 자동 재시도하지 않습니다. `ComputeCommissionRates`를 켜면 weight 20을 쓰고 주문에 적용될 수수료율을 `TestOrderResult`로 반환하며, 켜지 않으면 응답 필드가 모두 nil입니다.
- `CancelOpenOrders`는 한 심볼의 미체결 주문과 주문 리스트를 모두 취소합니다. 응답은 주문 `Orders`와 주문 리스트 `OrderLists`로 나뉘며 주문 리스트에 속한 주문은 해당 `OrderLists`의 `OrderReports`에 담깁니다.
- `AmendOrderKeepPriority`는 대기열 우선순위를 유지한 채 `NewQuantity`로 수량을 줄입니다. 주문 count를 쓰지 않습니다. `NewClientOrderID`를 지정하지 않으면 서버가 새 ID를 만들므로 현재 ID를 유지하려면 같은 값을 `NewClientOrderID`로 보냅니다.

### 취소 후 신규 주문

`CancelReplaceOrder`는 같은 심볼에서 기존 주문을 취소하고 새 주문을 냅니다. 취소 대상은 `CancelOrderID` 또는 `CancelOriginalClientOrderID`로 지정하고, 새 주문은 `NewOrder` 필드에 `NewOrderRequest`와 같은 규칙으로 넣습니다. `Mode`는 필수이며 `CancelReplaceModeStopOnFailure`(`STOP_ON_FAILURE`, 취소가 실패하면 신규 주문을 시도하지 않음) 또는 `CancelReplaceModeAllowFailure`(`ALLOW_FAILURE`, 취소가 실패해도 신규 주문을 시도)입니다. `RateLimitExceededMode`는 `OrderRateLimitExceededModeDoNothing`(`DO_NOTHING`, 서버 기본값) 또는 `OrderRateLimitExceededModeCancelOnly`(`CANCEL_ONLY`)입니다. 필터와 주문 count는 취소와 신규 주문을 처리하기 전에 평가되며, 신규 주문이 `NOT_ATTEMPTED`여도 미체결 주문 count는 1 증가합니다.

| 응답 | 의미 |
|---|---|
| HTTP 200 | 취소와 신규 주문이 모두 성공 |
| HTTP 400 (`-2022`) | `STOP_ON_FAILURE`에서 취소가 실패해 신규 주문을 시도하지 않았거나, `ALLOW_FAILURE`에서 둘 다 실패 |
| HTTP 409 (`-2021`) | 한쪽만 실패하는 부분 성공. 취소는 성공했으나 신규 주문이 실패했거나, `ALLOW_FAILURE`에서 취소는 실패했으나 신규 주문이 성공 |
| HTTP 429 | `STOP_ON_FAILURE`와 `CANCEL_ONLY` 조합에서 미체결 주문 count를 초과. 취소가 실패하면 신규 주문은 `NOT_ATTEMPTED`, 취소가 성공하면 신규 주문은 `FAILURE` |

`DO_NOTHING`에서 미체결 주문 count를 초과하면 취소도 시도하지 않고 단계별 결과 없이 `-1015`(HTTP 429, `RATE_LIMITED`)만 반환합니다.

`CancelReplaceOrder`는 400, 409, 429 오류에 단계별 결과가 들어 있으면 오류와 함께 `CancelReplaceOrderResult`도 반환합니다. 따라서 `err`가 nil이 아니어도 `CancelResult`와 `NewOrderResult`(`SUCCESS`, `FAILURE`, `NOT_ATTEMPTED`)를 확인해야 합니다.

- `CancelResponse`와 `NewOrderResponse`는 `CancelReplaceLeg`입니다. 성공하면 `Order`가 채워지고 실패하면 거래소 `Code`와 `Message`가 채워지며 시도하지 않은 쪽은 비어 있습니다.
- 취소는 성공하고 신규 주문이 실패한 경우 기존 주문은 이미 사라졌습니다. 같은 요청을 그대로 다시 보내지 말고 두 주문의 상태를 `QueryOrder`로 확인한 뒤 신규 주문 재전송 여부를 따로 결정합니다.

## 주문 리스트 API

| 메서드 | API | 권한 | weight | 주문 count |
|---|---|---|---|---|
| `NewOCOOrderList` | `POST /api/v3/orderList/oco` | `credential.PermissionTrade` | 1 | 2 |
| `NewOTOOrderList` | `POST /api/v3/orderList/oto` | `credential.PermissionTrade` | 1 | 2 |
| `NewOTOCOOrderList` | `POST /api/v3/orderList/otoco` | `credential.PermissionTrade` | 1 | 3 |
| `NewOPOOrderList` | `POST /api/v3/orderList/opo` | `credential.PermissionTrade` | 1 | 2 |
| `NewOPOCOOrderList` | `POST /api/v3/orderList/opoco` | `credential.PermissionTrade` | 1 | 3 |
| `CancelOrderList` | `DELETE /api/v3/orderList` | `credential.PermissionTrade` | 1 | 0 |
| `QueryOrderList` | `GET /api/v3/orderList` | `credential.PermissionRead` | 4 | 0 |
| `AllOrderLists` | `GET /api/v3/allOrderList` | `credential.PermissionRead` | 20 | 0 |
| `OpenOrderLists` | `GET /api/v3/openOrderList` | `credential.PermissionRead` | 6 | 0 |

사용 중단된 `POST /api/v3/order/oco`는 구현하지 않습니다. `NewOCOOrderList`는 현행 `POST /api/v3/orderList/oco`를 사용합니다.

```go
orderList, err := client.NewOCOOrderList(ctx, binance.OCOOrderListRequest{
	Symbol:            "BTCUSDT",
	ListClientOrderID: "exit-20261120-0001",
	Side:              binance.SideSell,
	Quantity:          "0.001",
	Above: binance.OrderListLeg{
		Type:  binance.OrderTypeLimitMaker,
		Price: "65000.00",
	},
	Below: binance.OrderListLeg{
		Type:        binance.OrderTypeStopLossLimit,
		StopPrice:   "58000.00",
		Price:       "57900.00",
		TimeInForce: binance.TimeInForceGTC,
	},
})
if err != nil {
	return err
}
fmt.Println(orderList.OrderListID, orderList.ListOrderStatus)
```

| 종류 | 구성 | 입력 규칙 |
|---|---|---|
| OCO | `Above`, `Below` | 두 leg가 `Side`와 `Quantity`를 공유합니다. leg 종류는 `LIMIT_MAKER`, `STOP_LOSS`, `STOP_LOSS_LIMIT`, `TAKE_PROFIT`, `TAKE_PROFIT_LIMIT`입니다. |
| OTO | `Working`, `Pending` | `Working`은 `LIMIT` 또는 `LIMIT_MAKER`이고 전량 체결되면 `Pending`이 나갑니다. `Pending`은 별도의 `PendingSide`, `PendingQuantity`를 받으며 7가지 주문 종류를 모두 허용합니다. |
| OTOCO | `Working`, `PendingAbove`, `PendingBelow` | `Working` 전량 체결 후 위·아래 OCO 쌍이 나갑니다. 두 pending leg는 `PendingSide`와 `PendingQuantity`를 공유하며 `PendingBelow`는 비워 둘 수 있습니다. |
| OPO | `Working`, `Pending` | `Working` 체결 수량만큼 `Pending`이 나가므로 `PendingQuantity`가 없습니다. |
| OPOCO | `Working`, `PendingAbove`, `PendingBelow` | `Working` 체결 수량만큼 위·아래 OCO 쌍이 나가며 `PendingBelow`는 비워 둘 수 있습니다. |

- `OrderListLeg`가 leg 하나의 `Type`, `ClientOrderID`, `Price`, `StopPrice`, `TrailingDelta`, `IcebergQuantity`, `TimeInForce`, `StrategyID`, `StrategyType`, peg 입력, `QuoteNotionalLimit`을 담습니다. 종류별 가격 입력 규칙은 `NewOrder`와 같고, `LIMIT`·`LIMIT_MAKER` leg에는 발동 조건(`StopPrice`, `TrailingDelta`)을 넣을 수 없습니다.
- `QuoteNotionalLimit`은 leg별로 받지만 OPO와 OPOCO에서는 `Working` leg에만 허용하고 `Pending` leg에 넣으면 전송 전에 거부합니다.
- `ListClientOrderID`(`listClientOrderId`)는 주문 리스트의 식별자이며 각 leg의 `ClientOrderID`와 별개입니다. 같은 ID의 새 주문 리스트는 이전 리스트가 전량 체결되거나 완전히 만료된 뒤에만 수락되므로 불명확한 결과를 조회하려면 직접 지정합니다.
- 주문 리스트를 만들거나 취소한 응답의 `OrderList.OrderReports`에는 주문 상세가 있고, 조회 응답에는 없습니다.
- 리스트에 속한 개별 주문을 `CancelOrder`로 취소해도 주문 리스트 전체가 취소됩니다. `CancelOrderList`는 `Symbol`과 함께 `OrderListID` 또는 `ListClientOrderID`로 지정하며 둘 다 주면 같은 주문 리스트를 가리켜야 합니다.
- `QueryOrderList`는 `OrderListID` 또는 `ListClientOrderID` 중 하나 이상이 필요합니다. `AllOrderLists`는 `FromID`와 `StartTime`·`EndTime`을 함께 쓸 수 없고 시간 간격은 24시간 이하, `Limit`은 최대 1,000입니다.
- 상품이 주문 리스트를 허용하는지는 `ExchangeInfo`의 `Symbol.OCOAllowed`, `Symbol.OTOAllowed`로 확인합니다. OPO 허용 여부 `opoAllowed`는 typed `Symbol`에 필드가 없으므로 `ExchangeInfo.Raw`에서 읽습니다.

## SOR 주문 API

| 메서드 | API | 권한 | weight | 주문 count |
|---|---|---|---|---|
| `SOROrder` | `POST /api/v3/sor/order` | `credential.PermissionTrade` | 1 | 1 |
| `TestSOROrder` | `POST /api/v3/sor/order/test` | `credential.PermissionTrade` | 1, `ComputeCommissionRates` 20 | 0 |

- SOR은 `LIMIT`과 `MARKET`만 지원하고 `quoteOrderQty`를 지원하지 않으므로 `Quantity`가 필수입니다. `LIMIT`은 `Price`와 `TimeInForce`가 필요하고 `MARKET`은 `Price`, `TimeInForce`, `IcebergQuantity`를 받지 않습니다.
- `SOROrderResult`는 `Order`의 공통 필드에 `WorkingFloor`, `UsedSOR`를 더하고, 체결 `Fills`는 체결 유형 `MatchType`과 배분 ID `AllocationID`를 포함하는 `SORFill`입니다. 배분 내역은 `MyAllocations`로 조회합니다.
- `TestSOROrder`는 주문을 만들지 않고 검증만 하며 `TestOrderResult`를 반환합니다. 공식 SOR 테스트 응답에는 `specialCommissionForOrder`가 없으므로 `SpecialCommissionForOrder`는 보통 nil입니다.
- SOR의 동작과 제약은 공식 [SOR FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/sor_faq.md)를 참고합니다.

## 계정 API

| 메서드 | API | 권한 | weight |
|---|---|---|---|
| `Account` | `GET /api/v3/account` | `credential.PermissionRead` | 20 |
| `AccountCommission` | `GET /api/v3/account/commission` | `credential.PermissionRead` | 20 |
| `MyTrades` | `GET /api/v3/myTrades` | `credential.PermissionRead` | 20, `OrderID` 지정 시 5 |
| `MyAllocations` | `GET /api/v3/myAllocations` | `credential.PermissionRead` | 20 |
| `MyPreventedMatches` | `GET /api/v3/myPreventedMatches` | `credential.PermissionRead` | `PreventedMatchID` 2, `OrderID` 20 |
| `OrderAmendments` | `GET /api/v3/order/amendments` | `credential.PermissionRead` | 4 |
| `OrderRateLimits` | `GET /api/v3/rateLimit/order` | `credential.PermissionRead` | 40 |
| `MyFilters` | `GET /api/v3/myFilters` | `credential.PermissionRead` | 40 |

- `Account`는 `OmitZeroBalances: true`로 잔고가 0인 자산을 제외할 수 있습니다.
- `AccountCommission`과 `MyFilters`는 `Symbol`이 필요합니다.
- `MyTrades`가 허용하는 조합은 `Symbol` 단독, `OrderID`, `StartTime`·`EndTime`, `FromID`, `OrderID`와 `FromID`입니다. `StartTime`·`EndTime`은 `OrderID`·`FromID`와 함께 쓸 수 없고 간격은 24시간 이하이며 `Limit`은 최대 1,000입니다.
- `MyAllocations`가 허용하는 조합은 `Symbol` 단독, `StartTime`·`EndTime`, `FromAllocationID`, `OrderID`, `OrderID`와 `FromAllocationID`이며 시간 범위 규칙은 `MyTrades`와 같습니다.
- `MyPreventedMatches`는 `PreventedMatchID` 또는 `OrderID` 중 정확히 하나가 필요합니다. `FromPreventedMatchID`와 `Limit`은 `OrderID`와만 함께 씁니다.
- `OrderAmendments`는 `Symbol`과 `OrderID`가 필수이고 `FromExecutionID`, `Limit`(최대 1,000)을 받습니다.
- `OrderRateLimits`는 구간별 현재 미체결 주문 count를 `[]RateLimitCount`(`Count` 필드)로 반환합니다.
- `MyFilters`는 `AccountFilters`로 거래소 필터(`ExchangeFilters`), 상품 필터(`SymbolFilters`), 계정에 걸린 자산 필터(`AssetFilters`, 현재 `MAX_ASSET`)와 요청 제한 사용량(`RateLimits`)을 반환합니다. 계정에 `MAX_ASSET` 필터가 걸려 있는지는 이 엔드포인트로만 확인할 수 있습니다.

## mutation과 불명확한 실행 상태

`credential.PermissionTrade` 메서드(`NewOrder`, `TestOrder`, `CancelOrder`, `CancelOpenOrders`, `CancelReplaceOrder`, `AmendOrderKeepPriority`, `NewOCOOrderList`, `NewOTOOrderList`, `NewOTOCOOrderList`, `NewOPOOrderList`, `NewOPOCOOrderList`, `CancelOrderList`, `SOROrder`, `TestSOROrder`)는 모두 `OperationMutation`으로 실행하며 자동 재시도하지 않습니다.

읽기 요청만 같은 송신 경로에서 제한적으로 재시도합니다. 기본은 최대 2회 시도(재시도 1회)이고 `ExecutorConfig.ReadRetryPolicy`로 1~5회까지 조정하며 50ms~500ms 지터 backoff를 씁니다. 재시도 대상은 전송 오류, 응답 본문 읽기 실패와 HTTP 408·425·500·502·503·504입니다. HTTP 429·418은 읽기도 재시도하지 않습니다.

mutation에서 다음은 요청이 처리됐는지 단정할 수 없으므로 `UNKNOWN_EXECUTION_STATE`(`trade.ErrUnknownExecutionState`)로 분류합니다.

- 요청을 보내기 시작한 뒤의 연결 오류, timeout, context 만료. limiter 대기 중에 context가 만료되면 요청을 보내지 않았으므로 `TIMEOUT`입니다.
- 응답 본문 읽기 실패와 본문 크기 초과
- HTTP 5xx
- 거래소 코드 `-1007`(매칭 엔진 응답 timeout, `Send status unknown; execution status unknown.`)

같은 5xx와 `-1007`을 읽기 요청이 받으면 `EXCHANGE_UNAVAILABLE`이며 재시도 가능으로 표시합니다. 서버는 요청을 10초 안에 처리하지 못하면 `-1007`을 반환하고 SDK 기본 `RequestTimeout`도 10초이므로, 클라이언트 timeout과 `-1007`은 모두 같은 방법으로 처리합니다.

성공(2xx) 응답이지만 본문 JSON을 해석하지 못한 경우는 `UNKNOWN_EXECUTION_STATE`가 아니라 일반 decode 오류(`decode Binance JSON response`)로 반환합니다. 거래소는 요청을 처리했으므로 같은 방법으로 상태를 조회합니다.

HTTP 4xx 거절(`-2010` 등)은 요청이 확정적으로 거절된 결과이며 불명확한 상태가 아닙니다. HTTP 429·418은 `RATE_LIMITED`이며 `CancelReplaceOrder`를 제외하면 요청이 실행되지 않은 것입니다. SDK가 다시 보내지 않으므로 대기 후 재전송은 호출자가 결정합니다. `CancelReplaceOrder`는 409와 429(`CANCEL_ONLY`)에도 부분 성공이 포함될 수 있으므로 [취소 후 신규 주문](#취소-후-신규-주문)의 단계별 결과를 확인합니다.

불명확한 결과는 다시 생성하기 전에 조회로 확인합니다.

| 호출 | 확인 방법 |
|---|---|
| `NewOrder`, `SOROrder` | `QueryOrder`를 `OriginalClientOrderID`로 호출합니다. 주문이 보이면 접수된 것이며 `OpenOrders`, `AllOrders`, `MyTrades`로 체결을 확인합니다. SOR은 `MyAllocations`도 확인합니다. |
| `CancelOrder` | `QueryOrder`로 `CANCELED` 여부를 확인합니다. 아직 미체결이면 다시 취소합니다. |
| `CancelOpenOrders` | `OpenOrders`와 `OpenOrderLists`로 남은 미체결을 확인합니다. |
| `CancelReplaceOrder` | 취소 대상 주문과 새 주문(`NewOrder.ClientOrderID`)을 각각 `QueryOrder`로 확인합니다. |
| `AmendOrderKeepPriority` | `QueryOrder`와 `OrderAmendments`로 수량이 줄었는지 확인합니다. `NewClientOrderID`를 지정하지 않으면 정정 뒤 ID가 서버 생성 값으로 바뀌므로 항상 지정합니다. |
| `New*OrderList` | `QueryOrderList`를 `ListClientOrderID`로 호출하고 `OpenOrderLists`도 확인합니다. |
| `CancelOrderList` | `QueryOrderList`로 `ListOrderStatus`를 확인합니다. |
| `TestOrder`, `TestSOROrder` | 주문을 만들지 않으므로 상태 조정이 필요 없고 다시 호출해도 됩니다. |

```go
const clientOrderID = "strategy-a-20261120-0001"

order, err := client.NewOrder(ctx, binance.NewOrderRequest{
	Symbol:        "BTCUSDT",
	Side:          binance.SideBuy,
	Type:          binance.OrderTypeLimit,
	TimeInForce:   binance.TimeInForceGTC,
	Quantity:      "0.001",
	Price:         "60000.00",
	ClientOrderID: clientOrderID,
	ResponseType:  binance.NewOrderResponseResult,
})
if errors.Is(err, trade.ErrUnknownExecutionState) {
	// 접수 여부를 모르므로 재전송하지 않고 같은 ClientOrderID로 먼저 조회합니다.
	order, err = client.QueryOrder(ctx, binance.QueryOrderRequest{
		Symbol:                "BTCUSDT",
		OriginalClientOrderID: clientOrderID,
	})
}
if err != nil {
	return err
}
```

- 조회에서 주문이 없다고 나오면(`-2013`, `ORDER_NOT_FOUND`) 접수되지 않은 것으로 보고 다시 보낼 수 있습니다. 이때는 처음과 같은 `ClientOrderID`로 보냅니다.
- 공식 문서는 `newClientOrderId`를 미체결 주문 사이에서만 유일한 값으로 정의하며, 같은 ID는 이전 주문이 체결된 뒤에만 수락된다고 밝힙니다. 이전 주문이 아직 미체결이면 재전송은 `-2010 Duplicate order sent.`로 거부되어 중복을 막습니다. 반대로 이전 주문이 이미 체결되었다면 같은 ID의 주문이 새로 수락될 수 있으므로, 조회로 확인하기 전에는 재전송하지 않습니다.
- 시장가, IOC, FOK처럼 즉시 종료되는 주문은 같은 ID 보호를 기대할 수 없습니다. 처음 요청이 늦게 처리되었을 가능성이 사라질 때까지 User Data Stream과 `MyTrades`로 체결을 확인한 뒤에만 재전송합니다.
- 실시간으로 확인하려면 User Data Stream의 `executionReport`를 함께 사용합니다. 공식 문서도 `-1007`을 받으면 User Data Stream에 상태가 나타나지 않았는지 보고 API 조회로 확인하라고 안내합니다. 사용법은 [Binance Spot WebSocket](BINANCE_WEBSOCKET.md)을 참고합니다.

## 오류 분류

`trade.APIError`는 HTTP 상태를 `HTTPStatus`, Binance 코드를 `ExchangeCode`, `msg`를 `ExchangeMessage`에 보존하고 `X-MBX-UUID`(없으면 `X-Response-ID`) 응답 헤더를 `RequestID`로 담습니다. 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

| 조건 | SDK 분류 |
|---|---|
| HTTP 429, HTTP 418, `-1003` | `RATE_LIMITED`, 재시도 가능 표시 |
| HTTP 401, `-1002`, `-1022`, `-2014`, `-2015` | `AUTHENTICATION` |
| HTTP 403 | `AUTHORIZATION` |
| `-2011`, `-2013`, `-2026` | `ORDER_NOT_FOUND` |
| `-2010`이고 메시지에 `insufficient` 포함 | `INSUFFICIENT_BALANCE` |
| mutation의 HTTP 5xx, `-1007` | `UNKNOWN_EXECUTION_STATE` |
| 읽기의 HTTP 5xx, `-1007` | `EXCHANGE_UNAVAILABLE`, 재시도 가능 표시 |
| 그 밖의 HTTP 4xx, `-1013`~`-1199`, `-1021` | `VALIDATION` |
| 그 밖 | `INTERNAL` |

표의 위쪽 조건이 먼저 적용됩니다. `-1022`는 `-1013`~`-1199` 범위지만 `AUTHENTICATION`으로 분류됩니다. `-2011`은 취소 거절 전반에 쓰이므로 `ExchangeMessage`로 원인(`Unknown order sent.`, `Order was not canceled due to cancel restrictions.`)을 구분합니다. `-2010`의 `Duplicate order sent.`는 HTTP 400이라 `VALIDATION`입니다.

HTTP 403은 WAF 규칙 위반을 뜻할 수 있으므로 요청 제한 위반과 별도로 `AUTHORIZATION`으로 분류됩니다. 요청에 SQL 키워드가 들어가면 WAF에 막힐 수 있으므로 `ClientOrderID` 같은 문자열에 사용하지 않습니다.

## 입력 검증과 SDK 제한

- 요청 구조체의 값은 전송 전에 검증하며 위반하면 네트워크 요청 없이 `errors.Is(err, trade.ErrValidation)`인 오류를 반환합니다.
- 시각 입력은 `*time.Time`이며 millisecond로 전송합니다. 응답의 시각 필드는 Binance 기본인 millisecond이며 `X-MBX-TIME-UNIT`(microsecond)을 지원하지 않습니다.
- ID 입력은 `*int64`이고 nil이면 파라미터를 보내지 않습니다.
- 응답 구조체의 `Raw`에 원본 JSON을 보존하므로 typed 필드에 없는 값은 `Raw`에서 decode합니다.
- `SelfTradePreventionMode`, `PegPriceType`, `PegOffsetType`은 문자열이며 SDK가 값을 열거형으로 제한하지 않습니다. 허용 값은 심볼 설정(`Symbol.AllowedSelfTradePreventionModes`)과 공식 문서를 따릅니다.
- `-1006`(`UNEXPECTED_RESP`)은 공식 문서가 `Execution status unknown`으로 설명하지만, SDK는 `-1007`과 HTTP 5xx만 `UNKNOWN_EXECUTION_STATE`로 분류합니다. mutation이 `ExchangeCode` `-1006`으로 실패하면 HTTP 상태와 관계없이 상태를 모르는 것으로 보고 위의 조회 절차를 따릅니다.

공식 문서에 있지만 SDK 요청 구조체에 아직 없는 파라미터는 다음과 같습니다.

| 항목 | 현재 동작 |
|---|---|
| `quoteNotionalLimit` (`POST /api/v3/order`, `POST /api/v3/order/test`, `POST /api/v3/order/cancelReplace`) | `NewOrderRequest`, `TestOrderRequest`, `CancelReplaceOrderRequest`에는 필드가 없습니다. 주문 리스트의 `OrderListLeg.QuoteNotionalLimit`만 지원합니다. |
| `symbolStatus` (`GET /api/v3/depth`) | `OrderBookRequest`에 필드가 없습니다. |
| `symbols`, `permissions`, `showPermissionSets`, `symbolStatus` (`GET /api/v3/exchangeInfo`) | `ExchangeInfoRequest`는 `Symbol`만 지원합니다. |

지원하지 않는 범위는 다음과 같습니다.

- 사용 중단된 `POST /api/v3/order/oco`
- RSA·Ed25519 서명, SBE 응답, FIX API
- WebSocket API(`ws-api.binance.com`) 요청·응답 방식의 주문과 조회
- SAPI 상품군(Margin, Wallet, Sub-account, Convert, Earn 등)

## 관련 문서

- [Binance Spot WebSocket](BINANCE_WEBSOCKET.md): market·user data stream, 로컬 오더북
- [공통 Spot API](../UNIFIED_SPOT.md): `NewUnifiedSpot`
- [Binance 공개 데이터 덤프](BINANCE_ARCHIVE.md): REST 한도를 쓰지 않는 과거 캔들·체결 백필
- [요청별 송신 IP 사용법](../EGRESS_IP_USAGE.md): `DefaultEgressRouteID`와 `trade.WithEgressRoute`

## 공식 기준 문서

weight, 주문 count와 파라미터 규칙은 공식 GitHub 저장소 `binance-spot-api-docs`의 REST 문서와 2026-11-19에 받은 `exchangeInfo` 응답으로 확인했습니다. `developers.binance.com`은 자동 조회가 WAF에 막혀 직접 읽지 못했으므로 아래 링크는 공식 문서의 안내 주소입니다.

- [Binance Spot REST General API Information](https://developers.binance.com/en/docs/products/spot/rest-api#general-api-information)
- [Binance Spot REST Limits](https://developers.binance.com/en/docs/products/spot/rest-api#limits)
- [Binance Spot Market Data REST API](https://developers.binance.com/en/docs/catalog/core-trading-spot-trading/api/rest-api/market)
- [Binance Spot Trade REST API](https://developers.binance.com/en/docs/catalog/core-trading-spot-trading/api/rest-api/trade)
- [Binance Spot Account REST API](https://developers.binance.com/en/docs/catalog/core-trading-spot-trading/api/rest-api/account)
- [Binance Spot Errors](https://developers.binance.com/en/docs/products/spot/errors)
- [Binance Spot Enums](https://developers.binance.com/en/docs/products/spot/enums)
- [Binance Spot Filters](https://developers.binance.com/en/docs/products/spot/filters)
- [Binance Spot User Data Stream](https://developers.binance.com/en/docs/products/spot/user-data-stream)
- [binance-spot-api-docs rest-api.md](https://github.com/binance/binance-spot-api-docs/blob/master/rest-api.md)
- [binance-spot-api-docs CHANGELOG.md](https://github.com/binance/binance-spot-api-docs/blob/master/CHANGELOG.md)
- [Spot Unfilled Order Count Rules](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/order_count_decrement.md)
- [Smart Order Routing FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/sor_faq.md)
- [Order Amend Keep Priority FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/order_amend_keep_priority.md)
- [Pegged Orders FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/pegged_orders.md)
- [OPO FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/opo.md)
- [Quote Notional Limit FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/quoteNotionalLimit.md)
- [Self Trade Prevention FAQ](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/stp_faq.md)
- [Market Data Only URLs](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/market_data_only.md)
