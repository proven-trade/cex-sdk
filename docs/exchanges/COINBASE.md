# Coinbase Advanced Trade Spot 어댑터

## 범위

첫 번째 범위는 Coinbase Advanced Trade v3의 Spot REST와 WebSocket API다.

- 서버 시간
- 공개 Spot 상품 목록과 단건 상품
- 공개 호가, 최근 체결, 캔들
- 거래 계정 목록과 단건 계정
- 시장가 IOC, 지정가 GTC·SOR IOC·FOK 주문 생성
- 최대 100건 주문 일괄 취소
- 주문 단건과 cursor 기반 주문 목록
- cursor 기반 체결 목록
- 공개 ticker, ticker batch, 체결, 호가, 5분 캔들, 상품 상태 stream
- private user 주문 stream
- 요청별 송신 경로 선택

이후 호가 일괄 조회, 주문 정정·미리보기·포지션 청산, 스테이블코인 전환, 수수료 요약, API key 권한, 결제수단, 포트폴리오, 미국 선물(CFM) REST를 추가했다. endpoint와 요구 권한은 [확장 REST API](#확장-rest-api)에 정리했다.

stop·bracket 같은 추가 주문 설정과 문서에 deprecated로 표시된 Coinbase International(INTX) endpoint는 범위가 아니다. 지원하지 않는 주문 설정을 임의의 필드 조합으로 전송하지 않고 명시적으로 거부한다. private WebSocket의 `futures_balance_summary` 채널은 아직 지원하지 않는다.

## 자격증명

Coinbase App API는 CDP에서 ECDSA 서명 알고리즘으로 생성한 key를 사용해야 한다. Ed25519 key는 이 어댑터에서 지원하지 않는다.

`credential.Material`에는 다음처럼 저장한다.

| 필드 | 값 |
|---|---|
| `APIKey` | `organizations/{org_id}/apiKeys/{key_id}` 형식의 API key name |
| `SecretKey` | P-256 EC private key PEM |
| `Passphrase` | 사용하지 않음 |

PEM의 실제 줄바꿈과 `\n`으로 escape된 줄바꿈을 모두 읽을 수 있다. Secret은 요청을 최종 생성할 때 조회하며, 요청이 끝나면 Provider가 반환한 byte slice를 덮어쓴다. 자격증명에 허용되지 않은 송신 경로나 필요한 read/trade/transfer 권한은 Secret 조회 전에 거부한다.

## JWT 인증

private REST 요청마다 만료 시간이 2분인 ES256 JWT를 새로 만든다.

- header: `alg=ES256`, `typ=JWT`, `kid={API key name}`, 16-byte random `nonce`
- claims: `sub={API key name}`, `iss=cdp`, `nbf`, `exp`, `uri`
- `uri`: `{대문자 HTTP method} {host}{path}`
- query string과 body는 `uri`에 넣지 않음
- 서명 결과는 JOSE 형식의 64-byte `R || S`

`BaseURL`을 공식 운영 주소가 아닌 프록시로 바꾸면 JWT의 host도 해당 `BaseURL` host가 된다. Coinbase가 직접 검증할 JWT를 중계하는 프록시는 원래 host를 보존하거나 SDK의 URL 설정을 공식 host에 맞춰야 한다.

## 생성 예제

```go
descriptor := &credential.Descriptor{
	AccountID:   "coinbase-main",
	Exchange:    model.ExchangeCoinbase,
	SecretRef:   "secret/coinbase/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
}

client, err := coinbase.New(coinbase.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:  secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}
```

## 공개 시세

public endpoint에는 인증 정보를 넣지 않고 `Cache-Control: no-cache`를 설정한다. Coinbase 공개 REST의 기본 1초 cache를 사용하고 싶다면 현재 어댑터의 공개 메서드 대신 별도 cache 계층을 두는 방식을 권장한다. 실시간 시세는 후속 WebSocket 어댑터를 사용한다.

```go
book, err := client.OrderBook(
	ctx,
	coinbase.OrderBookRequest{
		ProductID: "BTC-USD",
		Limit:     50,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

최근 공개 체결의 `Limit`은 필수이며 1~1000이다. 캔들은 Unix 초 단위의 시작·종료 시각과 지원 granularity를 전달하고 최대 350개를 요청할 수 있다.

## 주문

첫 범위는 다음 네 설정을 타입으로 분리한다.

- `MarketMarketIOC`: `QuoteSize` 또는 `BaseSize` 중 정확히 하나
- `LimitLimitGTC`: `BaseSize`, `LimitPrice`, 선택적 `PostOnly`
- `SORLimitIOC`: `BaseSize`, `LimitPrice`
- `LimitLimitFOK`: `BaseSize`, `LimitPrice`

```go
reference, err := client.PlaceOrder(
	ctx,
	coinbase.PlaceOrderRequest{
		ClientOrderID: "strategy-20260824-1",
		ProductID:     "BTC-USD",
		Side:          coinbase.SideBuy,
		OrderConfiguration: coinbase.OrderConfiguration{
			LimitLimitGTC: &coinbase.LimitGTCConfiguration{
				BaseSize:   "0.001",
				LimitPrice: "60000",
				PostOnly:   true,
			},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
```

가격과 수량은 `float64`가 아닌 decimal 문자열이다. SDK는 자동 반올림하지 않는다. 네트워크 오류나 5xx로 주문 접수 여부가 불명확하면 mutation을 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. 같은 `ClientOrderID`로 주문 상태를 조정하기 전에는 새 주문을 전송하지 않아야 한다.

일괄 취소 HTTP 응답이 성공해도 각 `CancelResult.Success`를 확인해야 한다. 취소 접수 후 최종 상태는 `OrderInfo` 또는 후속 user WebSocket으로 확인한다.

## 확장 REST API

모든 메서드는 기존 메서드처럼 `...trade.RequestOption`을 받고 응답에 원문 `Raw`를 보존한다. 표의 endpoint는 `/api/v3/brokerage` 접두사를 생략했고, 권한은 SDK `credential.Descriptor`가 Secret 조회 전에 검사하는 권한이다.

| 분류 | 메서드 | Endpoint | 권한 |
|---|---|---|---|
| 시세 | `BestBidAsk` | `GET /best_bid_ask` | read |
| 주문 | `EditOrder` | `POST /orders/edit` | trade |
| | `PreviewEditOrder` | `POST /orders/edit_preview` | read |
| | `PreviewOrder` | `POST /orders/preview` | read |
| | `ClosePosition` | `POST /orders/close_position` | trade |
| 전환 | `ConvertQuote` | `POST /convert/quote` | transfer |
| | `CommitConvertTrade` | `POST /convert/trade/{trade_id}` | transfer |
| | `ConvertTrade` | `GET /convert/trade/{trade_id}` | read |
| 계정 정보 | `TransactionSummary` | `GET /transaction_summary` | read |
| | `KeyPermissions` | `GET /key_permissions` | read |
| | `PaymentMethods` | `GET /payment_methods` | read |
| | `PaymentMethod` | `GET /payment_methods/{payment_method_id}` | read |
| 포트폴리오 | `Portfolios` | `GET /portfolios` | read |
| | `PortfolioBreakdown` | `GET /portfolios/{portfolio_uuid}` | read |
| | `CreatePortfolio` | `POST /portfolios` | transfer |
| | `EditPortfolio` | `PUT /portfolios/{portfolio_uuid}` | transfer |
| | `DeletePortfolio` | `DELETE /portfolios/{portfolio_uuid}` | transfer |
| | `MovePortfolioFunds` | `POST /portfolios/move_funds` | transfer |
| 미국 선물(CFM) | `CFMBalanceSummary` | `GET /cfm/balance_summary` | read |
| | `CFMPositions` | `GET /cfm/positions` | read |
| | `CFMPosition` | `GET /cfm/positions/{product_id}` | read |
| | `CFMCurrentMarginWindow` | `GET /cfm/intraday/current_margin_window` | read |
| | `CFMIntradayMarginSetting` | `GET /cfm/intraday/margin_setting` | read |
| | `SetCFMIntradayMarginSetting` | `POST /cfm/intraday/margin_setting` | trade |
| | `CFMSweeps` | `GET /cfm/sweeps` | read |
| | `ScheduleCFMSweep` | `POST /cfm/sweeps/schedule` | transfer |
| | `CancelCFMSweep` | `DELETE /cfm/sweeps` | transfer |

### 권한과 재시도

- `BestBidAsk`는 공개 market endpoint와 달리 서명된 read 요청이다.
- `PreviewEditOrder`와 `PreviewOrder`는 POST지만 주문 상태를 바꾸지 않고 Coinbase도 `view` key 권한을 요구한다. 그래서 read 요청으로 처리하며, 5xx와 네트워크 오류에는 공통 실행기의 제한적 읽기 재시도가 적용될 수 있다.
- Coinbase는 전환 견적·확정과 포트폴리오 생성·수정·삭제에 `trade` key 권한을, 포트폴리오 자금 이동과 sweep 예약·취소에 `transfer` key 권한을 요구한다. SDK는 전환과 포트폴리오 변경을 자산 이동과 계정 구조 변경으로 분류해 모두 `PermissionTransfer`를 요구한다. 운영 key에는 SDK Descriptor 권한과 Coinbase key 권한을 모두 부여해야 한다.
- 모든 mutation은 자동 재시도하지 않는다. 전송 결과가 불명확하면 `UNKNOWN_EXECUTION_STATE`로 반환하므로 조회 메서드로 상태를 확인한 뒤에 다시 요청해야 한다.

### 주문 정정·미리보기·청산

```go
preview, err := client.PreviewOrder(ctx, coinbase.PreviewOrderRequest{
	ProductID: "BTC-USD",
	Side:      coinbase.SideBuy,
	OrderConfiguration: coinbase.OrderConfiguration{
		LimitLimitGTC: &coinbase.LimitGTCConfiguration{
			BaseSize:   "0.001",
			LimitPrice: "60000",
		},
	},
})
if err != nil {
	return err
}
if len(preview.Errs) > 0 {
	return fmt.Errorf("preview rejected: %v", preview.Errs)
}

_, err = client.EditOrder(ctx, coinbase.EditOrderRequest{
	OrderID: "order-id",
	Price:   "60100",
	Size:    "0.001",
})
```

- `EditOrder`는 정정 후 `Price`와 `Size`를 모두 보내야 한다. `StopPrice`, `AverageEntryPrice`, `CancelAttachedOrder`는 선택이다. Coinbase는 정정 거부를 HTTP 200의 `success=false`로 돌려주므로, 이 경우 `edit_failure_reason`(없으면 `preview_failure_reason`)을 `ExchangeCode`에 담은 `*trade.APIError`를 반환한다. 접수 여부가 불명확하면 `OrderInfo`로 주문 상태를 확인한다.
- `PreviewEditOrder`는 주문을 바꾸지 않고 `EditOrderPreview`를 반환한다. 정정이 거부될 사유는 HTTP 오류가 아니라 `Errors`에 담긴다.
- `PreviewOrder`는 `PlaceOrder`와 같은 `OrderConfiguration`을 받아 예상 합계와 수수료를 돌려준다. 접수하면 거부될 사유는 `OrderPreview.Errs`와 `Warning`에 담기며 HTTP 오류가 아니다. `Leverage`와 `MarginType`(`CROSS`, `ISOLATED`)은 선물 상품에만 쓴다.
- `ClosePosition`은 `ClientOrderID`와 `ProductID`로 해당 상품의 미결제 포지션을 청산하는 주문을 접수하고 `OrderReference`를 반환한다. `Size`를 생략하면 거래소가 상품의 포지션 전체를 청산하는 주문으로 처리한다. 공식 문서의 예시와 `Size` 설명이 선물 계약 기준이며, `success=false` 응답은 `*trade.APIError`로 변환한다.

### 스테이블코인 전환

```go
quote, err := client.ConvertQuote(ctx, coinbase.ConvertQuoteRequest{
	FromAccount: usdAccountUUID,
	ToAccount:   usdcAccountUUID,
	Amount:      "100",
})
if err != nil {
	return err
}
committed, err := client.CommitConvertTrade(ctx, coinbase.ConvertTradeRequest{
	TradeID:     quote.ID,
	FromAccount: usdAccountUUID,
	ToAccount:   usdcAccountUUID,
})
```

`ConvertQuote`는 견적만 만들고 자산을 옮기지 않는다. `CommitConvertTrade`가 `UNKNOWN_EXECUTION_STATE`를 반환하면 같은 `TradeID`와 계정으로 `ConvertTrade`를 조회해 상태를 확인한다. `ConvertTrade`의 `source`, `target`, `unit_price`, `user_warnings` 같은 중첩 필드는 `Raw`에서 읽는다.

### 계정 정보와 포트폴리오

- `TransactionSummary`는 `ProductType`, `ContractExpiryType`, `ProductVenue`로 범위를 좁힐 수 있고 모두 비우면 전체를 합산한다. 거래량과 수수료는 정밀도를 보존하는 `json.Number`다. `deribit_venue_data` 같은 INTX 파생상품 필드는 `Raw`에서 읽는다.
- `KeyPermissions`는 요청에 쓴 key의 `can_view`, `can_trade`, `can_transfer`와 연결된 포트폴리오를 반환한다.
- `PortfolioBreakdown`은 잔고 요약과 Spot 포지션을 타입으로 반환하고, 무기한·선물·주식 포지션은 `Raw`의 `breakdown` 객체에서 읽는다.
- `DeletePortfolio`와 `SetCFMIntradayMarginSetting`은 응답 본문을 쓰지 않으므로 오류만 반환한다.
- `MovePortfolioFunds`는 `Funds`가 양수 decimal 문자열이어야 하고 출발·도착 포트폴리오가 달라야 한다.

### 미국 선물(CFM)

- `CFMBalanceSummary`는 선물 매수 가능액, 증거금, 청산 완충액과 장중·야간 증거금 구간 측정값을 반환한다. 각 항목은 `Amount`이거나 decimal 문자열이다.
- `CFMCurrentMarginWindow`의 `MarginProfileType`을 비우면 거래소 기본 프로필을 쓴다.
- `SetCFMIntradayMarginSetting`은 `IntradayMarginSettingStandard` 또는 `IntradayMarginSettingIntraday`만 보낼 수 있고 `trade` 권한이 필요하다.
- `ScheduleCFMSweep`은 `USDAmount`를 비우면 이체 가능한 잔여 자금 전체를 sweep한다. `ScheduleCFMSweep`과 `CancelCFMSweep`은 HTTP 성공이어도 `CFMSweepResult.Success`가 `false`이면 적용되지 않은 것이므로 반드시 확인한다. 대기 중(`PENDING`) sweep만 취소할 수 있고 처리 중(`PROCESSING`)은 취소할 수 없다.

## WebSocket

Coinbase Advanced Trade는 market data와 user data 주소가 분리되어 있다.

| 용도 | 운영 주소 |
|---|---|
| 공개 market data | `wss://advanced-trade-ws.coinbase.com` |
| private user data | `wss://advanced-trade-ws-user.coinbase.com` |

public stream은 다음 채널을 지원한다.

- `ticker`, `ticker_batch`: `[]StreamTickerEvent`
- `market_trades`: `[]StreamMarketTradeEvent`
- `level2`: `[]StreamLevel2Event`
- `candles`: `[]StreamCandleEvent`
- `status`: `[]StreamStatusEvent`
- 자동 구독되는 `heartbeats`: `[]StreamHeartbeatEvent`

```go
streams, err := coinbase.NewStreamClient(coinbase.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streams.PublicStream(
	coinbase.PublicStreamRequest{
		Subscriptions: []coinbase.StreamSubscription{
			{
				Channel:    coinbase.StreamChannelTicker,
				ProductIDs: []string{"BTC-USD"},
			},
			{
				Channel:    coinbase.StreamChannelLevel2,
				ProductIDs: []string{"BTC-USD"},
			},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message coinbase.StreamMessage) error {
	if message.Channel != coinbase.StreamChannelTicker {
		return nil
	}
	var events []coinbase.StreamTickerEvent
	if err := message.DecodeEvents(&events); err != nil {
		return err
	}
	return handleTickers(events)
})
```

서버는 `level2` 구독 데이터를 `channel=l2_data`로 전달하므로 raw handler에서 `StreamChannelLevel2Data`를 기준으로 분기해야 한다. snapshot을 적용한 뒤 update의 `NewQuantity`를 해당 가격 레벨의 전체 수량으로 교체하고, 값이 `0`이면 레벨을 제거해야 한다.

`level2`를 로컬 장부로 소비할 때는 위 `public.Run` 대신 `LocalOrderBook.Run`이 해당 public stream을 소유하도록 한다.

```go
book, err := coinbase.NewLocalOrderBook(coinbase.LocalOrderBookConfig{
	ProductID:     "BTC-USD",
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view coinbase.LocalOrderBookView) error {
	return handleBook(view)
})
```

SDK는 상품별 `SequenceNumber`가 직전 값보다 정확히 1 증가하는지 검사한다. 더 작은 오래된 순서는 무시하고, 앞으로 건너뛴 순서나 새 연결에서 snapshot보다 먼저 온 update는 현재 장부를 폐기한 뒤 같은 송신 경로로 재연결해 새 snapshot을 받는다. snapshot 이벤트는 언제든 전체 장부를 교체하며, 한 메시지에 같은 상품 이벤트가 여러 개 있으면 배열 순서대로 모두 반영한 뒤 한 번만 handler를 호출한다.

`SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 sequence 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타낸다. `ViewDepth` 기본값은 20이며 handler에 복사하는 상위 호가 수만 제한하고 내부 장부는 전체 snapshot과 이후 모든 가격 레벨을 유지한다. 로컬 오더북과 public stream의 상품·송신 경로가 다르면 네트워크 연결 전에 거부한다.

public `Subscribe`와 `Unsubscribe`가 성공하면 현재 구독 목록을 갱신한다. 연결이 끊기면 같은 송신 경로에서 heartbeat를 포함한 현재 목록을 채널별 메시지로 다시 구독한다. Coinbase는 연결 후 5초 안에 구독 메시지를 요구하며 채널 하나마다 별도 메시지를 보내야 한다.

user stream은 Spot 주문 snapshot과 update를 `[]StreamUserEvent`로 decode한다. 상품 목록이 비어 있으면 계정 전체 상품이며, 공식 계약상 user 연결의 상품 필터를 바꾸려면 기존 연결을 닫고 새 `UserStream`을 만들어야 한다.

```go
user, err := streams.UserStream(
	coinbase.UserStreamRequest{ProductIDs: []string{"BTC-USD"}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer user.Close()
```

user 연결은 `user`와 `heartbeats`를 모두 인증 구독한다. 연결할 때마다 최신 Secret을 조회하며, 각 subscribe 메시지마다 `uri` claim이 없는 새 2분 ES256 JWT를 만든다. 명시적인 JWT 인증 오류를 받으면 같은 key로 무한 재연결하지 않는다.

공개 구독 메시지는 IP당 초당 8건 공식 제한보다 낮도록 기본 150ms 간격으로 직렬화한다. heartbeat는 매초 수신되며 Coinbase가 60~90초 동안 업데이트가 없는 채널을 닫지 않도록 모든 세션에서 자동 구독한다. WebSocket protocol ping도 기본 20초 간격으로 수행한다.

한 WebSocket의 송신 경로는 생성 후 바뀌지 않는다. 최초 연결과 모든 재연결은 생성 시 선택한 route의 local source IP 전용 HTTP client로 handshake한다.

## 요청 제한

공식 한도와 계정별 상향 한도는 변경될 수 있으므로 SDK 기본값은 공개 route와 private 계정 모두 초당 10건으로 보수적으로 설정한다. `PublicRequestsPerSecond`와 `PrivateRequestsPerSecond`로 운영 계정의 공식 한도에 맞춰 조정할 수 있다. public 설정값은 한 송신 경로의 전체 REST 상한으로도 적용되어 public/private 요청을 합산한다.

HTTP 429와 `Retry-After`는 공통 limiter에 반영된다. 여러 송신 경로를 쓰더라도 거래소 제한이나 이용 정책을 우회하는 용도로 사용하면 안 된다.

## 송신 경로 선택

모든 메서드는 `trade.RequestOption`을 받는다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 선택한 local source IP 전용 연결 풀로 보낸다. HTTP keep-alive 풀은 route별로 분리되어 다른 송신 경로의 기존 연결을 재사용하지 않는다.

## 공식 기준 문서

- [Advanced Trade REST endpoint 목록](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/rest-api)
- [Coinbase App API key 인증](https://docs.cdp.coinbase.com/coinbase-app/authentication-authorization/api-key-authentication)
- [공개 상품 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/list-public-products)
- [공개 호가](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-product-book)
- [공개 체결](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-market-trades)
- [공개 캔들](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-product-candles)
- [주문 생성](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/create-order)
- [주문 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/list-orders)
- [체결 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/list-fills)
- [호가 일괄 조회](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/products/get-best-bid-ask)
- [주문 정정](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/edit-order)
- [주문 정정 미리보기](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/edit-order-preview)
- [주문 미리보기](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/preview-orders)
- [포지션 청산](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/close-position)
- [전환 견적](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/convert/create-convert-quote)
- [전환 확정](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/convert/commit-convert-trade)
- [전환 조회](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/convert/get-convert-trade)
- [수수료 요약](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/fees/get-transaction-summary)
- [API key 권한](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/data-api/get-api-key-permissions)
- [결제수단 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/payment-methods/list-payment-methods)
- [결제수단 단건](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/payment-methods/get-payment-method)
- [포트폴리오 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/list-portfolios)
- [포트폴리오 생성](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/create-portfolio)
- [포트폴리오 수정](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/edit-portfolio)
- [포트폴리오 삭제](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/delete-portfolio)
- [포트폴리오 구성](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/get-portfolio-breakdown)
- [포트폴리오 자금 이동](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/portfolios/move-portfolios-funds)
- [CFM 잔고 요약](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/get-futures-balance-summary)
- [CFM 포지션 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/list-futures-positions)
- [CFM 포지션 단건](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/get-futures-position)
- [CFM 현재 증거금 구간](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/get-current-margin-window)
- [CFM 장중 증거금 설정 조회](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/get-intraday-margin-setting)
- [CFM 장중 증거금 설정 변경](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/set-intraday-margin-settings)
- [CFM sweep 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/list-futures-sweeps)
- [CFM sweep 예약](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/schedule-futures-sweep)
- [CFM sweep 취소](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/futures/cancel-pending-futures-sweep)
- [Advanced Trade WebSocket 개요](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-overview)
- [Advanced Trade WebSocket 채널](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-channels)
- [Advanced Trade WebSocket 인증](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-authentication)
- [Advanced Trade WebSocket 요청 제한](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-rate-limits)
