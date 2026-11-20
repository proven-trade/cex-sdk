# Bybit V5 Spot·Linear 어댑터

## 범위

`exchange/bybit` 패키지는 Bybit V5의 다음 상품과 REST 기능을 제공합니다.

| Category | 공개 시세 | 계정 | 포지션 | 주문 |
|---|---:|---:|---:|---:|
| `spot` | 지원 | Unified 잔고 | 해당 없음 | 지원 |
| `linear` | 지원 | Unified 잔고 | 지원 | 지원 |

공개 시세는 상품 정보, ticker, 호가, 최근 체결, 캔들을 포함합니다. 주문은 생성, 정정, 단건 조회, 취소, 전체 취소, 일괄 생성·정정·취소, 미체결 목록, 이력을 포함하며 생성 요청은 조건부 주문·TP/SL·SMP 필드를 지원합니다. Linear 포지션은 레버리지, 포지션 모드, TP/SL·트레일링 스톱, 증거금, 청산 손익, 체결 내역, 포지션 이전을 지원합니다. 새 API 목록은 [Trade·Position 확장 API](#tradeposition-확장-api)를 참고합니다. Inverse, Option, 자산 이동은 아직 범위에 포함하지 않습니다.

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
- [Bybit V5 WebSocket Connect](https://bybit-exchange.github.io/docs/v5/ws/connect)
- [Bybit V5 Public Orderbook](https://bybit-exchange.github.io/docs/v5/websocket/public/orderbook)
- [Bybit V5 Private Order](https://bybit-exchange.github.io/docs/v5/websocket/private/order)
