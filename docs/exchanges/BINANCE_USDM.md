# Binance USDⓈ-M Futures REST·WebSocket 어댑터

## 패키지와 전제조건

USDⓈ-M Futures는 Spot과 base URL, 상품 모델, 주문 제한, 포지션 의미가 달라 `exchange/binance/usdm` 패키지로 분리합니다. 기본 REST 주소는 `https://fapi.binance.com`입니다. WebSocket은 2026년 분리 정책에 따라 호가용 `/public`, 일반 시세용 `/market`, 계정용 `/private` 진입점을 각각 사용합니다.

private API는 `credential.Material.APIKey`와 `credential.Material.SecretKey`를 사용합니다. `credential.Descriptor.Exchange`는 Spot과 같은 `model.ExchangeBinance`이며, `AccountID`에는 Binance 계정 또는 제한을 공유하는 계정 식별자를 넣습니다.

자격증명의 `AllowedEgressRouteIDs` 밖 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 |
|---|---|
| 연결·시간 | `Ping`, `ServerTime` |
| 계약·시세 | `ExchangeInfo`, `TickerPrice`, `OrderBook`, `RecentTrades`, `Candles` |
| 체결·캔들 확장 | `AggregateTrades`, `HistoricalTrades`, `ContinuousCandles`, `IndexPriceCandles`, `MarkPriceCandles`, `PremiumIndexCandles` |
| 가격·펀딩 | `MarkPrices`, `FundingRateHistory`, `FundingInfo`, `Ticker24hr`, `BookTickers` |
| 미결제약정·통계 | `OpenInterest`, `OpenInterestHistory`, `TopTraderAccountRatio`, `TopTraderPositionRatio`, `GlobalLongShortAccountRatio`, `TakerBuySellVolumes`, `Basis`, `DeliveryPrices` |
| 지수·위험·일정 | `CompositeIndexInfo`, `AssetIndices`, `IndexConstituents`, `InsuranceFundBalance`, `SymbolAdlRisks`, `RPIOrderBook`, `TradingSchedule` |
| 계정·포지션 | `Account`, `Positions`, `ChangeMarginType` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `OrderHistory` |
| 주문 확장 | `TestOrder`, `OpenOrder`, `ModifyOrder`, `PlaceBatchOrders`, `ModifyBatchOrders`, `CancelBatchOrders`, `CancelAllOpenOrders`, `CancelAllAlgoOpenOrders`, `CountdownCancelAll`, `OrderAmendments` |
| 포지션·마진 설정 | `ChangeLeverage`, `ChangePositionMode`, `ChangeMultiAssetsMode`, `ModifyPositionMargin`, `PositionMarginHistory` |
| 체결·청산 조회 | `UserTrades`, `ForceOrders`, `ADLQuantiles` |
| 계정 확장 | `Balances`, `AccountConfig`, `SymbolConfigs`, `PositionMode`, `MultiAssetsMode`, `IncomeHistory`, `CommissionRate`, `LeverageBrackets`, `OrderRateLimits`, `APITradingStatus`, `FeeBurn`, `ChangeFeeBurn`, `ClassicPortfolioMarginAccount`, `SignTradFiPerpsContract` |
| 이력 다운로드 | `RequestOrderHistoryDownload`, `OrderHistoryDownloadLink`, `RequestTradeHistoryDownload`, `TradeHistoryDownloadLink`, `RequestIncomeHistoryDownload`, `IncomeHistoryDownloadLink` |
| Convert | `ConvertPairs`, `RequestConvertQuote`, `AcceptConvertQuote`, `ConvertOrderStatus` |
| Algo 조건부 주문 | `PlaceAlgoOrder`, `AlgoOrderInfo`, `CancelAlgoOrder`, `OpenAlgoOrders`, `AlgoOrderHistory` |
| private 접속 키 | `StartUserDataStream`, `KeepaliveUserDataStream`, `CloseUserDataStream` |
| WebSocket | `MarketStream`, `UserDataStream`, 동적 구독·구독 해제 |

계정은 `/fapi/v3/account`, 포지션 위험은 `/fapi/v3/positionRisk`를 사용합니다. `ChangeMarginType`은 `POST /fapi/v1/marginType`으로 계약별 `ISOLATED`·`CROSSED` 모드를 변경합니다. 주문 정정·일괄 주문·전체 취소, 레버리지·포지션 모드·격리 마진 설정, 수입 이력·수수료율·이력 다운로드 같은 계정 조회와 Futures Convert는 권한과 제한을 [Trading·Account REST 확장](#tradingaccount-rest-확장)에 정리했습니다. 체결·캔들 확장부터 지수·위험·일정까지 네 영역의 Market Data 메서드는 엔드포인트와 권한, 제한을 [Market Data REST 확장](#market-data-rest-확장)에 정리했습니다.

## 인증과 시간

SIGNED 요청은 URL 인코딩한 최종 파라미터에 HMAC SHA-256을 적용합니다. 실행 순서는 다음과 같습니다.

1. 자격증명의 route와 권한을 검사합니다.
2. IP weight와 계정 주문 count 제한을 확보합니다.
3. Secret을 조회합니다.
4. 보정된 현재 시간과 `recvWindow`를 넣습니다.
5. 최종 쿼리에 서명하고 선택한 송신 경로로 전송합니다.

기본 `recvWindow`는 5초이며 최대 1분까지 설정할 수 있습니다. `ServerTime`은 요청 왕복 중간 시점을 기준으로 clock offset을 보정합니다.

## 요청 제한

초기 안전값은 현재 공식 기본 규칙을 사용합니다.

| 제한 | 초기값 | 범위 |
|---|---:|---|
| 요청 weight | 2400/분 | 송신 경로 |
| 주문 count | 300/10초 | 계정 |
| 주문 count | 1200/분 | 계정 |

`ExchangeInfo` 응답의 `REQUEST_WEIGHT`와 `ORDERS` 규칙으로 값을 동적으로 갱신합니다. 응답의 `X-MBX-USED-WEIGHT-1M`, `X-MBX-ORDER-COUNT-10S`, `X-MBX-ORDER-COUNT-1M`도 로컬 limiter에 반영합니다.

주문 생성은 IP weight를 소비하지 않지만 계정 주문 count를 소비합니다. 여러 공인 송신 IP를 사용해도 계정 주문 제한은 늘어나지 않습니다. 일괄 주문·정정은 10초 창에 5, 1분 창에 1의 주문 count와 IP weight 5를 함께 차감합니다.

펀딩과 `/futures/data` 통계처럼 엔드포인트 전용 IP 5분 제한이 있는 Market Data 호출은 요청 weight와 별도로 송신 경로별 규칙을 차감합니다. 규칙과 값은 [Market Data REST 확장](#market-data-rest-확장)을 참고하세요.

## 포지션 모드와 주문 검증

- 단방향 모드는 `PositionSideBoth` 또는 빈 값을 사용합니다.
- 헤지 모드는 `PositionSideLong` 또는 `PositionSideShort`를 명시합니다.
- 헤지 모드 주문에는 `reduceOnly`를 함께 보내지 않습니다.
- `closePosition`은 `STOP_MARKET` 또는 `TAKE_PROFIT_MARKET`에서 사용하며 `quantity`, `reduceOnly`와 함께 보낼 수 없습니다.
- GTD 주문은 현재 시각보다 최소 600초 뒤의 `GoodTillDate`가 필요합니다.
- 가격과 수량은 decimal 문자열로 입력하며 SDK가 자동 반올림하지 않습니다.

지원 주문 종류는 `LIMIT`, `MARKET`, `STOP`, `STOP_MARKET`, `TAKE_PROFIT`, `TAKE_PROFIT_MARKET`, `TRAILING_STOP_MARKET`입니다.

## Algo 조건부 주문

Algo 주문 서비스는 `/fapi/v1/algoOrder` 계열 엔드포인트로 조건부 주문을 관리합니다.

| 메서드 | 엔드포인트 | 제한 |
|---|---|---|
| `PlaceAlgoOrder` | `POST /fapi/v1/algoOrder` | 계정 주문 count 1, IP weight 0 |
| `AlgoOrderInfo` | `GET /fapi/v1/algoOrder` | IP weight 1 |
| `CancelAlgoOrder` | `DELETE /fapi/v1/algoOrder` | IP weight 1 |
| `OpenAlgoOrders` | `GET /fapi/v1/openAlgoOrders` | 단일 계약 1, 전체 40 |
| `AlgoOrderHistory` | `GET /fapi/v1/allAlgoOrders` | IP weight 5 |

- `AlgoType`은 현재 `CONDITIONAL`만 지원하며 비우면 `CONDITIONAL`로 전송합니다.
- 지원 주문 종류는 `STOP`, `TAKE_PROFIT`, `STOP_MARKET`, `TAKE_PROFIT_MARKET`, `TRAILING_STOP_MARKET`입니다.
- 발동 가격은 `stopPrice`가 아니라 `TriggerPrice`(`triggerPrice`)로 전송합니다.
- `STOP`·`TAKE_PROFIT`은 `Quantity`, `TriggerPrice`, 그리고 `Price` 또는 `PriceMatch` 중 하나가 필요합니다.
- `STOP_MARKET`·`TAKE_PROFIT_MARKET`은 `TriggerPrice`와 `Quantity` 또는 `ClosePosition`이 필요합니다.
- `TRAILING_STOP_MARKET`은 `Quantity`와 `CallbackRate`가 필요합니다.
- 단건 조회와 취소는 `AlgoID` 또는 `ClientAlgoID` 중 정확히 하나로 지정하며 계약 심볼을 보내지 않습니다.
- 전체 계약 미체결 조회는 `OpenAlgoOrdersRequest.AllSymbols`를 명시해야 합니다.
- 취소 응답은 식별자만 포함할 수 있으므로 최종 상태는 `AlgoOrderInfo`로 확인합니다.

## Market Data REST 확장

공식 Market Data REST의 체결, 캔들, 가격·펀딩, 미결제약정·통계, 지수·위험·일정 엔드포인트를 `Client` 메서드로 제공합니다. 모두 `OperationRead` 조회이며 요청 구조체의 `validate()`가 네트워크 전에 값을 검증해 잘못된 입력은 `trade.ErrValidation`으로 거부합니다. 가격과 수량은 decimal 문자열 그대로 보존하고, 목록·객체 응답은 항목마다 원본 JSON `Raw`를 함께 담습니다.

권한 열의 값은 다음 뜻입니다.

| 권한 | 의미 |
|---|---|
| 공개 | 자격증명 없이 호출합니다. 서명, `X-MBX-APIKEY`, Secret 조회와 권한 검사를 모두 하지 않습니다. |
| `credential.PermissionRead` | 공식 `MARKET_DATA` 엔드포인트입니다. 서명과 timestamp 없이 `X-MBX-APIKEY`만 보냅니다. 자격증명이 없으면 `trade.ErrAuthentication`, 허용되지 않은 route이거나 `PermissionRead`가 없으면 Secret 조회 전에 `trade.ErrAuthorization`을 반환합니다. |

### 체결

| 메서드 | 엔드포인트 | 권한 | IP 제한 |
|---|---|---|---|
| `AggregateTrades` | `GET /fapi/v1/aggTrades` | 공개 | weight 20 |
| `HistoricalTrades` | `GET /fapi/v1/historicalTrades` | `credential.PermissionRead` | weight 200 |

- `AggregateTradesRequest`는 `FromID`와 `StartTime`·`EndTime`을 함께 보낼 수 없습니다. 두 시각을 모두 보내면 간격이 1시간 미만이어야 하고, 최근 48시간 체결만 조회됩니다. `Limit` 최대는 1,000입니다.
- `HistoricalTradesRequest`는 `FromID`를 비우면 가장 최근 체결부터 반환하며 최근 1개월 체결만 조회됩니다. `Limit` 최대는 500이고 기존 `RecentTrades`와 같은 `PublicTrade`를 반환합니다.

### 캔들

| 메서드 | 엔드포인트 | 권한 | IP 제한 |
|---|---|---|---|
| `ContinuousCandles` | `GET /fapi/v1/continuousKlines` | 공개 | `Limit`별 weight |
| `IndexPriceCandles` | `GET /fapi/v1/indexPriceKlines` | 공개 | `Limit`별 weight |
| `MarkPriceCandles` | `GET /fapi/v1/markPriceKlines` | 공개 | `Limit`별 weight |
| `PremiumIndexCandles` | `GET /fapi/v1/premiumIndexKlines` | 공개 | `Limit`별 weight |

- weight는 `Limit` 1~99는 1, 100~499는 2, 500~1,000은 5, 1,001 이상은 10이며 `Limit` 최대는 1,500입니다.
- `Limit`을 비우면 서버 기본값 500이 적용되므로 weight 5를 차감합니다.
- `ContinuousCandles`만 `Candle1Second`(`1s`)와 `ContractTypeTradFiPerpetual`을 허용합니다. 나머지 세 메서드는 `Candles`와 같은 구간만 허용합니다.
- 지수·마크가·프리미엄 지수 캔들의 거래량과 거래 건수 계열 필드는 공식 문서가 무시하라고 밝힌 값입니다.

### 가격·펀딩

| 메서드 | 엔드포인트 | 권한 | IP 제한 |
|---|---|---|---|
| `MarkPrices` | `GET /fapi/v1/premiumIndex` | 공개 | `Symbol` 지정 1, 생략 10 |
| `FundingRateHistory` | `GET /fapi/v1/fundingRate` | 공개 | weight 0, 펀딩 500회/5분 공유 |
| `FundingInfo` | `GET /fapi/v1/fundingInfo` | 공개 | weight 0, 펀딩 500회/5분 공유 |
| `Ticker24hr` | `GET /fapi/v1/ticker/24hr` | 공개 | `Symbol` 지정 1, 생략 40 |
| `BookTickers` | `GET /fapi/v1/ticker/bookTicker` | 공개 | `Symbol` 지정 2, 생략 5 |

- `Symbol`을 생략할 수 있는 메서드는 공식 응답이 객체 하나 또는 배열로 달라지므로 항상 목록을 반환합니다. 단일 계약 응답도 한 건짜리 목록입니다.
- `FundingRateHistory`와 `FundingInfo`는 요청 weight를 쓰지 않지만 공식 문서가 두 엔드포인트의 IP당 500회/5분 제한을 공유한다고 밝힙니다. SDK는 송신 경로별 `funding` 규칙(500/5분)을 만들어 호출마다 1을 차감합니다.
- `FundingRateHistoryRequest`는 시각을 모두 생략하면 최근 200건을 오름차순으로 반환하며 `Limit` 최대는 1,000입니다.
- `bookTicker` 응답의 `X-MBX-USED-WEIGHT-1M`은 공식 문서가 부정확하다고 밝히므로 limiter 관측에서 제외하고 로컬 차감값만 사용합니다.
- `Ticker24hr`와 `MarkPrices`의 전체 조회는 weight가 크므로 필요한 계약만 지정하는 것이 안전합니다.

### 미결제약정·통계

| 메서드 | 엔드포인트 | 권한 | IP 제한 |
|---|---|---|---|
| `OpenInterest` | `GET /fapi/v1/openInterest` | 공개 | weight 1 |
| `OpenInterestHistory` | `GET /futures/data/openInterestHist` | 공개 | weight 0, 통계 1,000회/5분 |
| `TopTraderAccountRatio` | `GET /futures/data/topLongShortAccountRatio` | `credential.PermissionRead` | weight 0, 통계 1,000회/5분 |
| `TopTraderPositionRatio` | `GET /futures/data/topLongShortPositionRatio` | `credential.PermissionRead` | weight 0, 통계 1,000회/5분 |
| `GlobalLongShortAccountRatio` | `GET /futures/data/globalLongShortAccountRatio` | 공개 | weight 0, 통계 1,000회/5분 |
| `TakerBuySellVolumes` | `GET /futures/data/takerlongshortRatio` | 공개 | weight 0, 통계 1,000회/5분 |
| `Basis` | `GET /futures/data/basis` | 공개 | weight 0 |
| `DeliveryPrices` | `GET /futures/data/delivery-price` | 공개 | weight 0 |

- 통계 다섯 메서드는 `MarketStatisticsRequest`를 공유합니다. `Period`는 `StatisticsPeriod5Minutes`부터 `StatisticsPeriod1Day`까지(`5m`, `15m`, `30m`, `1h`, `2h`, `4h`, `6h`, `12h`, `1d`) 필수이고 `Limit` 최대는 500, 서버 기본값은 30입니다. 시각을 생략하면 최근 데이터를 반환하며 최근 30일(미결제약정 이력은 1개월)만 조회됩니다.
- 공식 문서는 통계 다섯 엔드포인트에 IP당 1,000회/5분 제한만 적고 엔드포인트별인지 공유인지는 밝히지 않습니다. SDK는 보수적으로 송신 경로별 `futures_data` 규칙(1,000/5분) 하나를 공유해 호출마다 1을 차감합니다. `Basis`와 `DeliveryPrices`는 문서에 weight 0만 적혀 있어 5분 제한을 차감하지 않습니다.
- `OpenInterestHistory`는 거래가 없던 구간이 응답에서 빠집니다.
- `BasisRequest`는 `ContractTypePerpetual`, `ContractTypeCurrentQuarter`, `ContractTypeNextQuarter`만 허용하고 `Period`와 `Limit`은 통계와 같습니다.
- `DeliveryPrice.DeliveryPrice`는 JSON 숫자 표기를 보존하는 `json.Number`입니다.

### 지수·위험·일정

| 메서드 | 엔드포인트 | 권한 | IP 제한 |
|---|---|---|---|
| `CompositeIndexInfo` | `GET /fapi/v1/indexInfo` | 공개 | weight 1 |
| `AssetIndices` | `GET /fapi/v1/assetIndex` | 공개 | `Symbol` 지정 1, 생략 10 |
| `IndexConstituents` | `GET /fapi/v1/constituents` | 공개 | weight 2 |
| `InsuranceFundBalance` | `GET /fapi/v1/insuranceBalance` | 공개 | weight 1 |
| `SymbolAdlRisks` | `GET /fapi/v1/symbolAdlRisk` | 공개 | weight 1 |
| `RPIOrderBook` | `GET /fapi/v1/rpiDepth` | 공개 | weight 20 |
| `TradingSchedule` | `GET /fapi/v1/tradingSchedule` | 공개 | weight 5 |

- `RPIOrderBookRequest.Limit`은 비우거나 1000만 허용합니다. 기존 `OrderBook`과 같은 `OrderBook` 타입으로 반환하며 RPI(Retail Price Improvement) 주문을 집계해 포함하고 교차 가격 구간은 숨겨집니다.
- `SymbolAdlRisks`의 `AdlRisk`는 `high`, `medium`, `low` 중 하나이며 30분마다 갱신됩니다.
- `IndexConstituents`는 TradFi 무기한 계약의 구성 가격을 공식 문서대로 `-1`로 숨겨 반환합니다.
- `TradingSchedule`의 `MarketSchedules`는 `EQUITY`, `COMMODITY`, `KR_EQUITY`, `HK_EQUITY`, `CN_EQUITY`, `FX` 같은 시장 분류를 키로 하는 세션 목록입니다.

```go
ratios, err := client.TopTraderPositionRatio(ctx, usdm.MarketStatisticsRequest{
	Symbol: "BTCUSDT",
	Period: usdm.StatisticsPeriod1Hour,
	Limit:  24,
})
if err != nil {
	return err
}

candles, err := client.ContinuousCandles(ctx, usdm.ContinuousCandlesRequest{
	Pair:         "BTCUSDT",
	ContractType: usdm.ContractTypePerpetual,
	Interval:     usdm.Candle1Minute,
	Limit:        500,
}, trade.WithEgressRoute("seoul-b"))
```

## Trading·Account REST 확장

공식 Trade·Account REST의 주문 정정·일괄·전체 취소, 포지션·레버리지·마진 설정, 계정·수수료·수입 이력 조회와 Futures Convert를 `Client` 메서드로 제공합니다. 요청 구조체의 `validate()`가 네트워크 전에 값을 검증해 잘못된 입력은 `trade.ErrValidation`으로 거부하고, 응답은 항목마다 원본 JSON `Raw`를 함께 담습니다. 가격·수량·잔고는 decimal 문자열 그대로 보존합니다.

모든 서명 요청은 자격증명의 route와 권한을 Secret 조회 전에 검사합니다. 권한 열의 값은 다음 뜻입니다.

| 권한 | 실행 종류 | 대상 |
|---|---|---|
| `credential.PermissionRead` | `OperationRead` | 조회 전용 엔드포인트입니다. 읽기 요청이므로 공통 실행기의 제한적 재시도가 적용됩니다. |
| `credential.PermissionTrade` | `OperationMutation` | 주문 생성·테스트·정정·취소, 레버리지·포지션 모드·멀티에셋 모드·격리 마진·BNB 수수료 차감 설정, TradFi Perps 약관 서명입니다. 자동 재시도하지 않습니다. |
| `credential.PermissionTransfer` | `OperationMutation` | Convert 견적 요청·수락입니다. 자동 재시도하지 않습니다. |
| 공개 | `OperationRead` | `ConvertPairs`입니다. 서명, `X-MBX-APIKEY`, Secret 조회와 권한 검사를 모두 하지 않습니다. |

불명확한 전송 결과(timeout, HTTP 5xx)는 변경 요청에서 `trade.ErrorUnknownExecutionState`로 반환합니다. 같은 요청을 다시 보내기 전에 조회 메서드로 상태를 확인합니다.

### 주문 확장

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `TestOrder` | `POST /fapi/v1/order/test` | `credential.PermissionTrade` | 차감 없음 |
| `OpenOrder` | `GET /fapi/v1/openOrder` | `credential.PermissionRead` | weight 1 |
| `ModifyOrder` | `PUT /fapi/v1/order` | `credential.PermissionTrade` | 주문 count 1, weight 0 |
| `PlaceBatchOrders` | `POST /fapi/v1/batchOrders` | `credential.PermissionTrade` | 주문 count 10초 5·1분 1, weight 5 |
| `ModifyBatchOrders` | `PUT /fapi/v1/batchOrders` | `credential.PermissionTrade` | 주문 count 10초 5·1분 1, weight 5 |
| `CancelBatchOrders` | `DELETE /fapi/v1/batchOrders` | `credential.PermissionTrade` | weight 1 |
| `CancelAllOpenOrders` | `DELETE /fapi/v1/allOpenOrders` | `credential.PermissionTrade` | weight 1 |
| `CancelAllAlgoOpenOrders` | `DELETE /fapi/v1/algoOpenOrders` | `credential.PermissionTrade` | weight 1 |
| `CountdownCancelAll` | `POST /fapi/v1/countdownCancelAll` | `credential.PermissionTrade` | weight 10 |
| `OrderAmendments` | `GET /fapi/v1/orderAmendment` | `credential.PermissionRead` | weight 1 |

- `TestOrder`는 `PlaceOrder`와 같은 `PlaceOrderRequest`와 검증을 쓰며 주문을 매칭 엔진에 보내지 않습니다. 공식 문서에 별도 weight가 없어 주문 count와 IP weight를 차감하지 않습니다.
- `ModifyOrder`는 미체결 `LIMIT` 주문만 정정합니다. 주문은 `OrderID` 또는 `ClientOrderID` 중 하나로, 가격은 `Price` 또는 `PriceMatch` 중 하나로 지정하며 `Quantity`는 필수입니다. `ModifyID`는 서버가 그대로 돌려주는 사용자 정의 식별자이고 `ReduceOnly`는 최소 주문 금액 검증을 건너뛰는 데만 쓰입니다. 한 주문은 10,000회 미만만 정정할 수 있습니다.
- 일괄 요청은 한 번에 최대 5건이고 항목은 동시에 처리되어 체결 순서가 보장되지 않습니다. 응답 목록은 요청 순서와 같으며 항목마다 `BatchOrderResult.Failed()`로 거부 여부를 확인합니다. 거부된 항목은 주문 필드 없이 음수 `Code`와 `Message`만 채워집니다.
- 일괄 주문 항목은 `PlaceOrderRequest`를 그대로 쓰므로 `PriceMatch`, `SelfTradePreventionMode`와 `GTX`·`RPI` timeInForce는 지원하지 않습니다. 일괄 정정은 `ReduceOnly`를 지원하지 않습니다. 항목 값은 모두 문자열로 직렬화해 `batchOrders` JSON 배열로 보냅니다.
- `CancelBatchOrders`는 한 계약의 `OrderIDs` 또는 `ClientOrderIDs` 중 하나만 최대 10건 받습니다.
- `CancelAllOpenOrders`는 일반 주문만, `CancelAllAlgoOpenOrders`는 Algo 조건부 주문(TP/SL·트레일링 스톱 포함)만 취소합니다. 응답 `AckResult.Code`가 200이면 성공이며, 문자열 `"200"`으로 와도 정수로 해석합니다.
- `CountdownCancelAll`은 하트비트처럼 반복 호출해 카운트다운을 새 값으로 교체합니다. `Countdown`을 0으로 보내면 중지하며, 서버가 약 10밀리초마다 확인하므로 지나치게 짧은 값은 피합니다.
- `OrderAmendments`는 `OrderID` 또는 `ClientOrderID` 중 하나로 정정 이력을 조회합니다. `Limit` 최대는 100이고 3개월보다 오래된 이력은 조회되지 않습니다.

### 포지션·마진 설정

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `ChangeLeverage` | `POST /fapi/v1/leverage` | `credential.PermissionTrade` | weight 1 |
| `ChangePositionMode` | `POST /fapi/v1/positionSide/dual` | `credential.PermissionTrade` | weight 1 |
| `ChangeMultiAssetsMode` | `POST /fapi/v1/multiAssetsMargin` | `credential.PermissionTrade` | weight 1 |
| `ModifyPositionMargin` | `POST /fapi/v1/positionMargin` | `credential.PermissionTrade` | weight 1 |
| `PositionMarginHistory` | `GET /fapi/v1/positionMargin/history` | `credential.PermissionRead` | weight 1 |

- 포지션 모드와 멀티에셋 모드는 전체 계약에 적용되며 미체결 주문이나 포지션이 있으면 거래소가 거부할 수 있습니다.
- `ModifyPositionMargin`은 격리 마진 계약 전용입니다. `Type`은 `PositionMarginAdd`(1)와 `PositionMarginReduce`(2)이고 헤지 모드에서는 `PositionSide`를 지정해야 합니다.
- `PositionMarginHistory`의 공식 보안 등급은 TRADE이지만 조회만 하므로 SDK는 `credential.PermissionRead`로 검사하고 `OperationRead`로 실행합니다. API Key에는 Futures 권한이 있어야 합니다. 최근 30일 이력만 조회되고 두 시각의 간격도 30일을 넘을 수 없습니다.

### 체결·청산 조회

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `UserTrades` | `GET /fapi/v1/userTrades` | `credential.PermissionRead` | weight 5 |
| `ForceOrders` | `GET /fapi/v1/forceOrders` | `credential.PermissionRead` | `Symbol` 지정 20, 생략 50 |
| `ADLQuantiles` | `GET /fapi/v1/adlQuantile` | `credential.PermissionRead` | weight 5 |

- `UserTrades`는 `FromID`와 `StartTime`·`EndTime`을 함께 보낼 수 없습니다. 시각을 모두 생략하면 최근 7일을 조회하고 두 시각의 간격은 7일을 넘을 수 없으며 최근 3개월만 조회됩니다. `Limit` 최대는 1,000입니다.
- `ForceOrders`는 `AutoCloseType`을 비우면 `LIQUIDATION`과 `ADL`을 모두 반환하고 `StartTime`을 생략하면 `EndTime` 이전 7일을 조회합니다. `Limit` 최대는 100이며 기존 `Order` 타입으로 반환합니다.
- `ADLQuantile.Quantile`은 `LONG`, `SHORT`, `BOTH` 키별 0~4 값입니다. 교차 헤지 모드에서는 `BOTH` 대신 `HEDGE` 키가 오며 이 값은 표시일 뿐입니다.

### 계정 조회·설정

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `Balances` | `GET /fapi/v3/balance` | `credential.PermissionRead` | weight 5 |
| `AccountConfig` | `GET /fapi/v1/accountConfig` | `credential.PermissionRead` | weight 5 |
| `SymbolConfigs` | `GET /fapi/v1/symbolConfig` | `credential.PermissionRead` | weight 5 |
| `PositionMode` | `GET /fapi/v1/positionSide/dual` | `credential.PermissionRead` | weight 30 |
| `MultiAssetsMode` | `GET /fapi/v1/multiAssetsMargin` | `credential.PermissionRead` | weight 30 |
| `IncomeHistory` | `GET /fapi/v1/income` | `credential.PermissionRead` | weight 30 |
| `CommissionRate` | `GET /fapi/v1/commissionRate` | `credential.PermissionRead` | weight 20 |
| `LeverageBrackets` | `GET /fapi/v1/leverageBracket` | `credential.PermissionRead` | weight 1 |
| `OrderRateLimits` | `GET /fapi/v1/rateLimit/order` | `credential.PermissionRead` | weight 1 |
| `APITradingStatus` | `GET /fapi/v1/apiTradingStatus` | `credential.PermissionRead` | `Symbol` 지정 1, 생략 10 |
| `FeeBurn` | `GET /fapi/v1/feeBurn` | `credential.PermissionRead` | weight 30 |
| `ChangeFeeBurn` | `POST /fapi/v1/feeBurn` | `credential.PermissionTrade` | weight 1 |
| `ClassicPortfolioMarginAccount` | `GET /fapi/v1/pmAccountInfo` | `credential.PermissionRead` | weight 5 |
| `SignTradFiPerpsContract` | `POST /fapi/v1/stock/contract` | `credential.PermissionTrade` | weight 50 |

- `Balances`는 v3 잔고이며 기존 `Account`의 `assets`와 별개로 자산별 잔고만 가볍게 조회합니다.
- `SymbolConfigs`와 `LeverageBrackets`는 `Symbol`을 생략하면 전체 계약을 반환합니다. `LeverageBrackets`는 `Symbol` 지정 여부에 따라 응답이 객체 하나 또는 배열로 달라지므로 항상 목록을 반환하고, 공식 응답이 JSON 숫자로 주는 명목가와 유지 마진율은 `json.Number`로 보존합니다. `SymbolConfig.IsAutoAddMargin`은 불리언과 `"true"`·`"false"` 문자열을 모두 받습니다.
- `IncomeHistory`는 `IncomeType`을 비우면 모든 종류를 조회하고 시각을 모두 생략하면 최근 7일을 조회합니다. 최근 3개월만 조회되며 `Limit` 최대는 1,000입니다. 공식 문서에서 폐지된 `page` 파라미터는 지원하지 않으므로 `StartTime`·`EndTime`으로 구간을 옮깁니다.
- `APITradingStatus.Indicators`는 계약 심볼을 키로 하며 계정 단위 지표는 `ACCOUNT` 키로 옵니다.
- `ChangeFeeBurn`은 모든 계약에 적용되는 BNB 수수료 차감 설정입니다.
- `ClassicPortfolioMarginAccount`는 `Asset`이 필수이며 클래식 포트폴리오 마진 계정의 이체 가능 수량을 반환합니다.
- `SignTradFiPerpsContract`의 공식 보안 등급은 USER_DATA이지만 계정 상태를 바꾸는 요청이므로 SDK는 `credential.PermissionTrade`로 검사하고 `OperationMutation`으로 실행합니다.

### 이력 다운로드

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `RequestOrderHistoryDownload` | `GET /fapi/v1/order/asyn` | `credential.PermissionRead` | weight 1,000, 월 10회 |
| `OrderHistoryDownloadLink` | `GET /fapi/v1/order/asyn/id` | `credential.PermissionRead` | weight 10 |
| `RequestTradeHistoryDownload` | `GET /fapi/v1/trade/asyn` | `credential.PermissionRead` | weight 1,000, 월 5회 |
| `TradeHistoryDownloadLink` | `GET /fapi/v1/trade/asyn/id` | `credential.PermissionRead` | weight 10 |
| `RequestIncomeHistoryDownload` | `GET /fapi/v1/income/asyn` | `credential.PermissionRead` | weight 1,000, 월 100회 |
| `IncomeHistoryDownloadLink` | `GET /fapi/v1/income/asyn/id` | `credential.PermissionRead` | weight 10 |

- 요청 메서드의 구간은 필수이며 1년을 넘을 수 없습니다. 월 한도는 웹 다운로드 페이지와 공유되고 거래 내역 한도는 USDⓈ-M과 COIN-M이 함께 씁니다. SDK는 월 한도를 로컬에서 세지 않습니다.
- 요청 메서드는 월 한도와 weight 1,000을 소모하므로 서명 GET이지만 `OperationMutation`으로 실행해 자동 재시도하지 않습니다. 링크 메서드는 `OperationRead`입니다.
- `DownloadLink.Status`가 `completed`일 때만 `URL`이 채워지며 링크는 7일 동안 유효합니다.

### Convert

| 메서드 | 엔드포인트 | 권한 | 제한 |
|---|---|---|---|
| `ConvertPairs` | `GET /fapi/v1/convert/exchangeInfo` | 공개 | weight 20 |
| `RequestConvertQuote` | `POST /fapi/v1/convert/getQuote` | `credential.PermissionTransfer` | weight 50 |
| `AcceptConvertQuote` | `POST /fapi/v1/convert/acceptQuote` | `credential.PermissionTransfer` | weight 200 |
| `ConvertOrderStatus` | `GET /fapi/v1/convert/orderStatus` | `credential.PermissionRead` | weight 50 |

- `ConvertPairs`는 `FromAsset`과 `ToAsset`을 모두 비우면 일부 페어만 반환합니다.
- `ConvertQuoteRequest`는 `FromAmount`와 `ToAmount` 중 정확히 하나만 보내며 `ValidTime`을 비우면 서버 기본값 `10s`를 씁니다. `QuoteID`는 잔고가 충분할 때만 응답에 포함됩니다. 공식 서버 한도(시간당 360회, 일 500회)는 SDK가 로컬에서 세지 않습니다.
- 견적은 구속력이 없으며 `AcceptConvertQuote`로 수락해야 변환 주문이 만들어집니다. 수락 요청의 전송 결과가 불명확하면 `ConvertOrderStatus`로 먼저 확인합니다. `ConvertOrderRequest`는 `OrderID` 또는 `QuoteID` 중 정확히 하나를 받습니다.
- Convert 주문 ID는 숫자 또는 숫자 문자열로 오므로 `json.Number`로 보존합니다.

```go
quote, err := client.RequestConvertQuote(ctx, usdm.ConvertQuoteRequest{
	FromAsset:  "USDT",
	ToAsset:    "BTC",
	FromAmount: "20",
})
if err != nil {
	return err
}
acceptance, err := client.AcceptConvertQuote(ctx, usdm.AcceptConvertQuoteRequest{QuoteID: quote.QuoteID})

results, err := client.PlaceBatchOrders(ctx, usdm.PlaceBatchOrdersRequest{Orders: orders})
if err != nil {
	return err
}
for _, result := range results {
	if result.Failed() {
		log.Printf("주문 거부 %d: %s", result.Code, result.Message)
	}
}
```

## 과거 캔들 덤프

REST 캔들보다 오래된 구간의 대량 백필은 `exchange/binance/archive`의 `MarketUSDM` 월·일 덤프로 받습니다. 사용법과 게시 범위는 [Binance 공개 데이터 덤프 문서](BINANCE_ARCHIVE.md)를 참고하세요.

## HTTP 503과 주문 안전성

Binance Futures는 HTTP 503의 문구별 의미가 다릅니다.

| 응답 | SDK 분류 |
|---|---|
| `Unknown error, please check...` | `UNKNOWN_EXECUTION_STATE` |
| `Service Unavailable.` | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |
| `Internal error; unable to process...` | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |
| `-1008` system-level protection | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |

SDK 실행기는 mutation을 자동 재시도하지 않습니다. 특히 불명확한 결과는 사용자 주문 ID나 주문 조회, User Data Stream으로 확인하기 전 다시 생성하면 안 됩니다.

## public·market WebSocket

`StreamClient`는 데이터 종류에 따라 공식 분리 진입점을 선택합니다.

| route | 기본 주소 | 지원 helper |
|---|---|---|
| `StreamRoutePublic` | `wss://fstream.binance.com/public/stream` | `BookTickerStream`, `DiffDepthStream`, `PartialDepthStream` |
| `StreamRouteMarket` | `wss://fstream.binance.com/market/stream` | `AggregateTradeStream`, `MarkPriceStream`, `KlineStream`, `TickerStream` |

한 연결에는 같은 route의 구독만 넣을 수 있습니다. 서로 다른 route를 섞으면 네트워크 연결 전에 검증 오류를 반환합니다. 연결당 최대 구독 수는 1,024개이며, `Subscribe`와 `Unsubscribe` 명령은 기본 250ms 간격으로 직렬화합니다. 연결이 끊어지면 동일한 송신 경로로 재연결한 뒤 현재 구독 목록을 정렬해 다시 전송합니다.

```go
aggregate, err := usdm.AggregateTradeStream("BTCUSDT")
if err != nil {
	return err
}
kline, err := usdm.KlineStream("BTCUSDT", usdm.Candle1Minute)
if err != nil {
	return err
}

streams, err := usdm.NewStreamClient(usdm.StreamClientConfig{
	Connector:             connector,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}
market, err := streams.MarketStream(
	usdm.MarketStreamRequest{Subscriptions: []usdm.StreamSubscription{aggregate, kline}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer market.Close()

err = market.Run(ctx, func(ctx context.Context, message usdm.MarketStreamMessage) error {
	if message.Response != nil {
		return nil
	}
	if message.EventType != "aggTrade" {
		return nil
	}
	var event usdm.StreamAggregateTrade
	if err := message.Decode(&event); err != nil {
		return err
	}
	return handleTrade(event)
})
```

typed 공개 이벤트는 aggregate trade, 마크가, 캔들, 24시간 ticker, 최우선 호가와 호가 갱신을 제공합니다. 추가 native 이벤트도 `Payload`와 `Raw`에서 손실 없이 decode할 수 있습니다.

## 로컬 오더북과 같은 송신 경로 복구

`LocalOrderBook`은 diff depth WebSocket을 먼저 열어 이벤트를 버퍼링하고, 같은 `EgressRouteID`로 `/fapi/v1/depth` snapshot을 조회합니다. REST와 WebSocket route가 다르거나 부분 호가 구독만 있으면 네트워크 작업 전에 거부합니다.

```go
depth, err := usdm.DiffDepthStream("BTCUSDT", 100*time.Millisecond)
if err != nil {
	return err
}
market, err := streams.MarketStream(
	usdm.MarketStreamRequest{Subscriptions: []usdm.StreamSubscription{depth}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer market.Close()

book, err := usdm.NewLocalOrderBook(usdm.LocalOrderBookConfig{
	RESTClient:    restClient,
	Symbol:        "BTCUSDT",
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

err = book.Run(ctx, market, func(ctx context.Context, view usdm.LocalOrderBookView) error {
	return handleBook(view)
})
```

공식 sequence 규칙에 따라 snapshot의 `lastUpdateId`보다 `u`가 작은 이벤트만 버리고, 첫 처리 이벤트가 `U <= lastUpdateId <= u`인지 확인합니다. 동기화 후에는 모든 새 이벤트의 `pu`가 직전 `u`와 같은지 검사합니다. `pu` 불일치나 WebSocket 연결 세대 변경이 감지되면 기존 장부를 폐기하고 같은 송신 경로에서 snapshot을 다시 받아 복구합니다. 수량은 절대값으로 반영하고 0이면 해당 가격 단계를 삭제합니다.

snapshot 기본 limit은 1,000이고 `ViewDepth` 기본값은 20입니다. `SynchronizationID`는 성공한 초기화 횟수, `GapCount`는 감지한 sequence gap 횟수이며 `Generation`은 WebSocket 재연결 세대를 나타냅니다. 동기화 중 이벤트가 `MaxBufferedEvents`를 넘으면 `ErrDepthBufferOverflow`로 안전하게 종료합니다.

## private User Data Stream

private 연결에는 REST 자격증명이 설정된 `Client`를 `StreamClientConfig.RESTClient`로 전달합니다. SDK는 연결할 때 `POST /fapi/v1/listenKey`로 키를 발급하고, 기본 50분마다 `PUT /fapi/v1/listenKey`로 수명을 연장합니다. 두 REST 요청과 WebSocket 연결은 모두 세션을 생성할 때 선택한 하나의 송신 경로를 사용합니다.

```go
streams, err := usdm.NewStreamClient(usdm.StreamClientConfig{
	Connector:             connector,
	RESTClient:            restClient,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}
userData, err := streams.UserDataStream(trade.WithEgressRoute("seoul-b"))
if err != nil {
	return err
}
defer userData.Close()

err = userData.Run(ctx, func(ctx context.Context, message usdm.UserDataStreamMessage) error {
	if message.EventType != "ORDER_TRADE_UPDATE" {
		return nil
	}
	var event usdm.StreamOrderTradeUpdate
	if err := message.Decode(&event); err != nil {
		return err
	}
	return handleOrder(event)
})
```

세션 생성 시 read 권한과 route 허용 목록을 Secret 조회 전에 검사합니다. listenKey 갱신 실패, 키 변경 또는 `listenKeyExpired` 이벤트가 발생하면 현재 연결만 닫고 같은 송신 경로에서 새 키를 받아 재연결합니다. typed private 이벤트는 계정·잔고·포지션, 주문·체결, 마진콜과 listenKey 만료를 포함합니다.

`UserDataStream.Close`는 로컬 WebSocket 세션을 종료합니다. 서버 listenKey를 즉시 무효화해야 하면 같은 route를 지정해 `CloseUserDataStream`도 호출합니다.

## WebSocket 연결 수명과 제한

- 연결은 최대 24시간이므로 정상 운영에서도 재연결을 예상합니다.
- 서버 protocol ping 응답과 client ping은 공통 `stream.Session`이 처리합니다.
- `Run` context가 세션 수명을 결정하며 `trade.WithTimeout`은 WebSocket 생성 옵션으로 허용하지 않습니다.
- 구독 제어 응답은 이벤트와 섞여 전달되므로 `MarketStreamMessage.Response`를 먼저 확인합니다.
- 로컬 오더북 자동 gap 복구와 별개로 장시간 soak test는 애플리케이션 운영 절차에 포함해야 합니다.

## 공식 기준 문서

- [Binance USDⓈ-M General Info](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/general-info)
- [Binance USDⓈ-M Trade REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/trade)
- [Binance USDⓈ-M Account REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/account)
- [Binance USDⓈ-M Market Data REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/market-data)
- [Binance USDⓈ-M WebSocket 연결](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/Connect)
- [Binance USDⓈ-M 로컬 오더북 관리](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/How-to-manage-a-local-order-book-correctly)
- [Binance USDⓈ-M WebSocket 분리 진입점 변경](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/Important-WebSocket-Change-Notice)
- [Binance USDⓈ-M User Data Stream](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/user-data-streams)
