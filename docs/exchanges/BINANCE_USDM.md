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
| 계정·포지션 | `Account`, `Positions` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `OrderHistory` |
| private 접속 키 | `StartUserDataStream`, `KeepaliveUserDataStream`, `CloseUserDataStream` |
| WebSocket | `MarketStream`, `UserDataStream`, 동적 구독·구독 해제 |

계정은 `/fapi/v3/account`, 포지션 위험은 `/fapi/v3/positionRisk`를 사용합니다. 주문 정정, 일괄 주문, 레버리지·마진 모드 변경과 수입 이력은 아직 포함하지 않습니다.

## 인증과 시간

SIGNED 요청은 URL 인코딩한 최종 파라미터에 HMAC SHA-256을 적용합니다. 실행 순서는 다음과 같습니다.

1. 자격증명의 route와 권한을 검사합니다.
2. IP weight와 계정 주문 count 제한을 확보합니다.
3. Secret을 조회합니다.
4. 보정된 현재 시간과 `recvWindow`를 넣습니다.
5. 최종 쿼리에 서명하고 선택한 EIP route로 전송합니다.

기본 `recvWindow`는 5초이며 최대 1분까지 설정할 수 있습니다. `ServerTime`은 요청 왕복 중간 시점을 기준으로 clock offset을 보정합니다.

## 요청 제한

초기 안전값은 현재 공식 기본 규칙을 사용합니다.

| 제한 | 초기값 | 범위 |
|---|---:|---|
| 요청 weight | 2400/분 | EIP route |
| 주문 count | 300/10초 | 계정 |
| 주문 count | 1200/분 | 계정 |

`ExchangeInfo` 응답의 `REQUEST_WEIGHT`와 `ORDERS` 규칙으로 값을 동적으로 갱신합니다. 응답의 `X-MBX-USED-WEIGHT-1M`, `X-MBX-ORDER-COUNT-10S`, `X-MBX-ORDER-COUNT-1M`도 로컬 limiter에 반영합니다.

주문 생성은 IP weight를 소비하지 않지만 계정 주문 count를 소비합니다. 여러 EIP를 사용해도 계정 주문 제한은 늘어나지 않습니다.

## 포지션 모드와 주문 검증

- 단방향 모드는 `PositionSideBoth` 또는 빈 값을 사용합니다.
- 헤지 모드는 `PositionSideLong` 또는 `PositionSideShort`를 명시합니다.
- 헤지 모드 주문에는 `reduceOnly`를 함께 보내지 않습니다.
- `closePosition`은 `STOP_MARKET` 또는 `TAKE_PROFIT_MARKET`에서 사용하며 `quantity`, `reduceOnly`와 함께 보낼 수 없습니다.
- GTD 주문은 현재 시각보다 최소 600초 뒤의 `GoodTillDate`가 필요합니다.
- 가격과 수량은 decimal 문자열로 입력하며 SDK가 자동 반올림하지 않습니다.

지원 주문 종류는 `LIMIT`, `MARKET`, `STOP`, `STOP_MARKET`, `TAKE_PROFIT`, `TAKE_PROFIT_MARKET`, `TRAILING_STOP_MARKET`입니다.

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

한 연결에는 같은 route의 구독만 넣을 수 있습니다. 서로 다른 route를 섞으면 네트워크 연결 전에 검증 오류를 반환합니다. 연결당 최대 구독 수는 1,024개이며, `Subscribe`와 `Unsubscribe` 명령은 기본 250ms 간격으로 직렬화합니다. 연결이 끊어지면 동일한 EIP route로 재연결한 뒤 현재 구독 목록을 정렬해 다시 전송합니다.

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

typed 공개 이벤트는 aggregate trade, 마크가, 캔들, 24시간 ticker, 최우선 호가와 호가 갱신을 제공합니다. 추가 native 이벤트도 `Payload`와 `Raw`에서 손실 없이 decode할 수 있습니다. 로컬 오더북은 `StreamDepth`의 update ID 연결성을 검사하고 gap 발생 시 REST snapshot으로 다시 구성해야 합니다.

## private User Data Stream

private 연결에는 REST 자격증명이 설정된 `Client`를 `StreamClientConfig.RESTClient`로 전달합니다. SDK는 연결할 때 `POST /fapi/v1/listenKey`로 키를 발급하고, 기본 50분마다 `PUT /fapi/v1/listenKey`로 수명을 연장합니다. 두 REST 요청과 WebSocket 연결은 모두 세션을 생성할 때 선택한 하나의 EIP route를 사용합니다.

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

세션 생성 시 read 권한과 route 허용 목록을 Secret 조회 전에 검사합니다. listenKey 갱신 실패, 키 변경 또는 `listenKeyExpired` 이벤트가 발생하면 현재 연결만 닫고 같은 EIP에서 새 키를 받아 재연결합니다. typed private 이벤트는 계정·잔고·포지션, 주문·체결, 마진콜과 listenKey 만료를 포함합니다.

`UserDataStream.Close`는 로컬 WebSocket 세션을 종료합니다. 서버 listenKey를 즉시 무효화해야 하면 같은 route를 지정해 `CloseUserDataStream`도 호출합니다.

## WebSocket 연결 수명과 제한

- 연결은 최대 24시간이므로 정상 운영에서도 재연결을 예상합니다.
- 서버 protocol ping 응답과 client ping은 공통 `stream.Session`이 처리합니다.
- `Run` context가 세션 수명을 결정하며 `trade.WithTimeout`은 WebSocket 생성 옵션으로 허용하지 않습니다.
- 구독 제어 응답은 이벤트와 섞여 전달되므로 `MarketStreamMessage.Response`를 먼저 확인합니다.
- 실시간 호가의 sequence gap 복구와 장시간 soak test는 애플리케이션 운영 절차에 포함해야 합니다.

## 공식 기준 문서

- [Binance USDⓈ-M General Info](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/general-info)
- [Binance USDⓈ-M Trade REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/trade)
- [Binance USDⓈ-M Account REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/account)
- [Binance USDⓈ-M Market Data REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/market-data)
- [Binance USDⓈ-M WebSocket 연결](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/Connect)
- [Binance USDⓈ-M WebSocket 분리 진입점 변경](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/Important-WebSocket-Change-Notice)
- [Binance USDⓈ-M User Data Stream](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/user-data-streams)
