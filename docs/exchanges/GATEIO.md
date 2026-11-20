# Gate.io API v4 Spot REST·WebSocket·공통 어댑터

구현 기준은 Gate.io API v4 Spot REST·JSON WebSocket과 기본 주소 `https://api.gateio.ws/api/v4`, `wss://api.gateio.ws/ws/v4/`입니다. `NewUnifiedSpot`은 native REST 클라이언트를 프로젝트 공통 Spot 계약으로 변환하며, Futures는 `exchange/gateio/futures` 패키지로 분리합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Gate.io 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | API Secret 원문 |

`credential.Descriptor.AccountID`에는 Gate.io UID 요청 제한을 공유하는 계정의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 IP 허용 목록에는 허용할 route에 연결된 공인 송신 IP를 모두 등록해야 합니다.

```go
client, err := gateio.New(gateio.Config{
	Executor: executor,
	Credentials: &credential.Descriptor{
		AccountID: "gateio-main",
		Exchange:  model.ExchangeGateIO,
		SecretRef: "secret/gateio-main",
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

ticker, err := client.Ticker(ctx, "BTC_USDT", trade.WithEgressRoute("seoul-b"))
```

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 거래쌍 규칙 | `CurrencyPairs` | `GET /spot/currency_pairs` |
| 현재가 | `Ticker` | `GET /spot/tickers` |
| 호가 | `OrderBook` | `GET /spot/order_book` |
| 공개 체결 | `RecentTrades` | `GET /spot/trades` |
| 캔들 | `Candles` | `GET /spot/candlesticks` |
| 계정 | `Accounts` | `GET /spot/accounts` |
| 주문 생성 | `PlaceOrder` | `POST /spot/orders` |
| 주문 상세 | `OrderInfo` | `GET /spot/orders/{order_id}` |
| 주문 취소 | `CancelOrder` | `DELETE /spot/orders/{order_id}` |
| 전체 미체결 | `OpenOrders` | `GET /spot/open_orders` |
| 내 체결 | `MyTrades` | `GET /spot/my_trades` |

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 문자열로 보존합니다. 객체와 배열 항목의 `Raw`에는 해당 원본 JSON을 보존합니다. 캔들은 `timestamp, quote volume, close, high, low, open, base volume, closed` 순서로 해석하며, 구형 7개 필드 응답은 `BaseVolume`을 빈 문자열로 둡니다.

## 추가 REST API

아래 메서드는 모두 `*Client`에 있고 가격·수량·수수료를 문자열로 보존하며 항목마다 `Raw` 원본 JSON을 담습니다. 변경 API(주문·이체·출금·서브계정 관리)는 자동 재시도하지 않습니다. 전송 결과가 불명확하면 `UNKNOWN_EXECUTION_STATE`로 반환하므로 조회 API로 최종 상태를 확인해야 합니다. `credential.Descriptor`에 해당 권한이 없으면 요청 전에 거부됩니다.

### 주문 (`orders_ext.go`)

| 메서드 | API | 권한 |
|---|---|---|
| `FinishedOrders` | `GET /spot/orders?status=finished` (거래쌍·방향·기간·페이지) | Read |
| `PlaceBatchOrders` | `POST /spot/batch_orders` (최대 4거래쌍, 거래쌍당 10건) | Trade |
| `AmendOrder` | `PATCH /spot/orders/{order_id}` (수량 또는 가격) | Trade |
| `AmendBatchOrders` | `POST /spot/amend_batch_orders` (최대 5건) | Trade |
| `CancelAllOrders` | `DELETE /spot/orders` (거래쌍·방향 선택) | Trade |
| `CancelBatchOrders` | `POST /spot/cancel_batch_orders` (최대 20건) | Trade |
| `CountdownCancelAll` | `POST /spot/countdown_cancel_all` (5초 이상, 0은 해제) | Trade |
| `PriceOrders` | `GET /spot/price_orders` | Read |
| `PlacePriceOrder` | `POST /spot/price_orders` | Trade |
| `CancelAllPriceOrders` | `DELETE /spot/price_orders` | Trade |
| `PriceOrder` | `GET /spot/price_orders/{order_id}` | Read |
| `CancelPriceOrder` | `DELETE /spot/price_orders/{order_id}` | Trade |

일괄 주문·정정·취소는 HTTP 200이어도 항목별로 실패할 수 있으므로 `BatchOrder.Succeeded`, `CancelOrderResult.Succeeded`와 `Label`, `Message`를 확인해야 합니다. 시장가 가격 트리거 주문은 Gate.io 스키마상 `put.price`가 필수여서 `"0"`을 전송합니다.

### 계정·수수료·통화 (`account_ext.go`)

| 메서드 | API | 권한 |
|---|---|---|
| `Currencies` | `GET /spot/currencies` | 공개 |
| `CurrencyInfo` | `GET /spot/currencies/{currency}` | 공개 |
| `CurrencyChains` | `GET /wallet/currency_chains` | 공개 |
| `SpotFee` | `GET /spot/fee` | Read |
| `BatchSpotFee` | `GET /spot/batch_fee` (최대 50거래쌍) | Read |
| `AccountBook` | `GET /spot/account_book` (30일 이내 구간) | Read |
| `WalletTradeFee` | `GET /wallet/fee` | Read |
| `TotalBalance` | `GET /wallet/total_balance` | Read |
| `SmallBalances` | `GET /wallet/small_balance` | Read |
| `SmallBalanceHistory` | `GET /wallet/small_balance_history` | Read |
| `ConvertSmallBalance` | `POST /wallet/small_balance` | Transfer |

### 입출금 (`wallet_ext.go`)

| 메서드 | API | 권한 |
|---|---|---|
| `DepositAddress` | `GET /wallet/deposit_address` | Read |
| `Deposits` | `GET /wallet/deposits` | Read |
| `Withdrawals` | `GET /wallet/withdrawals` | Read |
| `WithdrawStatuses` | `GET /wallet/withdraw_status` | Read |
| `SavedAddresses` | `GET /wallet/saved_address` | Read |
| `UIDPushOrders` | `GET /wallet/push` | Read |
| `Withdraw` | `POST /withdrawals` | Withdraw |
| `CancelWithdrawal` | `DELETE /withdrawals/{withdrawal_id}` | Withdraw |
| `PushToUID` | `POST /withdrawals/push` | Withdraw |

`WithdrawRequest.WithdrawOrderID`는 필수이며 중복 출금 방지 키입니다. 응답을 받지 못했다면 같은 값으로 `Withdrawals`를 조회해 처리 여부를 확인해야 합니다.

### 계정 간·서브계정 이체 (`transfer_ext.go`)

| 메서드 | API | 권한 |
|---|---|---|
| `Transfer` | `POST /wallet/transfers` | Transfer |
| `TransferInfo` | `GET /wallet/transfers?tx_id=` | Read |
| `TransferWithSubAccount` | `POST /wallet/sub_account_transfers` | Transfer |
| `SubAccountToSubAccount` | `POST /wallet/sub_account_to_sub_account` | Transfer |
| `SubAccountTransfers` | `GET /wallet/sub_account_transfers` | Read |
| `TransferOrderStatus` | `GET /wallet/order_status` | Read |

### 서브계정 (`subaccount_ext.go`)

| 메서드 | API | 권한 |
|---|---|---|
| `SubAccounts` | `GET /sub_accounts` | Read |
| `SubAccountInfo` | `GET /sub_accounts/{user_id}` | Read |
| `SubAccountKeys` | `GET /sub_accounts/{user_id}/keys` | Read |
| `SubAccountKeyInfo` | `GET /sub_accounts/{user_id}/keys/{key}` | Read |
| `SubAccountModes` | `GET /sub_accounts/unified_mode` | Read |
| `SubAccountBalances` | `GET /wallet/sub_account_balances` | Read |
| `SubAccountMarginBalances` | `GET /wallet/sub_account_margin_balances` | Read |
| `SubAccountFuturesBalances` | `GET /wallet/sub_account_futures_balances` | Read |
| `SubAccountCrossMarginBalances` | `GET /wallet/sub_account_cross_margin_balances` | Read |
| `CreateSubAccount` | `POST /sub_accounts` | Transfer |
| `CreateSubAccountKey` | `POST /sub_accounts/{user_id}/keys` | Transfer |
| `UpdateSubAccountKey` | `PUT /sub_accounts/{user_id}/keys/{key}` | Transfer |
| `DeleteSubAccountKey` | `DELETE /sub_accounts/{user_id}/keys/{key}` | Transfer |
| `LockSubAccount` / `UnlockSubAccount` | `POST /sub_accounts/{user_id}/lock`, `/unlock` | Transfer |

`CreateSubAccountKey` 응답의 `Secret`은 생성 시점에만 확인할 수 있으므로 호출자가 즉시 보관해야 합니다. 마진·선물·교차마진 서브계정 잔고는 계정 종류별로 `available` 구조가 달라 `Available`에 원본 JSON을 그대로 둡니다.

## 과거 데이터 덤프

REST 캔들은 Spot·Futures 모두 최근 10,000개보다 오래된 구간을 `Candlestick too long ago`로 거부하고, Spot 공개 체결은 30일보다 오래된 구간을 거부합니다. 그 이전 데이터는 Gate.io 공식 저장소 `https://download.gatedata.org`의 월별 gzip CSV를 `exchange/gateio/archive` 패키지로 읽습니다.

| 메서드 | 덤프 | 비고 |
|---|---|---|
| `Candles` | `{market}/candlesticks_{1m,5m,1h,4h,1d}` | 시간 오름차순, 열 순서는 REST와 같은 `t,v,c,h,l,o` |
| `Trades` | Spot `spot/deals`, Futures `{market}/trades` | 파일 기록 순서 그대로 반환 |
| `TradeCandles` | 체결 덤프 | 한 달 전체를 지정 구간 캔들로 합성해 오름차순 반환 |

`Market`은 `MarketSpot`, `MarketFuturesUSDT`, `MarketFuturesBTC`이고 `Symbol`은 `BTC_USDT` 형식입니다. 파일은 호출자가 `Close`해야 하는 스트림으로 읽으며 수백 MB 파일도 메모리에 올리지 않습니다. 요청은 다른 클라이언트와 같은 `Executor`와 요청별 송신 경로를 사용하고, `trade.WithTimeout`은 응답 헤더 수신까지만 적용합니다. 본문 수신 시간은 context로 제어합니다. 저장소는 요청 제한을 공개하지 않으므로 동시 다운로드 수는 호출자가 조절해야 합니다.

없는 파일은 `archive.ErrFileNotFound`와 `trade.ErrUnsupportedCapability`로 반환합니다. 2026-11 기준 BTC_USDT 실측 게시 범위는 다음과 같습니다.

- Spot 체결: 2018-01부터 지난달까지
- Spot 1·5분 캔들: 2017년부터 2024-05까지, 2024-04 누락
- Spot 1시간·4시간·1일 캔들: 2017년부터 지난달까지
- USDT Futures 캔들·체결: 2020-01부터 지난달까지

덤프는 월 단위로 게시되므로 당월 중 REST 캔들 10,000개 창보다 오래된 구간(1분봉 기준 약 7일 이전)은 다음 달 덤프가 나오기 전까지 어느 경로로도 받을 수 없습니다. 이 구간이 필요하면 WebSocket 또는 REST로 상시 수집해야 합니다.

Spot 캔들 덤프의 거래량은 저장소가 부동소수점으로 기록해 `6.137529907399998`처럼 원문 자릿수를 그대로 보존합니다. Futures 캔들과 체결의 수량은 계약 수이고, 체결의 음수 수량은 매도 방향이므로 `Amount`에는 절댓값을, `Side`에는 방향을 넣습니다. Spot 체결의 방향 값 `1`은 매수, `2`는 매도입니다.

```go
archiveClient, err := archive.New(archive.Config{Executor: executor, DefaultEgressRouteID: "seoul-a"})
if err != nil {
	return err
}
candles, err := archiveClient.TradeCandles(ctx, archive.TradesRequest{
	Market: archive.MarketSpot, Symbol: "BTC_USDT", Year: 2025, Month: time.March,
}, time.Minute)
```

`TradeCandles`의 구간은 월 경계와 일치하도록 하루를 나누어떨어져야 합니다. 저장소의 오래된 체결 파일은 최신 체결부터, 최근 파일은 오래된 체결부터 기록돼 있어 첫 두 체결 ID로 방향을 판별합니다. microsecond 시각을 쓰는 최근 파일에서는 합성 결과가 저장소 캔들과 일치합니다. 2018년 무렵의 초 단위 파일은 같은 초의 체결을 ID 순서로 시가·종가에 반영하므로 저장소 캔들과 시가·종가가 다를 수 있습니다. 캔들 덤프가 있는 월은 `Candles`를 우선 사용하고 체결 합성은 캔들 덤프가 없는 월에 사용하세요.

## 공통 Spot API

`NewUnifiedSpot`으로 생성한 어댑터는 `unified.SpotClient`의 마켓, 현재가, 호가, 최근 체결, 캔들, 잔고, 주문 생성·조회·취소·미체결 목록을 구현합니다. 공통 `BTC/USDT`는 Gate.io의 `BTC_USDT`로 변환하며 모든 메서드의 요청별 송신 경로 옵션을 native API까지 전달합니다.

시장가 매수의 `QuoteAmount`는 Gate.io `amount`의 견적 통화 금액으로, 시장가 매도의 `Quantity`는 기준 통화 수량으로 전달합니다. `ClientOrderID`를 생략하면 Gate.io 규칙에 맞는 `t-proven-` 접두사의 암호학적 난수 ID를 생성합니다. 지정가의 공통 PostOnly는 Gate.io POC로 변환하고, 시간 정책을 생략하면 GTC를 적용합니다. 주문 조회와 취소는 거래소 주문 ID뿐 아니라 `t-` 사용자 주문 ID도 지원합니다.

공통 캔들은 Gate.io가 직접 제공하는 1분·5분·15분·30분·1시간·4시간 구간을 지원하며, native 구간이 없는 3분 캔들은 1분 데이터 3개를 epoch 경계로 합성합니다. 공통 `Volume`에는 기준 통화 거래량을 사용하므로 구형 7개 필드 캔들처럼 `BaseVolume`이 없는 응답은 의미가 다른 quote volume으로 대체하지 않고 오류로 반환합니다. 전체 미체결 조회는 거래쌍별 `total`을 기준으로 모든 페이지를 끝까지 읽으며, 단일 마켓 요청은 결과를 해당 거래쌍으로 제한합니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 자격증명을 조회하고 최종 요청을 서명합니다.

1. 본문 bytes의 SHA-512 해시를 소문자 16진수로 계산합니다.
2. `대문자 HTTP method + 줄바꿈 + /api/v4 경로 + 줄바꿈 + query + 줄바꿈 + body hash + 줄바꿈 + Unix second`를 서명 원문으로 만듭니다.
3. API Secret으로 HMAC-SHA-512 서명하고 소문자 16진수로 변환합니다.
4. `KEY`, `Timestamp`, `SIGN` 헤더를 설정합니다.
5. POST는 서명한 것과 정확히 같은 JSON bytes를 전송하고 GET·DELETE는 본문 없이 실제 URL과 같은 query 순서를 서명합니다.

SDK가 지원하는 거래쌍, 통화, 주문 ID와 숫자 query는 URL 인코딩 전후가 달라지지 않는 문자만 허용합니다. 임의 문자를 받는 endpoint를 추가할 때는 Gate.io 규칙에 맞게 URL 인코딩 전 query 원문과 실제 URL의 파라미터 순서를 별도로 보존해야 합니다.

Provider가 반환한 API Key와 Secret byte slice는 요청 뒤 가능한 범위에서 덮어씁니다. Go 문자열과 HTTP 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## WebSocket

`StreamClient`는 public과 private이 같은 JSON endpoint를 사용하되, private 구독 명령마다 현재 Unix second와 API Key로 새 서명을 만듭니다.

| 구분 | `StreamChannel` | Gate.io 채널 |
|---|---|---|
| public 현재가 | `StreamChannelTicker` | `spot.tickers` |
| public 체결 | `StreamChannelTrades` | `spot.trades` |
| public 캔들 | `StreamChannelCandles` | `spot.candlesticks` |
| public 최우선 호가 | `StreamChannelBookTicker` | `spot.book_ticker` |
| public 증분 호가 | `StreamChannelOrderBookUpdate` | `spot.order_book_update` |
| public V2 호가 | `StreamChannelOrderBookV2` | `spot.obu` |
| private 주문 | `StreamChannelOrders` | `spot.orders` |
| private 내 체결 | `StreamChannelUserTrades` | `spot.usertrades` |
| private 잔고 | `StreamChannelBalances` | `spot.balances` |

```go
streamClient, err := gateio.NewStreamClient(gateio.StreamClientConfig{
	Connector:            connector,
	Credentials:          descriptor,
	CredentialProvider:   provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	gateio.StreamRequest{Subscriptions: []gateio.StreamSubscription{
		{Channel: gateio.StreamChannelTicker, CurrencyPair: "BTC_USDT"},
		{
			Channel:        gateio.StreamChannelOrderBookUpdate,
			CurrencyPair:   "BTC_USDT",
			UpdateInterval: gateio.StreamUpdate100Millis,
		},
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

return public.Run(ctx, func(_ context.Context, message gateio.StreamMessage) error {
	if message.Channel != gateio.StreamChannelTicker || message.Event != "update" {
		return nil
	}
	var ticker gateio.StreamTicker
	return message.Decode(&ticker)
})
```

public 캔들은 REST와 달리 10초부터 7일까지의 공식 구간만 허용합니다. 증분 호가는 20ms 또는 100ms 통지 주기를 명시해야 합니다. private 주문·내 체결은 안전한 복구 범위를 분명히 하기 위해 거래쌍을 요구하며, 잔고 채널은 거래쌍을 받지 않습니다.

private 서명 원문은 `channel=<channel>&event=<subscribe 또는 unsubscribe>&time=<Unix second>`이고 HMAC-SHA-512 소문자 hex를 `auth.SIGN`에 넣습니다. API Key는 `auth.KEY`, 인증 방식은 `api_key`입니다. 구독 시각과 서버 시각 차이는 60초 이하여야 하므로 인스턴스의 시각 동기화가 필요합니다.

연결은 선택한 송신 경로에 수명주기 동안 고정됩니다. 재연결하면 같은 route로 새 handshake를 수행하고 현재 구독을 새 시각으로 다시 서명해 복구합니다. 실행 중 `Subscribe`와 `Unsubscribe`를 사용할 수 있으며, 서버가 오류 응답을 보낸 변경은 로컬 복구 목록에서 되돌립니다. 기본 heartbeat는 30초마다 WebSocket protocol ping을 보내고 10초 안에 pong을 기다립니다. Gate.io의 연결 제한은 IP당 300개이며 여러 프로세스·클라이언트의 합산 연결 수는 SDK 밖에서도 관리해야 합니다.

`StreamChannelOrderBookUpdate`는 기존 원본 증분 채널입니다. 수량은 증감량이 아니라 해당 가격의 절대 수량이며 0이면 가격 단계를 삭제해야 합니다. 이 원본 채널을 직접 조립하려면 연결 뒤 이벤트를 임시 저장하고 `OrderBook`의 `with_id=true` snapshot과 결합해야 합니다.

### Spot Order Book V2 로컬 오더북

새 로컬 오더북은 서버가 첫 전체 snapshot을 직접 보내는 `spot.obu`를 사용합니다. 50단계는 20ms, 400단계는 100ms 간격이며 `StreamOrderBookDepth50` 또는 `StreamOrderBookDepth400`으로 선택합니다.

```go
public, err := streamClient.PublicStream(
	gateio.StreamRequest{Subscriptions: []gateio.StreamSubscription{{
		Channel:        gateio.StreamChannelOrderBookV2,
		CurrencyPair:   "BTC_USDT",
		OrderBookDepth: gateio.StreamOrderBookDepth50,
	}}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

orderBook, err := gateio.NewLocalOrderBook(gateio.LocalOrderBookConfig{
	CurrencyPair:  "BTC_USDT",
	Depth:         gateio.StreamOrderBookDepth50,
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

return orderBook.Run(ctx, public, func(
	_ context.Context,
	view gateio.LocalOrderBookView,
) error {
	return consume(view)
})
```

`full=true` snapshot은 기존 장부 전체를 교체하며 서버가 같은 세션에서 snapshot을 여러 번 보내도 모두 새 동기화 지점으로 적용합니다. 증분 이벤트는 `U`가 현재 `UpdateID + 1`과 정확히 같을 때만 적용하고 장부 ID를 `u`로 전진시킵니다. 불연속 ID, 중복·겹침 이벤트, 새 연결에서 snapshot보다 먼저 도착한 증분을 발견하면 장부를 버리고 선택한 같은 송신 경로로 재연결해 새 snapshot부터 복구합니다.

`SynchronizationID`는 적용한 전체 snapshot 횟수, `GapCount`는 복구를 유발한 불연속 횟수, `Generation`은 WebSocket 연결 세대입니다. `ViewDepth` 기본값은 20이고 구독 깊이를 넘을 수 없으며 내부 장부도 선택한 50 또는 400단계로 제한합니다. public stream에 같은 거래쌍·깊이의 V2 구독이 없거나 송신 경로가 다르면 네트워크 연결 전에 거부합니다.

Gate.io는 2026년 3월부터 테스트넷에서 최초 snapshot을 구독 응답보다 먼저 보낼 수 있다고 공지했습니다. SDK는 구독 성공 응답을 기다리지 않고 유효한 snapshot을 즉시 적용하므로 두 메시지 순서에 의존하지 않습니다.

JSON endpoint만 지원하며 Gate.io SBE binary push는 현재 범위에 포함하지 않습니다. public 이벤트 유실과 private 재연결 구간은 REST 조회로 최종 상태를 재조정해야 합니다. 시스템의 `spot.system` upgrade 알림을 받으면 연결 종료를 기다리지 말고 운영 계층에서 새 세션으로 교체하는 것이 안전합니다.

## 요청 제한과 송신 경로

기본 로컬 quota는 현재 공식 기본 제한을 따릅니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `gateio:route:<route>:public:<endpoint>:10seconds` | 200회/10초 | 선택한 송신 경로와 공개 endpoint |
| `gateio:account:<account>:private:<endpoint>:10seconds` | 200회/10초 | UID와 private endpoint |
| `gateio:account:<account>:spot-order:<market>:1second` | 10회/초 | UID와 거래쌍의 주문 생성 |
| `gateio:account:<account>:spot-cancel:1second` | 200회/초 | UID의 주문 취소 |

`X-Gate-RateLimit-Limit`과 `X-Gate-RateLimit-Requests-Remain`이 로컬 설정과 일치하면 관측 사용량을 반영합니다. remaining이 0이고 `X-Gate-RateLimit-Reset-Timestamp`가 미래 시각이면 해당 bucket을 reset까지 막습니다. 계정 등급이나 운영 정책이 다르면 `Config`의 `PublicQuota`, `PrivateQuota`, `OrderQuota`, `CancelQuota`를 더 보수적인 값으로 재정의할 수 있습니다.

Public endpoint는 IP와 endpoint 기준이므로 요청별 송신 경로가 각각 독립된 bucket을 사용합니다. Private, 주문, 취소 제한은 UID 기준이므로 송신 경로를 바꿔도 quota가 늘어나지 않습니다. 다중 송신 IP는 공개 처리량 분산, API Key IP 허용 목록, 장애 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- 현재 native 어댑터는 순수 `spot` 계정의 지정가와 시장가만 허용합니다.
- `ClientOrderID`는 필수이며 `t-` 접두사 뒤에 최대 28자의 영문자, 숫자, 밑줄, 하이픈, 점만 허용합니다.
- 지정가는 가격과 수량을 요구하며 GTC·IOC·POC·FOK를 지원합니다.
- 시장가는 가격을 받지 않으며 IOC·FOK를 지원합니다.
- 시장가 매수의 `Amount`는 견적 통화 금액이고 시장가 매도의 `Amount`는 기준 통화 수량입니다.
- SDK는 자동 반올림하지 않습니다. `CurrencyPairs`의 정밀도와 최소·최대 주문 값을 적용한 문자열을 전달해야 합니다.
- 주문 상세와 취소는 주문 ID와 거래쌍을 함께 요구합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 5xx 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 불명확한 주문 생성 결과는 고유한 `ClientOrderID`로 상태를 확인한 뒤 처리해야 합니다.

## 오류 처리

Gate.io의 비정상 응답은 일반적으로 비-2xx 상태와 `label`, `message`, 선택적인 `detail` JSON을 사용합니다. SDK는 HTTP 상태와 label을 함께 검사해 인증·서명, 권한·IP 허용 목록, 잔고 부족, 주문 없음, 요청 제한, 거래소 장애를 공통 `trade.APIError`로 변환합니다. 원본 label과 message, 요청 ID를 보존하되 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## 운영 검증

자동 테스트는 REST 서명 원문·본문·query 일치, 요청별 route 선택, route 허용 목록 사전 검사, Secret 덮어쓰기, 요청 제한 분리, 오류 분류, mutation 불명확 상태를 검증합니다. WebSocket은 public 재연결·재구독, private 명령 서명, typed event decode, 동적 구독 실패 rollback과 V2 snapshot 선도착·update ID 공백·동일 송신 경로 복구를 검증합니다. 실제 Gate.io 계정과 지정 송신 경로를 이용한 읽기·주문·장시간 stream smoke는 아직 대기 상태입니다.

## 공식 기준

- [Gate.io API v4 개요·인증·요청 제한](https://www.gate.com/docs/developers/apiv4/en/)
- [Gate.io API v4 Spot](https://www.gate.com/docs/developers/apiv4/en/spot/)
- [Gate.io API v4 Spot WebSocket](https://www.gate.com/docs/developers/apiv4/ws/en/)
- [Gate.io API v4 반환 label](https://www.gate.com/docs/developers/apiv4/en/#label-list)
