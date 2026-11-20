# Bitget v3 UTA 어댑터

## 전제조건

이 어댑터는 Bitget Unified Trading Account의 v3 REST API를 기준으로 합니다. Classic v2 계정용 endpoint는 포함하지 않습니다.

private API를 사용하려면 다음 세 값이 `credential.Provider`의 `credential.Material`에 있어야 합니다.

| 필드 | Bitget 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | HMAC Secret Key |
| `Passphrase` | API Key 생성 시 설정한 Passphrase |

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 상품 범위

| Category | 지원 |
|---|---|
| `SPOT` | 시세, 자산, 주문 |
| `USDT-FUTURES` | 시세, 자산, 포지션, 주문 |
| `MARGIN` | 아직 미지원 |
| `COIN-FUTURES` | 아직 미지원 |
| `USDC-FUTURES` | 아직 미지원 |

## 과거 캔들

`Candles`(`GET /api/v3/market/candles`)는 최근 90일 이내 구간만 반환합니다. 그보다 오래된 구간은 `HistoryCandles`(`GET /api/v3/market/history-candles`)로 조회합니다. 두 메서드는 같은 `CandlesRequest`를 사용하지만 `HistoryCandles`는 요청당 최대 100개이고 `StartTime`과 `EndTime`을 함께 지정하면 범위가 90일을 넘을 수 없습니다. 이 제약은 요청 전에 검증합니다.

응답의 가장 오래된 봉 시각을 다음 요청의 `EndTime`으로 넘기면 상장 시점까지 거슬러 올라갈 수 있습니다. 페이지 경계의 봉이 양쪽 응답에 모두 포함될 수 있으므로 수집기는 봉 시각으로 중복을 제거해야 합니다. 과거 캔들 endpoint의 한도는 `5/IP/초`이며(응답 헤더 `x-mbx-used-remain-limit` 실측), SDK는 이 경로에 별도 `5/초` bucket을 사용합니다.

```go
end := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
candles, err := client.HistoryCandles(ctx, bitget.CandlesRequest{
	Category: bitget.CategorySpot, Symbol: "BTCUSDT",
	Interval: bitget.Candle1Minute, EndTime: &end, Limit: 100,
})
```

## 요청 제한

요청 한 건은 다음 두 제한을 원자적으로 차감합니다.

1. route 단위 전체 제한 `6000/IP/분`
2. endpoint별 문서 제한
   - 공개 시세: `20/IP/초`
   - 과거 캔들(`history-candles`): `5/IP/초`
   - 조회 private API: `20/UID/초`
   - 주문 생성·취소: `10/UID/초`

각 송신 경로는 독립된 IP 제한 bucket을 사용합니다. UID 제한은 route를 변경해도 같은 계정 bucket을 사용하므로 송신 경로 변경을 UID 제한 우회 수단으로 사용하지 않습니다.

## 서명과 안전한 주문 실패

서명 문자열은 다음 순서로 구성합니다.

```text
timestamp + uppercase(method) + requestPath + optional("?" + sortedQuery) + exactBody
```

최종 문자열에 HMAC SHA-256을 적용하고 결과를 Base64로 인코딩합니다. 서명은 요청 제한 대기가 끝난 뒤 생성되므로 대기 시간 때문에 timestamp가 불필요하게 만료되지 않습니다.

주문 생성·취소의 전송 오류와 Bitget의 `40010`, `40725`, `45001` 응답은 실행 여부가 불명확할 수 있습니다. SDK는 이를 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. `clientOid`를 사용해 `OrderInfo` 또는 private order stream으로 최종 상태를 확인해야 합니다.

## Demo Trading

Demo API Key를 사용할 때 클라이언트 설정에 `DemoTrading: true`를 지정합니다. 모든 REST 요청에 `paptrading: 1` 헤더가 추가됩니다.

## 추가 REST API

아래 메서드는 공식 Bitget v3 SDK(`BitgetLimited/v3-bitget-api-sdk`)와 Postman 컬렉션으로 경로를 확인했습니다. 공식 문서 페이지가 JS 렌더링이라 응답 필드와 일부 열거값(`holdMode`, `deduct`, `operation`, `accountType`, `role` 등)은 SDK 계약으로 고정하지 않았습니다. 응답은 `json.RawMessage`(`data` 필드)로 반환하고, 해당 열거값은 호출자가 Bitget 문서의 값을 문자열로 전달합니다. 모든 mutation은 자동 재시도하지 않으며, 전송 결과가 불명확하면 `UNKNOWN_EXECUTION_STATE`로 반환합니다. 권한이 없는 자격증명은 요청 전에 거부됩니다. 이 메서드들의 요청 제한은 공식 수치가 확인되지 않아 공개 시세 `20/IP/초`, private `10/UID/초`로 보수적으로 차감합니다.

### 시세 (인증 없음)

| 메서드 | Endpoint |
|---|---|
| `ServerTime` | `GET /api/v3/market/time` |
| `RPIOrderBook` | `GET /api/v3/market/rpi-orderbook` |
| `OpenInterest` | `GET /api/v3/market/open-interest` |
| `CurrentFundingRate` | `GET /api/v3/market/current-fund-rate` |
| `FundingRateHistory` | `GET /api/v3/market/history-fund-rate` |
| `PositionTier` | `GET /api/v3/market/position-tier` |
| `OpenInterestLimit` | `GET /api/v3/market/oi-limit` |
| `RiskReserve`, `RiskReserveAll`, `RiskReserveHourly` | `GET /api/v3/market/risk-reserve`, `risk-reserve-all`, `risk-reserve-hour` |
| `DiscountRate` | `GET /api/v3/market/discount-rate` |
| `IndexComponents` | `GET /api/v3/market/index-components` |
| `ProofOfReserves`, `ProofOfReservesSummary` | `GET /api/v3/market/proof-of-reserves`, `/por` |

### 주문 (Trade 권한)

| 메서드 | Endpoint |
|---|---|
| `PlaceBatch` | `POST /api/v3/trade/place-batch` (최대 20건, TP/SL 불가) |
| `CancelBatch` | `POST /api/v3/trade/cancel-batch` (최대 20건, 건당 `orderId`/`clientOid` 중 하나) |
| `ModifyOrder` | `POST /api/v3/trade/modify-order` |
| `BatchModifyOrder` | `POST /api/v3/trade/batch-modify-order` (최대 20건, 주문 중복 불가) |
| `CancelSymbolOrders` | `POST /api/v3/trade/cancel-symbol-order` |
| `CountdownCancelAll` | `POST /api/v3/trade/countdown-cancel-all` (Bitget 담당자 신청 필요, 0이면 해제) |
| `ClosePositions` | `POST /api/v3/trade/close-positions` (시장가 청산, USDT-FUTURES) |
| `PlaceStrategyOrder`, `ModifyStrategyOrder`, `CancelStrategyOrder` | `POST /api/v3/trade/place-strategy-order`, `modify-strategy-order`, `cancel-strategy-order` |

`PlaceOrderRequest`는 `StrategyPrices`(`takeProfit`, `stopLoss`, `tpTriggerBy`, `slTriggerBy`, `tpOrderType`, `slOrderType`, `tpLimitPrice`, `slLimitPrice`)로 단건 주문에 TP/SL을 붙일 수 있습니다. 일괄 endpoint는 부분 성공을 허용하므로 반환된 `data`에서 건별 결과를 확인해야 합니다.

### 주문·체결 조회 (Read 권한)

| 메서드 | Endpoint |
|---|---|
| `Fills` | `GET /api/v3/trade/fills` |
| `UnfilledStrategyOrders` | `GET /api/v3/trade/unfilled-strategy-orders` |
| `HistoryStrategyOrders` | `GET /api/v3/trade/history-strategy-orders` |
| `HistoryPositions` | `GET /api/v3/position/history-position` |
| `ADLRank` | `GET /api/v3/position/adlRank` |

### 계정 (Read 권한)

| 메서드 | Endpoint |
|---|---|
| `AccountInfo`, `AccountSettings` | `GET /api/v3/account/info`, `/settings` |
| `FeeRate`, `AllFeeRates` | `GET /api/v3/account/fee-rate`, `/all-fee-rate` |
| `FinancialRecords` | `GET /api/v3/account/financial-records` |
| `MaxOpenAvailable` | `POST /api/v3/account/max-open-available` (본문을 쓰는 조회이므로 Read) |
| `AccountOpenInterestLimit`, `DeltaInfo`, `DeductInfo`, `SwitchStatus` | `GET /api/v3/account/open-interest-limit`, `/delta-info`, `/deduct-info`, `/switch-status` |
| `ConvertRecords`, `RepayableCoins`, `PaymentCoins` | `GET /api/v3/account/convert-records`, `/repayable-coins`, `/payment-coins` |

### 계정 설정 (Trade 권한)

| 메서드 | Endpoint |
|---|---|
| `SetLeverage` | `POST /api/v3/account/set-leverage` |
| `SetHoldMode` | `POST /api/v3/account/set-hold-mode` |
| `AdjustAccountMode` | `POST /api/v3/account/adjust-account-mode` |
| `SetMargin` | `POST /api/v3/account/set-margin` |
| `SwitchDeduct` | `POST /api/v3/account/switch-deduct` |
| `Repay` | `POST /api/v3/account/repay` |

### 자산·서브계정

| 메서드 | Endpoint | 권한 |
|---|---|---|
| `FundingAssets`, `TransferableCoins`, `MaxTransferable`, `MaxWithdrawal` | `GET /api/v3/account/funding-assets`, `/transferable-coins`, `/max-transferable`, `/max-withdrawal` | Read |
| `DepositAddress`, `SubDepositAddress`, `DepositRecords`, `SubDepositRecords` | `GET /api/v3/account/deposit-address`, `/sub-deposit-address`, `/deposit-records`, `/sub-deposit-records` | Read |
| `WithdrawalRecords`, `WithdrawAddresses` | `GET /api/v3/account/withdrawal-records`, `/withdraw-address` | Read |
| `SubUnifiedAssets`, `SubTransferRecords`, `SubAccounts`, `SubAPIKeys` | `GET /api/v3/account/sub-unified-assets`, `/sub-transfer-record`, `/api/v3/user/sub-list`, `/sub-api-list` | Read |
| `Transfer`, `SubTransfer`, `SubMasterTransfer` | `POST /api/v3/account/transfer`, `/sub-transfer`, `/sub-master-transfer` | Transfer |
| `SetDepositAccount`, `SwitchAccount` | `POST /api/v3/account/deposit-account`, `/switch` | Transfer |
| `CreateSubAccount`, `ModifySubAccount`, `FreezeSubAccount` | `POST /api/v3/user/create-sub`, `/modify-sub`, `/freeze-sub` | Transfer |
| `CreateSubAPIKey`, `UpdateSubAPIKey`, `DeleteSubAPIKey` | `POST /api/v3/user/create-sub-api`, `/update-sub-api`, `/delete-sub-api` | Transfer |
| `Withdraw`, `CancelWithdrawal` | `POST /api/v3/account/withdrawal`, `/cancel-withdrawal` | Withdraw |

`SubDepositRecords`는 공식 Go/Java SDK 기준 `GET`을 사용합니다(Postman 컬렉션은 `POST`로 표기). `Withdraw` 전송 결과가 불명확하면 `WithdrawalRecords`에 `ClientOID`를 지정해 실제 접수 여부를 확인해야 합니다.

### 아직 구현하지 않은 항목

`fee-group`, `score-weights`, `unfilled-orders-realtime`, `move-positions`, `move-position-history`, `agent-create`, `custom-collateral-coins`, `set-collateral-type`, `collateral-type`, `pre-set-leverage`는 응답/요청 형식을 확인하지 못해 구현하지 않았습니다. MARGIN, USDC-FUTURES, COIN-FUTURES, Copy trading, Earn, Loan, Broker, 그리고 WebSocket의 RPI depth·liquidation·fast fill·ADL·strategy order 채널과 WS 주문 API도 아직 없습니다.

## WebSocket

v3 UTA WebSocket은 다음 endpoint를 사용합니다.

| 환경 | public | private |
|---|---|---|
| 운영 | `wss://ws.bitget.com/v3/ws/public` | `wss://ws.bitget.com/v3/ws/private` |
| Demo | `wss://wspap.bitget.com/v3/ws/public` | `wss://wspap.bitget.com/v3/ws/private` |

`StreamClient`는 각 연결과 모든 재연결을 생성 시 선택한 송신 경로에 고정합니다.

```go
streams, err := bitget.NewStreamClient(bitget.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

ticker, err := bitget.PublicStreamArgument(bitget.CategorySpot, "ticker", "BTCUSDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	bitget.StreamRequest{Arguments: []bitget.StreamArgument{ticker}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message bitget.StreamMessage) error {
	if message.Event != "" || message.Pong {
		return nil
	}
	var tickers []bitget.StreamTicker
	if err := message.DecodeData(&tickers); err != nil {
		return err
	}
	return handleTickers(tickers)
})
```

public channel의 1차 typed 범위는 다음과 같습니다.

- `ticker`: `[]StreamTicker`
- `books`, `books1`, `books5`, `books50`: `[]StreamOrderBook`
- `publicTrade`: `[]StreamPublicTrade`
- `kline`: `[]StreamCandle` (REST `Candle`의 위치 배열과 달리 `start`·`open`·`close`·`high`·`low`·`volume`·`turnover` 객체)

`PublicStreamArgument`와 `KlineStreamArgument`가 Spot·USDT Futures 인자를 만듭니다. 동적 `Subscribe`와 `Unsubscribe`에 성공하면 현재 구독 목록을 갱신하고, 연결이 끊어졌을 때 같은 route에서 전체 목록을 다시 구독합니다.

Bitget은 4096바이트를 넘는 구독 요청을 `30002 Unrecognized request`로 거부합니다(kline 인자 약 50개). SDK는 구독·해제·재구독 요청을 입력 순서대로 4096바이트 이하 메시지로 나눕니다. 재연결 재구독은 연결당 초당 10개 메시지 제한에 맞춰 110ms 간격으로 보내고, 동적 `Subscribe`·`Unsubscribe`는 나눈 메시지마다 `SubscriptionInterval` 간격을 지킵니다. 일부만 보낸 뒤 실패하면 보낸 묶음까지만 재연결 복구 목록에 반영합니다. 대량 구독은 `PublicStream` 생성 시 인자로 넘기는 편이 빠릅니다.

구독 오류 프레임(`{"event":"error","code":30001,...}`)의 숫자 `code`는 문자열로 보존해 `StreamMessage{Event: "error"}`로 handler에 전달합니다. 연결은 유지되므로 존재하지 않는 상품 하나 때문에 다른 구독이 끊기지 않습니다.

### Spot 로컬 오더북과 자동 갭 복구

Bitget v3 UTA의 `books` 채널은 최초 full snapshot과 이후 incremental update를 같은 WebSocket에서 제공합니다. REST order book에는 WebSocket과 연결할 sequence가 없으므로 로컬 장부는 REST snapshot을 섞지 않고, 선택한 송신 경로의 `books` snapshot과 `pseq`·`seq`만 사용합니다.

```go
books, err := bitget.PublicStreamArgument(bitget.CategorySpot, "books", "BTCUSDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	bitget.StreamRequest{Arguments: []bitget.StreamArgument{books}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := bitget.NewLocalOrderBook(bitget.LocalOrderBookConfig{
	Symbol:        "BTCUSDT",
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view bitget.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- 로컬 오더북과 WebSocket의 `EgressRouteID`가 다르면 연결 전에 거부합니다.
- incremental update를 제공하는 Spot `books`만 허용하며, 매번 snapshot인 `books1`·`books5`·`books50`은 로컬 오더북 입력으로 받지 않습니다.
- snapshot은 즉시 게시하고, 첫 update에서 `snapshot seq ∈ [pseq, seq]`인지 확인합니다.
- 이후에는 `update.pseq == 이전 update.seq`이고 `seq`가 증가하는 데이터만 적용합니다.
- sequence 누락·역행 또는 동기화 완료 뒤 `pseq=0` reset을 발견하면 현재 장부를 폐기하고 같은 송신 경로로 재연결해 새 snapshot부터 복구합니다.
- 일반 네트워크 재연결도 이전 세대 장부를 폐기하지만 `GapCount`는 늘리지 않습니다.
- `SynchronizationID`, `Generation`, `GapCount`, `Sequence`, `MaxDepth`로 재동기화와 데이터 연속성을 관측할 수 있습니다.
- `Run` 중에는 대상 `books` 구독을 해제하지 않아야 합니다.

Bitget은 2026년 5월 19일부터 v3 depth의 `checksum`을 제거하고 `seq`·`pseq`를 정합성 기준으로 지정했습니다. `StreamOrderBook.Checksum`은 이전 payload decode 호환에만 남아 있으며 로컬 오더북 검증에는 사용하지 않습니다. v3의 고정 snapshot 채널도 기존 `books15`가 아니라 `books50`입니다.

private stream은 API Key, HMAC Secret, Passphrase를 사용합니다.

```go
account, err := bitget.PrivateStreamArgument("account")
if err != nil {
	return err
}
orders, err := bitget.PrivateStreamArgument("order")
if err != nil {
	return err
}
private, err := streams.PrivateStream(
	bitget.StreamRequest{Arguments: []bitget.StreamArgument{account, orders}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

연결마다 다음 순서로 인증합니다.

1. 자격증명 read 권한과 송신 경로 허용 목록 검사
2. Secret Provider에서 API Key, HMAC Secret, Passphrase 조회
3. `timestamp + "GET" + "/user/verify"`를 HMAC SHA-256과 Base64로 서명
4. login 요청 전송 후 `event=login`, `code=0` 응답 확인
5. account, position, order, fill channel 구독
6. 사용한 민감 byte slice 파기

로그인이 명시적으로 거절되면 같은 잘못된 자격증명으로 무한 재연결하지 않습니다. 네트워크 단절이면 같은 route에 새 연결을 만들고 최신 timestamp와 Secret으로 로그인부터 다시 수행합니다.

private typed data는 `[]StreamAccount`, `[]Position`, `[]Order`, `[]StreamFill`로 decode할 수 있습니다. 원본 `Data`도 보존하므로 이후 추가되는 필드는 별도 구조체로 받을 수 있습니다.

### Heartbeat와 연결 제한

Bitget은 WebSocket control ping이 아니라 application 문자열 heartbeat를 요구합니다. SDK는 기본 30초마다 `ping`을 보내고 10초 안에 `pong`이 관측되지 않으면 연결 실패로 처리해 같은 route로 재연결합니다.

- 연결 시도: IP당 5분에 300회
- 동시 연결: IP당 최대 100개
- 구독 요청: 연결당 시간당 240회
- 구독 channel: 연결당 최대 1,000개, 안정성을 위해 50개 미만 권장
- 수신 메시지: 연결당 초당 최대 10개이며 ping, login, subscribe, unsubscribe 포함

SDK의 동적 구독 명령은 기본 15초 간격으로 직렬화해 시간당 240회 제한을 지킵니다. 여러 프로세스와 세션을 합산하는 IP 연결 제한은 운영 메트릭에서 별도로 감시해야 합니다. `Run` context가 세션 수명을 결정하므로 stream 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [Bitget v3 UTA Quick Start](https://www.bitget.com/api-doc/uta/guide)
- [Bitget v3 UTA Ticker WebSocket](https://www.bitget.com/api-doc/uta/websocket/public/Tickers-Channel)
- [Bitget v3 UTA Depth WebSocket](https://www.bitget.com/api-doc/uta/websocket/public/Order-Book-Channel)
- [Bitget v3 UTA Change Log](https://www.bitget.com/api-doc/uta/changelog)
- [Bitget v3 UTA Account WebSocket](https://www.bitget.com/api-doc/uta/websocket/private/Account-Channel)
- [Bitget v3 UTA Place Order](https://www.bitget.com/api-doc/uta/trade/Place-Order)
- [Bitget v3 UTA Error Code](https://www.bitget.com/api-doc/uta/error-code/restapi)
