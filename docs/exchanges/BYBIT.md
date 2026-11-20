# Bybit V5 Spot·Linear 어댑터

## 범위

`exchange/bybit` 패키지는 Bybit V5의 다음 상품과 REST 기능을 제공합니다.

| Category | 공개 시세 | 계정 | 포지션 | 주문 |
|---|---:|---:|---:|---:|
| `spot` | 지원 | Unified 잔고 | 해당 없음 | 지원 |
| `linear` | 지원 | Unified 잔고 | 지원 | 지원 |

공개 시세는 상품 정보, ticker, 호가, 최근 체결, 캔들을 포함합니다. 주문은 생성, 정정, 단건 조회, 취소, 전체 취소, 일괄 생성·정정·취소, 미체결 목록, 이력을 포함하며 생성 요청은 조건부 주문·TP/SL·SMP 필드를 지원합니다. Linear 포지션은 레버리지, 포지션 모드, TP/SL·트레일링 스톱, 증거금, 청산 손익, 체결 내역, 포지션 이전을 지원합니다. 새 API 목록은 [Trade·Position 확장 API](#tradeposition-확장-api)를 참고합니다. 입출금·이체·코인 전환·서브 계정 API는 [Asset·User API](#assetuser-api)를 참고합니다. Market·Account 확장 API(가격 캔들, 펀딩비·미결제약정 이력, 수수료율, 거래 로그, 담보·차입·상환 등)는 [Market·Account 확장 API](#marketaccount-확장-api)를 참고합니다. Inverse, Option은 이 패키지의 지원 범위 밖입니다.

## 클라이언트 생성

운영 기본 endpoint는 `https://api.bybit.com`입니다. `Testnet: true`를 지정하면 별도 `BaseURL`이 없는 경우 `https://api-testnet.bybit.com`을 사용합니다.

```go
client, err := bybit.New(bybit.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
	ReceiveWindow:        5 * time.Second,
})
if err != nil {
	return err
}

order, err := client.PlaceOrder(
	ctx,
	bybit.PlaceOrderRequest{
		Category:     bybit.CategoryLinear,
		Symbol:       "BTCUSDT",
		Side:         bybit.SideBuy,
		OrderType:    bybit.OrderTypeLimit,
		Quantity:     "0.001",
		Price:        "60000",
		TimeInForce:  bybit.TimeInForceGTC,
		OrderLinkID:  "strategy-a-0001",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

자격증명에 `seoul-b`가 허용되어 있지 않으면 Secret Provider를 호출하거나 HTTP 요청을 보내기 전에 실패합니다.

## 인증과 시간 보정

HMAC API Key는 `credential.Material`의 다음 값을 사용합니다.

| 필드 | Bybit 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | HMAC Secret Key |

GET 요청은 최종 정렬 query string, POST 요청은 실제 전송할 JSON 바이트를 아래 원문 뒤에 붙여 HMAC SHA-256 소문자 16진수 서명을 만듭니다.

```text
timestamp + apiKey + receiveWindow + queryStringOrJSONBody
```

요청에는 `X-BAPI-API-KEY`, `X-BAPI-TIMESTAMP`, `X-BAPI-RECV-WINDOW`, `X-BAPI-SIGN` 헤더가 추가됩니다. 서명은 요청 제한 대기가 끝난 다음 생성합니다.

`ServerTime`은 요청 왕복 시간의 중간값과 응답의 서버 시간을 비교해 클라이언트 서명 시계 오프셋을 갱신합니다. 운영 환경에서도 NTP 또는 chrony 동기화가 전제입니다.

## 요청 제한

모든 요청은 route별 `600/IP/5초` bucket을 차감합니다. 추가로 공개 endpoint는 route별 보수적 endpoint bucket, private endpoint는 계정별 endpoint bucket을 사용합니다.

- 공개 조회: 기본 `20/route/초`, 서버 시간 `50/route/초`
- private 조회: `20/account/초`
- 주문 생성·정정·취소·전체 취소, 포지션 설정·이전, 포지션 이전 이력 조회: `10/account/초`
- 일괄 주문 생성·정정·취소: `10/account/초`이며 요청 수가 아니라 요청에 담은 주문 수만큼 차감합니다.
- 청산 손익·체결 내역·Spot 주문 가능 수량 조회: `20/account/초`
- 연결 끊김 보호(DCP) 설정: `5/account/초`

서버가 `X-Bapi-Limit`과 `X-Bapi-Limit-Status`를 반환하고 해당 endpoint의 설정 한도와 일치하면 로컬 사용량에 반영합니다. 계정 bucket은 송신 경로를 바꿔도 공유되므로 다중 송신 IP를 UID 제한 우회 수단으로 사용하지 않습니다. VIP 또는 Pro 등급에서 실제 한도가 다르면 해당 등급을 반영하는 설정 확장이 필요합니다.

Bybit은 한도 초과를 HTTP 429가 아니라 HTTP 200 본문의 `retCode: 10006`으로 알리기도 합니다. SDK는 이 응답을 받으면 해당 요청이 차감한 모든 bucket을 `X-Bapi-Limit-Reset-Timestamp`까지, 없거나 이미 지났으면 IP 창 길이인 5초 동안 막습니다. 읽기 요청은 block이 풀린 뒤 `ReadRetryPolicy` 안에서 다시 보내고, 끝내 실패하면 `trade.ErrRateLimited`를 반환합니다. 주문 생성·정정·취소와 모든 포지션 변경 요청은 자동 재시도하지 않습니다. 실행 관측 훅에는 이 시도가 HTTP 200이어도 `ErrorRateLimited`로 기록됩니다. 다른 거래소 어댑터도 `commonexchange.Execution.RateLimit`에 본문 판별 함수를 넣어 같은 처리를 쓸 수 있습니다.

## 안전한 주문 실패

주문 생성과 취소는 mutation으로 처리합니다. 전송 타임아웃, 연결 단절, 읽을 수 없는 응답, HTTP 5xx, Bybit `10000`·`10016` 응답처럼 실행 여부가 불명확한 결과는 `trade.ErrUnknownExecutionState`로 반환하며 자동 재시도하지 않습니다.

가능하면 고유한 `OrderLinkID`를 지정하고 `OrderInfo` 또는 향후 private order stream으로 최종 상태를 확인해야 합니다. 성공한 생성·취소 응답도 접수 결과이므로 최종 주문 상태를 뜻하지 않습니다.

## 요청별 송신 경로 선택

모든 메서드는 마지막 인자로 `trade.RequestOption`을 받습니다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 다른 local source IP 전용 연결 풀로 보냅니다.

```go
book, err := client.OrderBook(
	ctx,
	bybit.OrderBookRequest{
		Category: bybit.CategorySpot,
		Symbol:   "BTCUSDT",
		Limit:    50,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

Spot과 Linear의 일반 호가 조회 `Limit`은 현재 공식 범위에 맞춰 1부터 1,000까지 허용합니다. 0이면 Bybit category별 기본값을 사용합니다.

## WebSocket

public endpoint는 category별로 분리됩니다.

| 환경 | Spot public | Linear public | private |
|---|---|---|---|
| 운영 | `wss://stream.bybit.com/v5/public/spot` | `wss://stream.bybit.com/v5/public/linear` | `wss://stream.bybit.com/v5/private` |
| Testnet | `wss://stream-testnet.bybit.com/v5/public/spot` | `wss://stream-testnet.bybit.com/v5/public/linear` | `wss://stream-testnet.bybit.com/v5/private` |

public stream은 생성 시 category와 송신 경로를 고정합니다. 동적 구독·해제가 성공하면 현재 topic 목록을 갱신하고, 연결이 끊기면 같은 endpoint와 route에서 전체 목록을 다시 구독합니다.

```go
streams, err := bybit.NewStreamClient(bybit.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

ticker, err := bybit.TickerStreamTopic(bybit.CategorySpot, "BTCUSDT")
if err != nil {
	return err
}
book, err := bybit.OrderBookStreamTopic(bybit.CategorySpot, "BTCUSDT", 50)
if err != nil {
	return err
}
public, err := streams.PublicStream(
	bybit.PublicStreamRequest{
		Category: bybit.CategorySpot,
		Topics:   []string{ticker, book},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message bybit.StreamMessage) error {
	if message.Operation != "" || message.Pong {
		return nil
	}
	if message.Topic != ticker {
		return nil
	}
	var ticker bybit.StreamTicker
	if err := message.DecodeData(&ticker); err != nil {
		return err
	}
	return handleTicker(ticker)
})
```

public typed data는 다음 구조체로 decode할 수 있습니다.

- `tickers.{symbol}`: `StreamTicker`
- `orderbook.{depth}.{symbol}`: `StreamOrderBook`
- `publicTrade.{symbol}`: `[]StreamPublicTrade`
- `kline.{interval}.{symbol}`: `[]StreamKline`

호가는 현재 Bybit가 Spot·Linear에 제공하는 depth `1`, `50`, `200`, `1000`을 지원합니다. Spot은 구독 명령 한 건당 topic을 최대 10개까지 허용하므로 SDK가 더 큰 목록을 자동 분할합니다. 한 public 연결의 전체 topic 문자열은 공식 제한인 21,000자를 넘을 수 없습니다.

## Spot·Linear 로컬 오더북

`LocalOrderBook`은 Bybit가 구독 직후 보내는 WebSocket snapshot으로 장부를 만들고 이후 delta를 적용합니다. 새 snapshot이 오면 기존 가격 단계를 모두 버리고 교체하며, delta 수량이 0이면 삭제하고 그 외에는 삽입 또는 갱신합니다. depth 1은 공식 계약대로 snapshot만 처리합니다.

```go
topic, err := bybit.OrderBookStreamTopic(bybit.CategorySpot, "BTCUSDT", 50)
if err != nil {
	return err
}
public, err := streams.PublicStream(
	bybit.PublicStreamRequest{
		Category: bybit.CategorySpot,
		Topics:   []string{topic},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := bybit.NewLocalOrderBook(bybit.LocalOrderBookConfig{
	Category:      bybit.CategorySpot,
	Symbol:        "BTCUSDT",
	Depth:         50,
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view bybit.LocalOrderBookView) error {
	return handleBook(view)
})
```

SDK는 같은 topic의 `u`가 직전 update ID의 다음 값이고 `seq`가 증가하는지 검사합니다. 초기 snapshot 없이 delta가 오거나 update ID·cross sequence 이상이 감지되면 현재 장부를 폐기하고 WebSocket을 같은 송신 경로로 다시 연결해 새 snapshot을 받습니다. 서버가 `u=1`인 새 snapshot을 보내는 서비스 재시작도 전체 교체로 처리합니다.

`SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 sequence 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타냅니다. `ViewDepth`는 구독 `Depth`를 넘을 수 없으며 기본값은 `min(20, Depth)`입니다. 설정한 category·topic·송신 경로가 public stream과 일치하지 않으면 연결 전에 거부합니다.

private stream은 연결할 때마다 `GET/realtime + expires`를 HMAC SHA-256으로 서명해 인증한 뒤 topic을 구독합니다.

```go
private, err := streams.PrivateStream(
	bybit.PrivateStreamRequest{
		Topics: []string{"order.spot", "order.linear", "execution", "position.linear", "wallet"},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

private typed data는 `[]StreamOrder`, `[]StreamExecution`, `[]StreamPosition`, `[]StreamWallet`로 decode할 수 있습니다. `order`와 `order.linear`처럼 같은 종류의 all-in-one topic과 category topic은 Bybit 규칙에 따라 한 구독에서 함께 사용할 수 없습니다.

연결이 끊기면 같은 송신 경로에서 새 만료 시각과 Secret으로 재인증하고 현재 topic을 다시 구독합니다. 인증이 명시적으로 거절된 경우 잘못된 키로 무한 재연결하지 않습니다. heartbeat는 Bybit 권장 주기인 20초마다 `{"op":"ping"}`을 보내고 10초 안에 pong이 없으면 재연결합니다. stream 수명은 `Run`의 context로 제어하므로 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

WebSocket 연결은 도메인별 IP당 5분에 500회보다 자주 만들면 안 됩니다. 재연결 횟수와 장시간 연결 상태는 운영 메트릭에서 함께 감시해야 합니다.

## Trade·Position 확장 API

모든 메서드는 `func (client *Client) Name(ctx, request, options ...trade.RequestOption)` 형태이며 `trade.WithEgressRoute` 등 요청 옵션을 받습니다. 모든 요청은 공통 `600/IP/5초` bucket도 함께 차감하므로 아래 표의 rate bucket은 추가로 차감하는 endpoint bucket입니다. 변경(mutation) 요청은 `commonexchange.OperationMutation`으로 실행하며 자동 재시도하지 않습니다. 성공 응답은 접수 결과이므로 최종 상태는 조회 API 또는 private stream으로 확인해야 합니다.

### 주문 확장

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `AmendOrder` | `POST /v5/order/amend` | `credential.PermissionTrade` | `10/account/초` | 미체결·부분 체결 주문의 수량·가격·TP/SL·트리거 가격을 정정합니다. 수정하지 않을 필드는 비웁니다. `TakeProfit`·`StopLoss`를 `"0"`으로 보내면 기존 TP/SL을 취소합니다. |
| `CancelAllOrders` | `POST /v5/order/cancel-all` | `credential.PermissionTrade` | `10/account/초` | 조건에 맞는 미체결 주문을 모두 취소합니다. `Symbol`, `BaseCoin`, `SettleCoin`을 함께 지정하면 이 순서로 하나만 적용되며 Linear는 셋 중 하나가 필수입니다. Spot은 `SettleCoin`을 지원하지 않고, 모두 비우면 전체 Spot 일반 주문을 취소합니다. `OrderFilter`, `StopOrderType`으로 취소할 주문 종류를 좁힐 수 있고 결과는 `CancelAllOrdersResult`입니다. |
| `SetDisconnectCancelAll` | `POST /v5/order/disconnected-cancel-all` | `credential.PermissionTrade` | `5/account/초` | 연결 끊김 보호(DCP) 시간 창(`TimeWindow`, 3~300초)을 설정합니다. Bybit 문서상 옵션(Unified Account) 주문에만 적용되므로 Spot·Linear 주문 보호 수단으로 쓸 수 없습니다. 반영까지 시간이 걸립니다. |
| `SpotBorrowCheck` | `GET /v5/order/spot-borrow-check` | `credential.PermissionRead` | `20/account/초` | Spot 심볼의 현물·마진 거래 가능 최대 수량과 금액(`SpotBorrowQuota`)을 조회합니다. |

### 일괄 주문

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `PlaceBatchOrders` | `POST /v5/order/create-batch` | `credential.PermissionTrade` | `10/account/초`, 포함한 주문 수만큼 차감 | 최대 10건의 주문을 한 번에 생성합니다. |
| `AmendBatchOrders` | `POST /v5/order/amend-batch` | `credential.PermissionTrade` | `10/account/초`, 포함한 주문 수만큼 차감 | 최대 10건의 주문을 한 번에 정정합니다. |
| `CancelBatchOrders` | `POST /v5/order/cancel-batch` | `credential.PermissionTrade` | `10/account/초`, 포함한 주문 수만큼 차감 | 최대 10건의 주문을 한 번에 취소합니다. |

일괄 요청의 `Category`는 최상위에만 두고, 각 항목의 `Category`는 비워 두거나 일괄 요청과 같아야 합니다. 항목이 1~10건이 아니면 전송 전에 거부합니다. 결과 `BatchOrderResult.Items[i]`는 요청의 i번째 주문 결과이며 `retExtInfo.list`의 `Code`·`Message`를 포함합니다. 일부 주문만 실패해도 요청 전체는 성공으로 반환되므로 반드시 항목별 `Succeeded()`(`Code == 0`)를 확인해야 합니다. `result.list`와 `retExtInfo.list` 길이가 다르면 항목별 성공 여부를 알 수 없어 `trade.ErrUnknownExecutionState`로 반환합니다.

### 포지션 설정 (Linear)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `SetLeverage` | `POST /v5/position/set-leverage` | `credential.PermissionTrade` | `10/account/초` | 심볼 레버리지를 설정합니다. 단방향 모드에서는 `BuyLeverage`와 `SellLeverage`가 같아야 합니다. |
| `SwitchPositionMode` | `POST /v5/position/switch-mode` | `credential.PermissionTrade` | `10/account/초` | `Symbol` 또는 정산 `Coin` 단위로 단방향(`PositionModeOneWay`)·헤지(`PositionModeHedge`) 모드를 전환합니다. `Symbol`이 `Coin`보다 우선합니다. |
| `SetTradingStop` | `POST /v5/position/trading-stop` | `credential.PermissionTrade` | `10/account/초` | 포지션 TP/SL과 트레일링 스톱을 설정합니다. `"0"`은 해당 설정 취소이고, Partial 모드는 새 부분 TP/SL만 추가하며 `TPSize`와 `SLSize`가 같아야 합니다. |
| `SetAutoAddMargin` | `POST /v5/position/set-auto-add-margin` | `credential.PermissionTrade` | `10/account/초` | 격리 마진 포지션의 자동 증거금 추가를 켜거나 끕니다. 헤지 모드는 `PositionIndex`(1 또는 2)가 필수입니다. |
| `AddMargin` | `POST /v5/position/add-margin` | `credential.PermissionTrade` | `10/account/초` | 격리 마진 증거금을 수동 조정합니다. `Margin`은 양수면 추가, 음수면 감소이며 소수점 이하 4자리까지 허용하고 결과로 포지션 상태(`AddMarginResult`)를 반환합니다. |
| `ConfirmPendingMMR` | `POST /v5/position/confirm-pending-mmr` | `credential.PermissionTrade` | `10/account/초` | reduce-only로 표시된 포지션의 새 리스크 한도를 확인해 표시를 해제합니다. |
| `SymbolLeverages` | `GET /v5/position/symbol-info` | `credential.PermissionRead` | `10/account/초` | 포지션이 없어도 심볼별 격리 레버리지 설정을 조회합니다. |

레버리지·포지션 모드·TP/SL·증거금·MMR 확인은 `credential.PermissionTrade`이며, 설정 응답은 `MutationAck`(원문 `Raw`)로 반환합니다. 포지션 설정은 Linear category만 허용합니다.

### 포지션 조회와 이전

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `ClosedPnL` | `GET /v5/position/closed-pnl` | `credential.PermissionRead` | `20/account/초` | Linear 청산 손익을 cursor 기반으로 조회합니다(`ClosedPnLPage`). |
| `Executions` | `GET /v5/execution/list` | `credential.PermissionRead` | `20/account/초` | 내 체결 내역을 cursor 기반으로 조회합니다(`ExecutionPage`). 시간 범위를 생략하면 최근 7일이며, 지정하면 7일을 넘을 수 없습니다. |
| `MovePositions` | `POST /v5/position/move-positions` | `credential.PermissionTransfer` | `10/account/초` | 마스터-서브 또는 서브-서브 UID 사이에서 포지션을 이전합니다. 마스터 UID의 API Key로만 호출할 수 있고 한 요청에 최대 25개 `MovePositionLeg`를 담을 수 있습니다. |
| `MovePositionHistory` | `GET /v5/position/move-history` | `credential.PermissionRead` | `10/account/초` | 포지션 이전 이력을 cursor 기반으로 조회합니다(`MovePositionPage`). 시간 범위는 7일 이하이며 `Status`는 `Processing`, `Filled`, `Rejected` 중 하나입니다. |

`MovePositions`는 계정 간 자산 이동에 해당하므로 `PermissionTrade`가 아니라 `credential.PermissionTransfer`가 필요합니다. 응답의 `RejectParty`가 비어 있으면 초기 검증만 통과한 것이므로 최종 상태는 `MovePositionHistory`로 확인합니다. 조회 요청의 시간 범위는 `StartTime`·`EndTime`을 모두 지정했을 때 7일을 넘을 수 없고 `Limit`은 `ClosedPnL`·`Executions` 최대 100, `MovePositionHistory` 최대 200입니다.

### 주문 생성 신규 필드

`PlaceOrderRequest`에 다음 선택 필드가 추가되었고 모두 비워 두면 전송하지 않습니다. `PlaceOrderRequest`와 `CancelOrderRequest`의 `Category`는 일괄 요청 항목으로 쓸 때 비워 둘 수 있습니다. 필드는 로컬에서 먼저 검증하며 Spot·Linear에서 허용되지 않는 조합은 전송 전에 거부합니다.

| 필드 | Bybit 파라미터 | 적용 | 설명 |
|---|---|---|---|
| `IsLeverage` | `isLeverage` | Spot | `0` 현물, `1` 마진 거래 |
| `OrderFilter` | `orderFilter` | Spot | `OrderFilterOrder`, `OrderFilterTPSLOrder`, `OrderFilterStopOrder` |
| `TriggerDirection` | `triggerDirection` | Linear | `1` 상승, `2` 하락 도달 시 트리거, `TriggerPrice` 필요 |
| `TriggerPrice` | `triggerPrice` | Spot·Linear | 지정하면 조건부 주문 |
| `TriggerBy` | `triggerBy` | Linear | `TriggerByLastPrice`, `TriggerByIndexPrice`, `TriggerByMarkPrice` |
| `TakeProfit`, `StopLoss` | `takeProfit`, `stopLoss` | Spot·Linear | 주문과 함께 설정하는 TP/SL 가격, reduceOnly 주문에는 불가 |
| `TPTriggerBy`, `SLTriggerBy` | `tpTriggerBy`, `slTriggerBy` | Linear | TP/SL 트리거 가격 기준 |
| `TPSLMode` | `tpslMode` | Linear | `TPSLModeFull`, `TPSLModePartial`, 지정가 TP/SL은 Partial만 허용 |
| `TPLimitPrice`, `SLLimitPrice` | `tpLimitPrice`, `slLimitPrice` | Spot·Linear | TP/SL 발동 시 지정가, 해당 `OrderType`이 `Limit`일 때만 사용 |
| `TPOrderType`, `SLOrderType` | `tpOrderType`, `slOrderType` | Spot·Linear | TP/SL 발동 시 `Market`(기본) 또는 `Limit` |
| `SMPType` | `smpType` | Spot·Linear | `SMPTypeNone`, `SMPTypeCancelMaker`, `SMPTypeCancelTaker`, `SMPTypeCancelBoth` |

Linear reduceOnly·closeOnTrigger 주문은 `Quantity`를 `"0"`으로 보내 현재 심볼의 전체 포지션을 닫을 수 있습니다. Spot 주문은 `TriggerDirection`, `TriggerBy`, `TPTriggerBy`, `SLTriggerBy`, `TPSLMode`를 받지 않습니다.

## Asset·User API

`asset.go`·`user.go`가 제공하는 자산 이동과 서브 계정·API Key 관리 API입니다. 변경 요청은 자동 재시도하지 않으며 응답이 불명확하면 `UNKNOWN_EXECUTION_STATE`로 반환합니다. 성공 응답은 접수이므로 최종 상태는 이체·출금·전환 조회 API로 확인해야 합니다. 서브 계정·서브 입금 조회는 마스터 UID의 API Key가 필요합니다. rate bucket은 Bybit 문서의 계정별 한도이며 분당 한도는 초당 환산값으로 내려 적용합니다.

### 코인 정보·잔고·이체 (Asset)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `CoinInfo` | `GET /v5/asset/coin/query-info` | `credential.PermissionRead` | `5/account/초` | 코인·체인별 입출금 수수료·최소 수량·상태를 조회합니다. |
| `TransferableCoins` | `GET /v5/asset/transfer/query-transfer-coin-list` | `credential.PermissionRead` | `1/account/초` | 두 계정 유형 사이에 이체 가능한 코인을 조회합니다. |
| `AccountCoinBalance` | `GET /v5/asset/transfer/query-account-coin-balance` | `credential.PermissionRead` | `450/account/초` | 계정 유형의 단일 코인 잔고와 안전 이체 금액을 조회합니다. 마스터 키는 `MemberID`로 서브 UID를 조회합니다. |
| `AccountCoinsBalance` | `GET /v5/asset/transfer/query-account-coins-balance` | `credential.PermissionRead` | `5/account/초` | 계정 유형의 여러 코인(최대 10개, `UNIFIED`는 필수) 잔고를 조회합니다. |
| `CreateInterTransfer` | `POST /v5/asset/transfer/inter-transfer` | `credential.PermissionTransfer` | `1/account/초` | 같은 UID의 계정 유형 사이를 이체합니다. `TransferID`는 호출자가 만든 UUID입니다. |
| `InterTransferRecords` | `GET /v5/asset/transfer/query-inter-transfer-list` | `credential.PermissionRead` | `1/account/초` | 내부 이체 내역을 cursor 기반으로 조회합니다(`Limit` 1~50). |
| `CreateUniversalTransfer` | `POST /v5/asset/transfer/universal-transfer` | `credential.PermissionTransfer` | `5/account/초` | 마스터-서브·서브-서브 UID 사이를 이체합니다. 같은 UID 사이는 거부합니다. |
| `UniversalTransferRecords` | `GET /v5/asset/transfer/query-universal-transfer-list` | `credential.PermissionRead` | `5/account/초` | 유니버설 이체 내역을 cursor 기반으로 조회합니다. |

### 입금 (Asset)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `DepositRecords` | `GET /v5/asset/deposit/query-record` | `credential.PermissionRead` | `1/account/초` | 온체인 입금 내역을 cursor 기반으로 조회합니다. |
| `SubDepositRecords` | `GET /v5/asset/deposit/query-sub-member-record` | `credential.PermissionRead` | `5/account/초` | 서브 UID의 온체인 입금 내역을 조회합니다. |
| `InternalDepositRecords` | `GET /v5/asset/deposit/query-internal-record` | `credential.PermissionRead` | `300/account/초` | Bybit 내부 입금 내역을 조회합니다. 시간 범위를 비우면 최근 30일입니다. |
| `DepositAddress` | `GET /v5/asset/deposit/query-address` | `credential.PermissionRead` | `5/account/초` | 마스터 계정의 입금 주소를 조회합니다. |
| `SubDepositAddress` | `GET /v5/asset/deposit/query-sub-member-address` | `credential.PermissionRead` | `5/account/초` | 서브 UID의 입금 주소를 조회합니다. |

### 출금 (Asset)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `WithdrawRecords` | `GET /v5/asset/withdraw/query-record` | `credential.PermissionRead` | `5/account/초` | 출금 내역을 cursor 기반으로 조회합니다. |
| `WithdrawableAmount` | `GET /v5/asset/withdraw/withdrawable-amount` | `credential.PermissionRead` | `300/account/초` | 지갑(SPOT·FUND·UTA·EARN)별 출금 가능 금액을 조회합니다. |
| `CreateWithdraw` | `POST /v5/asset/withdraw/create` | `credential.PermissionWithdraw` | `5/account/초` | 출금을 요청합니다. 주소는 Bybit 주소록에 등록되어 있어야 하며 `ForceChain`이 2가 아니면 `Chain`이 필수입니다. `Timestamp`가 0이면 시각 보정된 클라이언트 시각을 씁니다. 멱등 확인용 `RequestID`를 지정할 수 있습니다. |
| `CancelWithdraw` | `POST /v5/asset/withdraw/cancel` | `credential.PermissionWithdraw` | `1/account/초` | 처리 전 출금을 취소합니다. `CancelWithdrawResult.Succeeded`로 성공 여부를 확인합니다. |

### 코인 전환 (Asset)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `ConvertCoins` | `GET /v5/asset/exchange/query-coin-list` | `credential.PermissionRead` | `30/account/초` | 전환에 쓸 수 있는 코인(`side` 0 매도·1 매수)을 조회합니다. |
| `RequestConvertQuote` | `POST /v5/asset/exchange/quote-apply` | `credential.PermissionTransfer` | `20/account/초` | 전환 견적(약 15초 유효)을 요청합니다. `RequestCoin`을 비우면 `FromCoin`을 씁니다. |
| `ConfirmConvertQuote` | `POST /v5/asset/exchange/convert-execute` | `credential.PermissionTransfer` | `20/account/초` | 견적을 확정해 전환을 실행합니다. 응답은 접수이며 `ConvertStatus`로 확인합니다. |
| `ConvertStatus` | `GET /v5/asset/exchange/convert-result-query` | `credential.PermissionRead` | `50/account/초` | 견적 ID로 전환 결과를 조회합니다. |
| `ConvertHistory` | `GET /v5/asset/exchange/query-convert-history` | `credential.PermissionRead` | `50/account/초` | 전환 기록을 page index 기반으로 조회합니다(`Limit` 최대 100). |

### 서브 계정·API Key (User)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `CreateSubMember` | `POST /v5/user/create-sub-member` | `credential.PermissionTransfer` | `1/account/초` | 서브 UID를 만듭니다. `Username` 6~16자, `MemberType`은 `SubMemberTypeNormal`(1)·`SubMemberTypeCustodial`(6)입니다. |
| `CreateSubAPIKey` | `POST /v5/user/create-sub-api` | `credential.PermissionTransfer` | `1/account/초` | 서브 UID의 API Key를 만듭니다. 응답의 `Secret`은 이 호출에서만 확인할 수 있습니다. |
| `UpdateSubAPIKey` | `POST /v5/user/update-sub-api` | `credential.PermissionTransfer` | `5/account/초` | 서브 API Key의 읽기 전용 여부·IP 묶음·권한을 수정합니다. `nil` 필드는 바꾸지 않으며 빈 `IPs`는 묶음 해제(`*`)입니다. |
| `DeleteSubAPIKey` | `POST /v5/user/delete-sub-api` | `credential.PermissionTransfer` | `5/account/초` | 서브 API Key를 삭제합니다. 삭제한 키는 즉시 무효가 됩니다. |
| `FreezeSubMember` | `POST /v5/user/frozen-sub-member` | `credential.PermissionTransfer` | `5/account/초` | 서브 UID를 동결·동결 해제합니다. |
| `QuerySubMembers` | `GET /v5/user/query-sub-members` | `credential.PermissionRead` | `10/account/초` | 서브 UID 목록(최대 1만 건)을 조회합니다. |
| `QueryAPIKey` | `GET /v5/user/query-api` | `credential.PermissionRead` | `10/account/초` | 호출에 쓴 API Key의 권한·IP·만료일 정보를 조회합니다. |

아직 구현하지 않은 Asset·User endpoint: `asset-overview`, `delay-amount`, `all-balance` 계열의 일부, `set-deposit-acct`, `submit-info`, `withdraw-address`, `vasp-list`, `questionnaire`, `delivery`, `settlement`, `fund-history`, `convert-small-balance`, `fiat-convert`, `sub-uid-list`, `total-members-assets`, `portfolio-margin`, `page-subuid`, `fund-subuid-list`, `rm-subuid`, `list-sub-apikeys`, `modify-master-apikey`, `rm-master-apikey`, `wallet-type`.

## Market·Account 확장 API

Market 조회는 서명 없이 `credential.PermissionRead` 범위의 공개 요청으로 보내며 기본 `20/route/초` bucket을 사용합니다. Account 조회는 `credential.PermissionRead`, 부채·마진·담보 설정 변경은 `credential.PermissionTrade`이며 변경 요청은 `commonexchange.OperationMutation`으로 한 번만 전송하고 자동 재시도하지 않습니다. 모든 Market 요청은 Linear만 허용합니다.

### Market (Linear)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `MarkPriceCandles` | `GET /v5/market/mark-price-kline` | 공개(서명 없음) | `20/route/초` | 마크가격 캔들(`PriceCandle`, OHLC만). startTime 역순이며 `Limit` 최대 1,000입니다. |
| `IndexPriceCandles` | `GET /v5/market/index-price-kline` | 공개(서명 없음) | `20/route/초` | 지수가격 캔들입니다. |
| `PremiumIndexPriceCandles` | `GET /v5/market/premium-index-price-kline` | 공개(서명 없음) | `20/route/초` | 프리미엄 지수 캔들이며 값이 음수일 수 있습니다. |
| `FundingRateHistory` | `GET /v5/market/funding/history` | 공개(서명 없음) | `20/route/초` | 펀딩비 이력(`FundingRateRecord`). `StartTime`만 지정하면 Bybit가 오류를 내므로 로컬에서 거부하며 `Limit` 최대 200입니다. |
| `OpenInterest` | `GET /v5/market/open-interest` | 공개(서명 없음) | `20/route/초` | 미결제약정 이력(`OpenInterestPage`). `IntervalTime`은 `DataPeriod5Minutes`~`DataPeriod1Day`이며 `Limit` 최대 200입니다. |
| `Insurance` | `GET /v5/market/insurance` | 공개(서명 없음) | `20/route/초` | 보험기금 코인별 잔고(`InsurancePool`)이며 24시간 주기로 갱신됩니다. `Coin`을 비우면 전체입니다. |
| `RiskLimits` | `GET /v5/market/risk-limit` | 공개(서명 없음) | `20/route/초` | 심볼의 리스크 한도 티어(`RiskLimitTier`). 증거금율은 숫자·문자열 모두 `DecimalString`으로 받습니다. |
| `DeliveryPrices` | `GET /v5/market/delivery-price` | 공개(서명 없음) | `20/route/초` | 선물 인도가격(`DeliveryPricePage`)이며 `Limit` 최대 200입니다. |
| `AccountRatios` | `GET /v5/market/account-ratio` | 공개(서명 없음) | `20/route/초` | 순매수·순매도 사용자 비율(`AccountRatioRecord`). `Period`는 `DataPeriod*`이며 `Limit` 최대 500입니다. |

서버 시간(`ServerTime`)은 이미 제공합니다. `historical-volatility`는 Option 전용이라 구현하지 않습니다.

### Account 조회

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `AccountInfo` | `GET /v5/account/info` | `credential.PermissionRead` | `50/account/초` | 마진 모드와 계정 상태(`AccountInfo`)를 조회합니다. |
| `FeeRates` | `GET /v5/account/fee-rate` | `credential.PermissionRead` | `5/account/초` | Spot·Linear 심볼의 maker·taker 수수료율을 조회합니다. |
| `TransactionLog` | `GET /v5/account/transaction-log` | `credential.PermissionRead` | `25/account/초` | 거래 로그(원장)를 cursor 기반으로 조회합니다. 시간 범위를 둘 다 지정하면 7일 이하, `Limit` 최대 50입니다. |
| `BorrowHistory` | `GET /v5/account/borrow-history` | `credential.PermissionRead` | `50/account/초` | 차입 이자 이력을 조회합니다. 시간 범위는 30일 이하, `Limit` 최대 50입니다. |
| `CollateralInfos` | `GET /v5/account/collateral-info` | `credential.PermissionRead` | `50/account/초` | 코인별 담보·차입 한도 정보를 조회합니다. |
| `PayInfo` | `GET /v5/account/pay-info` | `credential.PermissionRead` | `50/account/초` | 상환 전 사용 가능한 담보와 차입 현황을 조회합니다. `Coin`은 미상환 부채가 있어야 합니다. |
| `DCPInfos` | `GET /v5/account/query-dcp-info` | `credential.PermissionRead` | `5/account/초` | 상품별 DCP 설정을 조회합니다. DCP를 신청한 계정만 값을 받습니다. |
| `SMPGroup` | `GET /v5/account/smp-group` | `credential.PermissionRead` | `5/account/초` | 자전거래 방지(SMP) 그룹 ID를 조회합니다. |
| `UserSettingConfig` | `GET /v5/account/user-setting-config` | `credential.PermissionRead` | `50/account/초` | 가격 제한 초과 시 동작, 델타 중립, MNT 수수료 공제 설정을 조회합니다. |
| `SpotTradeAnalysis` | `GET /v5/account/trade-info-for-analysis` | `credential.PermissionRead` | `50/account/초` | Spot 심볼 체결 집계와 일별 내역을 조회합니다. |

### Account 설정·부채 (변경)

| 메서드 | HTTP·경로 | 권한 | rate bucket | 설명 |
|---|---|---|---|---|
| `SetMarginMode` | `POST /v5/account/set-margin-mode` | `credential.PermissionTrade` | `5/account/초` | `MarginModeIsolated`·`MarginModeRegular`·`MarginModePortfolio`로 변경합니다. 조건 미충족 사유는 Bybit 오류 응답의 `result.reasons`에 담깁니다. |
| `SetCollateralSwitch` | `POST /v5/account/set-collateral-switch` | `credential.PermissionTrade` | `5/account/초`(문서 미표기, 일괄 설정 값 준용) | 코인 하나의 담보 사용 여부를 설정합니다. USDT·USDC는 끌 수 없어 로컬에서 거부합니다. |
| `BatchSetCollateralSwitch` | `POST /v5/account/set-collateral-switch-batch` | `credential.PermissionTrade` | `5/account/초` | 여러 코인의 담보 사용 여부를 한 번에 설정하고 코인별 반영 상태를 반환합니다. |
| `SetDeltaMode` | `POST /v5/account/set-delta-mode` | `credential.PermissionTrade` | `5/account/초`(문서 미표기, 보수적으로 5) | 델타 중립 모드를 켜거나 끕니다. 현재 상태는 `UserSettingConfig`의 `DeltaEnabled`로 확인합니다. |
| `Borrow` | `POST /v5/account/borrow` | `credential.PermissionTrade` | `1/account/초` | 변동금리 수동 차입으로 부채를 만듭니다. |
| `Repay` | `POST /v5/account/repay` | `credential.PermissionTrade` | `1/account/초` | 수동 상환입니다. 현물 잔고가 부족하면 청산 순서대로 다른 자산을 전환합니다. 코인 없이 전체를 상환하려면 `RepaymentTypeAll`이 필요합니다. |
| `RepayWithoutConversion` | `POST /v5/account/no-convert-repay` | `credential.PermissionTrade` | `1/account/초` | 부채 코인의 현물 잔고로만 상환하며 `Coin`이 필수입니다. |
| `RepayLiability` | `POST /v5/account/quick-repayment` | `credential.PermissionTrade` | `1/account/초` | 부채를 일괄 상환합니다. `Coin`을 비우면 모든 코인이 대상입니다. |

`Repay`·`RepayWithoutConversion`의 `FIXED`·`ALL`은 비동기로 처리되므로 `RepayResult.ResultStatus`(`P` 처리 중, `SU` 성공, `FA` 실패)만으로 최종 상태를 단정하지 말고 `CollateralInfos`로 부채 잔액을 다시 확인합니다. 차입·상환은 자산 이동이 아니라 계정 내 부채 변경이므로 `credential.PermissionTransfer`가 아닌 `PermissionTrade`로 분류합니다.

### 구현하지 않은 항목

- `/v5/market/historical-volatility`: Option 전용이며 이 패키지는 Option을 지원하지 않습니다.
- `/v5/market/adlAlert`, `full_orderbook`, `rpi_orderbook`, `price-limit`, `index-price-components`, `fee-group-info`, `instrument-summary`: 공식 문서 원문에서 경로·파라미터를 확인하지 못해 구현하지 않았습니다.
- `/v5/account/withdrawal`, `/v5/account/set-limit-px-action`, `/v5/account/instruments-info`: 공식 문서 페이지에서 요청·응답 명세를 확인하지 못해 구현하지 않았습니다.

## 공식 기준 문서

- [Bybit V5 Integration Guidance](https://bybit-exchange.github.io/docs/v5/guide)
- [Bybit V5 Rate Limit](https://bybit-exchange.github.io/docs/v5/rate-limit)
- [Bybit V5 Market](https://bybit-exchange.github.io/docs/v5/market/instrument)
- [Bybit V5 Place Order](https://bybit-exchange.github.io/docs/v5/order/create-order)
- [Bybit V5 Amend Order](https://bybit-exchange.github.io/docs/v5/order/amend-order)
- [Bybit V5 Cancel All Orders](https://bybit-exchange.github.io/docs/v5/order/cancel-all)
- [Bybit V5 Batch Place Order](https://bybit-exchange.github.io/docs/v5/order/batch-place)
- [Bybit V5 Set Leverage](https://bybit-exchange.github.io/docs/v5/position/leverage)
- [Bybit V5 Move Position](https://bybit-exchange.github.io/docs/v5/position/move-position)
- [Bybit V5 Wallet Balance](https://bybit-exchange.github.io/docs/v5/account/wallet-balance)
- [Bybit V5 Asset](https://bybit-exchange.github.io/docs/v5/asset/coin-info)
- [Bybit V5 User](https://bybit-exchange.github.io/docs/v5/user/create-subuid)
- [Bybit V5 WebSocket Connect](https://bybit-exchange.github.io/docs/v5/ws/connect)
- [Bybit V5 Public Orderbook](https://bybit-exchange.github.io/docs/v5/websocket/public/orderbook)
- [Bybit V5 Private Order](https://bybit-exchange.github.io/docs/v5/websocket/private/order)
