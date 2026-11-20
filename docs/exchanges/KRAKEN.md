# Kraken Spot REST·WebSocket v2 어댑터

## 범위

현재 범위는 Kraken Spot REST API와 WebSocket v2다.

- 서버 시간
- Spot 상품 정보와 주문 단위
- 전체 또는 선택 상품 ticker
- L2 호가, 최근 공개 체결, OHLCV
- 계정의 자산별 총 잔고와 주문 보류액을 포함한 확장 잔고
- 시장가·지정가 GTC·IOC·FOK 주문 생성
- 최대 50건 주문 조회와 단건 취소
- 미체결·종료 주문 목록과 계정 체결 이력
- WebSocket ticker, L2 호가, 체결, OHLC, 상품 규칙
- WebSocket 주문·체결과 잔고 account stream
- 요청별 송신 경로 선택
- 자산 정보, 시스템 상태, 스프레드, 집계 호가, L3 호가, 투명성 데이터
- 주문 정정, 일괄 주문·취소, 전체 취소와 dead man's switch
- TWAP 알고리즘 전략 생성·취소·조회
- 증거금 잔고·포지션, 원장, 30일 거래량·수수료 등급, 보고서 export, API key 정보
- Earn 전략·배분 조회와 배분·해제
- 입출금 수단·주소·상태, 출금 요청·취소, 지갑 이체와 서브계정 관리

추가된 REST API의 endpoint, 필요한 SDK 권한과 주의사항은 [추가 REST API](#추가-rest-api)에 정리한다. Futures는 별도 package로 분리한다. 지원하지 않는 주문 종류와 WebSocket 구독 조합은 전송 전에 검증 오류로 거부한다.

## 자격증명

`credential.Material`에는 다음 값을 저장한다.

| 필드 | 값 |
|---|---|
| `APIKey` | Kraken public API key 원문 |
| `SecretKey` | Kraken이 발급한 Base64 secret 원문 |
| `Passphrase` | 사용하지 않음 |

조회 API key에는 `read`, 주문 API key에는 `trade` 상위 권한을 선언한다. 내부 이체·서브계정·입금 주소 생성·Earn 배분은 `transfer`, 출금 요청과 취소는 `withdraw`를 선언한다. 실제 Kraken key에도 호출 endpoint에 대응하는 funds·orders·data 권한을 부여해야 한다. API key에 API 2FA를 켜면 private 요청마다 유효한 `otp`가 필요하므로 현재 범위에서는 지원하지 않는다.

```go
descriptor := &credential.Descriptor{
	AccountID:  "kraken-main",
	Exchange:   model.ExchangeKraken,
	SecretRef:  "secret/kraken/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
}
```

private 요청 body는 `application/x-www-form-urlencoded`로 직렬화한다. 중첩 배열이나 JSON 전용 필드가 필요한 `Level3`, `AddOrderBatch`, `CancelOrderBatch`, 알고리즘 전략, Earn 요청은 `application/json`으로 직렬화하며 nonce는 JSON 숫자로 넣는다. 두 경우 모두 서명은 실제 전송하는 본문 문자열 전체를 대상으로 한다. `API-Sign`은 URI path와 `SHA256(nonce + POST data)`를 결합한 뒤 Base64 decode한 secret으로 HMAC-SHA-512를 계산하고 다시 Base64로 인코딩한다.

## 클라이언트

```go
client, err := kraken.New(kraken.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

tickers, err := client.Tickers(
	ctx,
	kraken.TickersRequest{Pairs: []string{"XBTUSD", "ETHUSD"}},
	trade.WithEgressRoute("seoul-b"),
)
```

Kraken 응답의 상품 key는 요청 alias와 다를 수 있다. 예를 들어 `XBTUSD` 요청 결과가 `XXBTZUSD` key로 반환될 수 있으므로 `Ticker.PairID`, `OrderBook.PairID`, `RecentTrades.PairID`, `Candles.PairID`에 실제 응답 key를 보존한다.

## 공개 시세

| 메서드 | endpoint | 주의사항 |
|---|---|---|
| `ServerTime` | `GET /0/public/Time` | Unix 초와 RFC 1123 원문 반환 |
| `AssetPairs` | `GET /0/public/AssetPairs` | 상품 ID, alias, 주문 최소값, tick size 반환 |
| `Tickers` | `GET /0/public/Ticker` | 상품 목록을 비우면 전체 ticker 반환 |
| `OrderBook` | `GET /0/public/Depth` | 단일 상품, 최대 500개 L2 가격 레벨 |
| `RecentTrades` | `GET /0/public/Trades` | `Last`를 다음 `Since`에 전달 |
| `Candles` | `GET /0/public/OHLC` | 공식 계약상 최대 최근 720개 반환 |

OHLC 응답의 마지막 항목은 아직 끝나지 않은 현재 구간이며 `Since` 값과 관계없이 항상 포함된다. 확정 봉만 필요한 전략은 현재 시각과 interval을 비교해 마지막 항목을 제외해야 한다.

### 과거 캔들

OHLC는 `Since`를 과거로 지정해도 최근 720개 밖의 구간을 반환하지 않는다. 그 이전 캔들은 상장 시점부터 보존되는 `Trades` 이력으로 합성한다. `RecentTrades`의 `Last`를 다음 `Since`에 넘겨 오래된 체결부터 차례로 받고, `PublicTrade.Unified`로 변환한 체결을 `unified.TradeCandleBuilder`에 넣는다. 합성기는 진행 중인 구간 하나만 보관하므로 페이지 경계에서 구간이 나뉘지 않는다.

```go
builder, err := unified.NewTradeCandleBuilder(time.Minute, unified.TradeOrderAscending)
if err != nil {
	return err
}
since := strconv.FormatInt(listingTime.UnixNano(), 10)
for {
	page, err := client.RecentTrades(ctx, kraken.RecentTradesRequest{Pair: "XBTUSD", Since: since, Count: 1000})
	if err != nil {
		return err
	}
	if len(page.Trades) == 0 {
		break
	}
	for _, item := range page.Trades {
		tick, err := item.Unified()
		if err != nil {
			return err
		}
		candle, completed, err := builder.Add(tick)
		if err != nil {
			return err
		}
		if completed {
			store(candle)
		}
	}
	since = page.Last
}
```

체결이 없는 구간은 만들지 않으며 `Flush`로 꺼낸 마지막 구간은 이후 체결이 더 있을 수 있다. 체결 한 페이지는 최대 1,000건이므로 거래량이 많은 상품의 전체 이력은 많은 요청을 소비하며 공개 요청 제한을 따른다.

## 주문과 계정

```go
reference, err := client.PlaceOrder(
	ctx,
	kraken.PlaceOrderRequest{
		Pair:          "XBTUSD",
		Side:          kraken.SideBuy,
		OrderType:     kraken.OrderTypeLimit,
		Volume:        "0.01",
		Price:         "64000",
		ClientOrderID: "strategy-1",
		PostOnly:      true,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

첫 범위의 `PlaceOrder`는 Spot 시장가와 지정가만 지원한다. `ClientOrderID`는 Kraken 계약에 맞춰 최대 18자의 안전한 문자열 또는 UUID를 허용한다. `ValidateOnly`는 검증만 수행하며 이때 빈 transaction ID 목록도 정상 결과다.

시장가 매수에서 `VolumeInQuote`를 사용하면 Kraken `viqc` 플래그로 결제 자산 금액을 전달한다. 지정가 주문의 `TimeInForce`는 `GTC`, `IOC`, `FOK`를 지원하며 post-only는 GTC 지정가와만 함께 사용할 수 있다.

`ExtendedBalance`는 `BalanceEx`의 총액, 신용, 사용 신용, Spot 주문 보류액을 decimal 문자열로 보존한다. 공통 Spot 잔고는 공식 식인 `balance + credit - credit_used - hold_trade`를 주문 가능액으로 사용하고 `hold_trade`를 잠금액으로 사용한다.

`CancelOrder`는 취소 제한을 계정+상품 단위로 안전하게 적용하기 위해 `Pair`를 요구한다. `TransactionID`와 `ClientOrderID` 중 정확히 하나를 지정하며 각각 Kraken의 `txid`와 `cl_ord_id`로 전송한다. 취소 접수 뒤 최종 상태는 `OrderInfo`, `OpenOrders`, `ClosedOrders`에서 확인한다.

주문 생성과 취소의 전송 오류, timeout, 읽을 수 없는 성공 응답은 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. `ClientOrderID`와 주문 조회를 이용해 실제 처리 여부를 먼저 확인해야 한다.

## 추가 REST API

아래 API는 같은 `Client`, 송신 경로 선택, nonce, limiter, 오류 분류 규칙을 따른다. 모든 메서드는 `func (c *Client) Name(ctx, request, options ...trade.RequestOption)` 형태이며 응답 구조체의 `Raw`에 공식 result 원문을 보존한다. SDK 권한 열은 `credential.Descriptor`에 선언해야 하는 상위 권한이고, 실제 Kraken key에는 각 endpoint가 요구하는 세부 권한을 부여해야 한다.

변경 요청은 모두 자동 재시도하지 않는다. 전송 오류, timeout, 읽을 수 없는 성공 응답과 접수 여부를 확인할 수 없는 빈 응답은 `UNKNOWN_EXECUTION_STATE`로 반환하므로 주문·전략·출금 조회로 실제 처리 여부를 먼저 확인해야 한다. 수치는 문자열로 보존하며 Kraken이 문자열 또는 숫자로 내려주는 필드는 `DecimalString` 또는 원문 문자열로 정밀도 손실 없이 읽는다.

### 시장 데이터

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `Assets` | `GET /0/public/Assets` | 없음 | 자산 정보. 자산 목록을 비우면 전체 |
| `SystemStatus` | `GET /0/public/SystemStatus` | 없음 | 거래 엔진 상태와 장애 공지 |
| `MaintenanceSchedule` | `GET /0/public/MaintenanceSchedule` | 없음 | 향후 7일 점검 일정 |
| `Spreads` | `GET /0/public/Spread` | 없음 | 단일 상품의 최근 top-of-book 스프레드, `Last`를 다음 `Since`에 전달 |
| `GroupedBook` | `GET /0/public/GroupedBook` | 없음 | 가격 구간별 수량 집계 호가 |
| `Level3` | `POST /0/private/Level3` | `read` | 개별 주문 단위 L3 호가. JSON 본문, Kraken 주문·체결 조회 권한 필요 |
| `PreTrade` | `GET /0/public/PreTrade` | 없음 | 상위 10호가 거래 전 투명성 데이터 |
| `PostTrade` | `GET /0/public/PostTrade` | 없음 | 거래 후 투명성 체결 데이터 |

### 주문 정정·일괄·전체 취소

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `AmendOrder` | `POST /0/private/AmendOrder` | `trade` | 대기열 위치를 유지하는 원자적 정정. `TransactionID` 또는 `ClientOrderID` 중 하나와 변경할 값이 필요하며 계정+상품 주문 제한을 쓴다 |
| `EditOrder` | `POST /0/private/EditOrder` | `trade` | 기존 주문을 취소하고 새 주문으로 재등록하는 레거시 정정. 새 주문 ID는 `EditOrderResult.TransactionID` |
| `AddOrderBatch` | `POST /0/private/AddOrderBatch` | `trade` | 단일 상품 2~15건, JSON 본문. `ValidateOnly`이 아니면 접수 개수가 요청과 다를 때 `UNKNOWN_EXECUTION_STATE` |
| `CancelOrderBatch` | `POST /0/private/CancelOrderBatch` | `trade` | 거래소 주문 ID 또는 client order ID 최대 50건, JSON 본문 |
| `CancelAll` | `POST /0/private/CancelAll` | `trade` | 계정의 모든 미체결 주문 취소 |
| `CancelAllOrdersAfter` | `POST /0/private/CancelAllOrdersAfter` | `trade` | dead man's switch. `Timeout`이 0이면 타이머 해제, 15~30초마다 60초 timeout으로 갱신 |

### 알고리즘 전략

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `AddStrategy` | `POST /0/private/AddStrategy` | `trade` | TWAP 생성, JSON 본문. `ExpireTime`은 `+초` 또는 Unix 초이고 `ValidateOnly`이면 빈 ID가 정상이다 |
| `CancelStrategy` | `POST /0/private/CancelStrategy` | `trade` | pending·open 전략 취소. 이미 제출된 하위 주문은 원장에 체결될 수 있다 |
| `OpenStrategies` | `POST /0/private/GetOpenStrategies` | `read` | pending·open 전략, 전략 ID 순서 |
| `QueryStrategies` | `POST /0/private/QueryStrategies` | `read` | 전략 ID 최대 20건 |
| `ClosedStrategies` | `POST /0/private/GetClosedStrategies` | `read` | closed·expired·cancelled 전략, cursor 페이지네이션이며 다음 페이지에도 같은 필터를 반복해야 한다 |

### 계정 조회와 보고서

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `TradeBalance` | `POST /0/private/TradeBalance` | `read` | 담보 잔고, 평가액, 자본과 증거금 수준 요약 |
| `QueryTrades` | `POST /0/private/QueryTrades` | `read` | 체결 ID 최대 20건, 이력 조회 한도 |
| `OpenPositions` | `POST /0/private/OpenPositions` | `read` | 증거금 미결제 포지션. `ConsolidateByMarket`이면 `Consolidated`를 채운다 |
| `Ledgers` | `POST /0/private/Ledgers` | `read` | `WithCursor`로 cursor 페이지네이션, 이력 조회 한도 |
| `QueryLedgers` | `POST /0/private/QueryLedgers` | `read` | 원장 ID 최대 20건, 이력 조회 한도 |
| `TradeVolume` | `POST /0/private/TradeVolume` | `read` | 30일 거래량과 maker·taker 수수료 등급 |
| `OrderAmends` | `POST /0/private/OrderAmends` | `read` | 주문 정정 이력 |
| `CreditLines` | `POST /0/private/CreditLines` | `read` | VIP 신용 한도. 신용 한도가 없으면 빈 결과 |
| `APIKeyInfo` | `POST /0/private/GetApiKeyInfo` | `read` | 요청에 쓴 API Key의 권한·허용 IP·사용 시각. 공식 문서가 snake_case와 camelCase를 함께 싣어 두 표기를 모두 수용한다. API 2FA가 있으면 `OTP`를 전달한다 |
| `ListWalletAccounts` | `POST /0/private/ListWalletAccounts` | `read` | 지갑 계정 목록 |
| `AddExport` | `POST /0/private/AddExport` | `read` | 보고서 생성 요청이므로 변경 요청으로 취급하며 Kraken `Data - Export data` 권한이 필요하다 |
| `ExportStatus` | `POST /0/private/ExportStatus` | `read` | 보고서 생성 상태 |
| `RetrieveExport` | `POST /0/private/RetrieveExport` | `read` | 성공 응답은 JSON이 아닌 zip 바이너리이며 `ExportFile.Data`에 담는다 |
| `RemoveExport` | `POST /0/private/RemoveExport` | `read` | 보고서 취소(`cancel`) 또는 삭제(`delete`), 변경 요청으로 취급한다 |

### Earn

Earn 요청은 JSON 본문이다. 배분과 해제는 비동기라서 접수 응답이 `true`여도 완료를 뜻하지 않으며 상태 조회의 `Pending`이 false가 될 때 끝난다. 같은 사용자와 전략에는 진행 중인 배분·해제 요청이 하나만 있을 수 있다.

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `EarnStrategies` | `POST /0/private/Earn/Strategies` | `read` | 사용 가능한 전략과 잠금 방식·기간, 수수료, 예상 수익률 |
| `EarnAllocations` | `POST /0/private/Earn/Allocations` | `read` | 상태별 배분 금액과 다음 보상 지급 정보 |
| `EarnAllocate` | `POST /0/private/Earn/Allocate` | `transfer` | 자금 배분, 비동기 |
| `EarnDeallocate` | `POST /0/private/Earn/Deallocate` | `transfer` | 배분 해제, 비동기 |
| `EarnAllocateStatus` | `POST /0/private/Earn/AllocateStatus` | `read` | 마지막 배분 요청 진행 여부 |
| `EarnDeallocateStatus` | `POST /0/private/Earn/DeallocateStatus` | `read` | 마지막 해제 요청 진행 여부 |

### 입출금과 이체

입출금 메서드는 아래 `DepositMethods`부터 `WalletTransfer`까지 Kraken 공식 명세가 deprecated로 표시한 endpoint다. 계속 동작하지만 더 이상 갱신되지 않으며 Kraken은 Funding (Beta) API로의 이전을 권장한다. 이 클라이언트는 `/0/private/*` form 본문 endpoint만 제공하며 `account_id` 다중 지갑 파라미터는 보내지 않는다.

| 메서드 | endpoint | SDK 권한 | 주의사항 |
|---|---|---|---|
| `DepositMethods` | `POST /0/private/DepositMethods` | `read` | 입금 수단. `limit`이 `false`이면 `DepositMethod.Limit`은 빈 문자열이다 |
| `DepositAddresses` | `POST /0/private/DepositAddresses` | 조회 `read`, `New`이면 `transfer` | 주소 생성은 변경 요청이다. `Tag`는 XRP destination tag, `Memo`는 STX·XLM·EOS memo다 |
| `DepositStatus` | `POST /0/private/DepositStatus` | `read` | 최신순, `Paginated` 또는 `Cursor`로 페이지네이션 |
| `WithdrawMethods` | `POST /0/private/WithdrawMethods` | `read` | 출금 수단, 수수료와 시간 창별 한도 |
| `WithdrawAddresses` | `POST /0/private/WithdrawAddresses` | `read` | 계정에 등록된 출금 주소, `Verified`로 인증 여부 필터 |
| `WithdrawInfo` | `POST /0/private/WithdrawInfo` | `read` | 출금 키와 금액에 대한 수수료·한도 추정 |
| `Withdraw` | `POST /0/private/Withdraw` | `withdraw` | 출금 대상은 계정에 등록한 `Key`로 지정하고 `Address`는 일치 확인용이다 |
| `WithdrawStatus` | `POST /0/private/WithdrawStatus` | `read` | 최신순, `DepositStatus`와 같은 요청 형식 |
| `WithdrawCancel` | `POST /0/private/WithdrawCancel` | `withdraw` | 아직 처리되지 않은 출금만 취소한다. `Canceled`가 false면 거래소가 취소하지 못한 것이다 |
| `WalletTransfer` | `POST /0/private/WalletTransfer` | `transfer` | Spot 지갑에서 Futures 지갑으로만 이체한다. 반대 방향은 Futures API로만 가능하다 |
| `CreateSubaccount` | `POST /0/private/CreateSubaccount` | `transfer` | 기관 마스터 계정 API Key 전용, Kraken은 `Funds permissions - Withdraw` 권한을 요구한다 |
| `AccountTransfer` | `POST /0/private/AccountTransfer` | `transfer` | 마스터·서브계정 간 이체, 마스터 계정 API Key 전용, Kraken은 `Funds permissions - Withdraw` 권한을 요구한다 |

입금 주소를 새로 만드는 `DepositAddresses`, 출금, 이체, 서브계정 생성은 성공 응답이어도 접수 확인 필드(`refid`, `transfer_id`, `true`)가 비어 있으면 실패가 아니라 `UNKNOWN_EXECUTION_STATE`로 분류한다. 출금은 `WithdrawStatus`, 이체는 `Ledgers`로 처리 여부를 확인한다.

## nonce

private 요청은 limiter 대기가 끝난 다음 Secret을 조회하고 서명한다. 한 `Client`에서 생성하는 millisecond nonce는 시계가 멈추거나 뒤로 이동하고 여러 goroutine이 동시에 호출해도 원자적으로 항상 증가한다.

Kraken nonce는 API key 단위다. 같은 API key를 여러 프로세스나 여러 `Client`가 동시에 공유하면 각 로컬 증가 규칙만으로 전체 순서를 보장할 수 없다. 운영에서는 API key 하나당 장수명 `Client` 하나를 사용하거나 외부에서 공유하는 단조 nonce 정책을 적용해야 한다.

## 요청 제한

기본 공개 제한은 route, 즉 송신 IP별 초당 1건이다. 공식 정책상 `Trades`와 `OHLC`는 IP+상품별 제한이고 나머지는 IP별 제한이지만, 첫 범위는 모든 공개 호출을 송신 IP별 초당 1건으로 더 보수적으로 직렬화한다. `PublicRequestsPerSecond`로 조정할 수 있다.

private account endpoint는 API key counter를 근사해 기본 40초 구간에서 20 point를 허용한다. 일반 조회는 1 point, `ClosedOrders`와 `TradesHistory`는 공식 증가량에 맞춰 4 point다. `QueryTrades`, `Ledgers`, `QueryLedgers`, `ClosedStrategies`도 같은 이력 조회 한도를 적용한다. `CancelOrderBatch`, `CancelAll`, `CancelAllOrdersAfter`와 입출금·Earn·전략 요청은 계정 counter를 사용한다. 실제 정책은 연속 감소 counter이므로 로컬 fixed window는 보수적 근사다. `PrivateCounterLimit`과 `PrivateCounterWindow`로 계정 등급에 맞춰 조정할 수 있다.

주문 생성·취소와 `AmendOrder`, `EditOrder`, `AddOrderBatch`는 private account counter와 분리하고 계정+상품별 기본 초당 1건으로 제한한다. Kraken의 실제 trading limiter는 주문이 장부에 머문 시간과 상호작용에 따른 point 방식이므로 로컬 제한이 거래소 제한을 대체하지 않는다. `TradingRequestsPerSecond`는 운영 전략에 맞춰 조정하되 `EOrder:Rate limit exceeded`를 관측해야 한다.

HTTP 429·418의 `Retry-After`는 공통 limiter에 반영한다. HTTP 200이어도 `error` 배열이 비어 있지 않으면 실패이며 인증, 권한, 제한, 잔고 부족, 주문 없음, 거래소 장애 범주로 정규화한다.

## 다중 송신 IP

public 요청 제한은 선택한 route별로 분리된다. private account와 주문 제한은 송신 경로를 바꿔도 계정 규칙을 우회하지 않도록 account ID와 상품을 기준으로 공유한다. Secret을 조회하기 전에 API key의 route 허용 목록을 검사한다.

## WebSocket v2

public endpoint는 `wss://ws.kraken.com/v2`, private endpoint는 `wss://ws-auth.kraken.com/v2`다. public stream은 다음 channel을 지원한다.

| channel | typed data | 구독 옵션 |
|---|---|---|
| `ticker` | `SpotStreamTicker` | 상품, `trades` 또는 `bbo` 갱신 기준, snapshot |
| `book` | `SpotStreamBook` | 상품, depth 10·25·100·500·1000, snapshot |
| `trade` | `SpotStreamTrade` | 상품, snapshot |
| `ohlc` | `SpotStreamOHLC` | 상품, 공식 interval, snapshot |
| `instrument` | `SpotStreamInstrument` | snapshot |

private stream은 `executions`의 주문·체결과 `balances`의 잔고 event를 지원한다. `executions`의 주문 snapshot, 체결 snapshot, 주문 상태 포함 여부와 `balances` snapshot 여부를 각각 설정할 수 있다. 가격·수량·수수료는 JSON 숫자와 문자열이 섞여도 `SpotStreamDecimal`로 원문 정밀도를 보존한다.

```go
streamClient, err := kraken.NewSpotStreamClient(kraken.SpotStreamClientConfig{
	Connector:            streamConnector,
	RESTClient:           client,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	kraken.SpotPublicStreamRequest{Subscriptions: []kraken.SpotPublicSubscription{
		{
			Channel:      kraken.SpotChannelTicker,
			Symbols:      []string{"BTC/USD"},
			EventTrigger: kraken.SpotTickerOnBBO,
		},
		{
			Channel: kraken.SpotChannelBook,
			Symbols: []string{"BTC/USD"},
			Depth:   100,
		},
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(_ context.Context, message kraken.SpotStreamMessage) error {
	if message.Channel != string(kraken.SpotChannelTicker) {
		return nil
	}
	var tickers []kraken.SpotStreamTicker
	return message.DecodeData(&tickers)
})
```

public stream은 `Subscribe`와 `Unsubscribe`로 구독을 변경하며, 연결이 끊기면 현재 구독 집합을 정렬된 순서로 복구한다. 같은 세션의 최초 연결과 모든 재연결은 생성할 때 선택한 송신 경로에 고정된다.

private stream은 WebSocket 연결 직후 REST `GetWebSocketsToken`을 호출한다. 토큰은 발급 후 15분 안에 구독에 사용해야 하며 성공한 private 구독의 연결이 유지되는 동안에는 유효하다. 재연결 때는 이전 토큰을 재사용하지 않고 새 토큰을 발급한 뒤 `executions`와 `balances`를 다시 구독한다. REST 토큰 요청과 WebSocket handshake에는 반드시 같은 route를 사용하므로 API key IP 허용 목록과 실제 공인 송신 IP가 일치한다.

```go
private, err := streamClient.PrivateStream(
	kraken.SpotPrivateStreamRequest{Subscriptions: []kraken.SpotPrivateSubscription{
		{Channel: kraken.SpotChannelExecutions},
		{Channel: kraken.SpotChannelBalances},
	}},
	trade.WithEgressRoute("seoul-b"),
)
```

`book`을 로컬 장부로 소비할 때는 위 `public.Run` 대신 `SpotLocalOrderBook.Run`이 해당 public stream을 소유하도록 한다.

```go
book, err := kraken.NewSpotLocalOrderBook(kraken.SpotLocalOrderBookConfig{
	Symbol:        "BTC/USD",
	Depth:         100,
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view kraken.SpotLocalOrderBookView) error {
	return handleBook(view)
})
```

SDK는 한 메시지의 모든 가격 변경을 배열 순서대로 반영하고 구독 depth로 장부를 자른 뒤 CRC32 checksum을 검증한다. checksum은 구독 depth와 관계없이 ask 상위 10단계를 가격 오름차순, bid 상위 10단계를 가격 내림차순으로 이어 계산한다. 계산 중 가격·수량의 소수점만 제거하고 선행 0을 잘라내므로 `SpotStreamDecimal`의 원문 정밀도를 유지한다.

checksum이 다르거나 새 연결에서 snapshot보다 update가 먼저 오면 현재 장부를 폐기하고 같은 송신 경로로 재연결해 새 snapshot을 받는다. `SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 checksum·snapshot 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타낸다. `ViewDepth` 기본값은 `min(20, Depth)`이며 handler 출력만 제한하고 내부 장부는 구독 depth까지 유지한다. symbol·depth·snapshot·송신 경로 계약이 public stream과 다르면 네트워크 연결 전에 거부한다.

## 공식 문서

- [Spot REST 인증](https://docs.kraken.com/exchange/guides/rest/authentication)
- [Spot WebSocket v2 안내](https://docs.kraken.com/api/docs/websocket-v2/)
- [WebSocket token 발급](https://docs.kraken.com/api/docs/rest-api/get-websockets-token/)
- [WebSocket v2 ticker](https://docs.kraken.com/api/docs/websocket-v2/ticker/)
- [WebSocket v2 book](https://docs.kraken.com/api/docs/websocket-v2/book/)
- [WebSocket v2 book checksum](https://docs.kraken.com/exchange/guides/websockets/book-checksum-v2)
- [WebSocket v2 executions](https://docs.kraken.com/api/docs/websocket-v2/executions/)
- [WebSocket v2 balances](https://docs.kraken.com/api/docs/websocket-v2/balances/)
- [서버 시간](https://docs.kraken.com/api-reference/market-data/get-server-time)
- [거래 가능 상품](https://docs.kraken.com/api-reference/market-data/get-tradable-asset-pairs)
- [Ticker](https://docs.kraken.com/api-reference/market-data/get-ticker-information)
- [L2 호가](https://docs.kraken.com/api-reference/market-data/get-order-book)
- [최근 체결](https://docs.kraken.com/api-reference/market-data/get-recent-trades)
- [OHLC](https://docs.kraken.com/api-reference/market-data/get-ohlc-data)
- [주문 생성](https://docs.kraken.com/api-reference/trading/add-order)
- [주문 취소](https://docs.kraken.com/api-reference/trading/cancel-order)
- [Spot REST OpenAPI 명세](https://docs.kraken.com/openapi/spot-rest.yaml)
- [Spot API 요청 제한](https://support.kraken.com/articles/206548367-what-are-the-api-rate-limits-)
