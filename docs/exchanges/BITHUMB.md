# Bithumb Spot REST·WebSocket 어댑터

구현 기준은 빗썸 Developer Docs 2.1.5와 REST 기본 주소 `https://api.bithumb.com`입니다. 현재 빗썸 주문 API는 v1과 v2가 함께 사용되므로 SDK도 endpoint별 버전을 그대로 유지합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Bithumb 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

Secret Key는 Base64 디코딩하지 않고 HS256 서명 키로 직접 사용합니다. `credential.Descriptor.AccountID`에는 오류와 감사 로그에서 계정을 식별할 안정적인 값을 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 마켓 | `Markets` | `GET /v1/market/all` |
| 공개 시세 | `Tickers`, `OrderBooks`, `RecentTrades`, `MinuteCandles` | v1 |
| 계정 | `Accounts` | `GET /v1/accounts` |
| 주문 생성 | `PlaceOrder` | `POST /v2/orders` |
| 주문 상세 | `OrderInfo` | `GET /v1/order` |
| 주문 취소 | `CancelOrder` | `DELETE /v2/order` |
| 주문 목록 | `PendingOrders`, `OrderHistory` | v2 |

가격, 수량, 금액은 `float64`로 변환하지 않습니다. 공개 JSON 숫자는 `Decimal` 문자열로 보존하며 private 응답 문자열도 그대로 유지합니다. 각 결과의 `Raw`에는 해당 응답의 원본 JSON만 보존합니다.

호가 API는 깊이 파라미터를 받지 않습니다. 단일 마켓 조회는 최대 30호가, 복수 마켓 조회는 마켓별 최대 15호가를 거래소 응답 그대로 반환합니다.

## 추가 API와 필요 권한

아래 메서드도 모두 `*Client`에 있으며 같은 규칙을 따릅니다. 요청 구조체의 `validate()`가 전송 전에 입력을 검증하고, 응답의 `Raw`에 원본 JSON을 보존합니다. 필요한 권한이 `credential.Descriptor.Permissions`에 없으면 Secret 조회와 전송 전에 `trade.ErrAuthorization`(`credential.ErrPermissionNotAllowed`)으로 거부합니다. 상태를 바꾸는 요청(Mutation)은 자동 재시도하지 않으며 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, HTTP 5xx는 `trade.ErrUnknownExecutionState`로 반환합니다. 조회(Read)만 같은 route에서 제한적으로 재시도합니다.

### 시장 정보 (공개 API, 인증·권한 불필요)

| 메서드 | API | 비고 |
|---|---|---|
| `DayCandles` | `GET /v1/candles/days` | `ConvertingPriceUnit`에 `bithumb.ConvertingPriceUnitKRW`를 지정하면 `ConvertedTradePrice`에 원화 환산 종가를 담습니다. |
| `WeekCandles`, `MonthCandles` | `GET /v1/candles/weeks`, `GET /v1/candles/months` | `FirstDayOfPeriod`에 캔들 기간의 시작일이 담깁니다. |
| `VirtualAssetWarnings` | `GET /v1/market/virtual_asset_warning` | 경보제로 지정된 마켓의 유형(`WarningType`)·단계(`WarningStep`)·종료 일시입니다. |
| `Notices` | `GET /v1/notices` | `Count`는 0~20이며 0이면 거래소 기본값 5건입니다. 공식 제한이 IP별 1회/초라 별도 bucket을 씁니다. |
| `InOutFees` | `GET /v2/fee/inout/{currency}` | `FeeCurrencyAll`(`ALL`)을 주면 전체 자산을 반환합니다. 출금 수수료는 금액 기준 또는 정률 기준이며 해당하지 않는 필드는 빈 값입니다. |

일·주·월 캔들의 `Timestamp`는 기존 `MinuteCandles`와 같은 `int64`입니다. 경보제·공지사항·입출금 수수료 문서의 요청 예시에는 인증 헤더가 없어 공개 API로 호출합니다.

### 주문 (Read, Trade 권한)

| 메서드 | API | 권한 |
|---|---|---|
| `OrderChance` | `GET /v1/orders/chance` | Read |
| `SearchOrders` | `POST /v2/orders/search` | Read |
| `TWAPOrders` | `GET /v1/twap` | Read |
| `PlaceBatchOrders` | `POST /v2/orders/batch` | Trade |
| `CancelBatchOrders` | `POST /v2/orders/cancel` | Trade |
| `PlaceTWAPOrder` | `POST /v1/twap` | Trade |
| `CancelTWAPOrder` | `DELETE /v1/twap` | Trade |

- `SearchOrders`는 POST이지만 상태를 바꾸지 않는 조회라 Read 권한과 읽기 재시도 정책을 따릅니다. `OrderIDs`와 `ClientOrderIDs` 중 정확히 하나만 최대 100건까지 받으며, 거래소는 둘 다 받으면 `order_ids`를 우선하지만 SDK는 의도하지 않은 조회를 막기 위해 하나만 허용합니다. 매칭되지 않는 ID는 응답에서 빠지므로 결과가 요청보다 적을 수 있습니다.
- `PlaceBatchOrders`는 최대 30건이며 각 주문을 `PlaceOrderRequest`와 같은 규칙으로 검증합니다. 같은 요청 안의 `ClientOrderID` 중복은 거래소가 뒤의 주문을 실패 처리하므로 전송 전에 거부합니다. 거래소는 일부 주문이 실패해도 HTTP 200을 반환하므로 항목마다 `BatchOrderResult.Succeeded()`(`order_id` 존재 여부)를 확인해야 하며, 실패 항목에는 `ErrorName`과 `ErrorMessage`만 채워집니다.
- 다건 주문의 서명 해시 입력은 공식 예제대로 `batch_orders[0][market]=KRW-BTC&batch_orders[0][side]=bid&...&batch_orders[1][market]=...` 형식이며 JSON 본문은 `{"batch_orders":[{...}]}`입니다. 본문과 해시는 하나의 ordered parameter에서 만들어 항목 필드 순서가 항상 일치하고, 항목 필드 순서는 `PlaceOrder`와 같습니다.
- `CancelBatchOrders`도 `OrderIDs`와 `ClientOrderIDs` 중 정확히 하나만 최대 30건까지 받습니다. 성공 응답은 취소 접수일 뿐이므로 최종 상태는 `OrderInfo`나 `SearchOrders`로 다시 확인해야 합니다. `Fail` 항목에는 사유(`Reason`)가 담깁니다.
- 다건 주문 생성과 취소는 단건 주문과 분리된 `order-batch` bucket(20회/초)을 함께 사용합니다.
- `OrderHistory`와 `SearchOrders`가 반환하는 `OrderSummary`에는 `CancelType`(`stp_cancel`, `user_cancel`, `admin_cancel`, `tif_cancel`)과 `CancelingOrderID`가 포함됩니다.
- `PlaceTWAPOrder`의 `Duration`과 `Frequency`는 `time.Duration`으로 받아 초 단위 문자열로 전송합니다. `Duration`은 초 단위로 떨어지는 300~43200초, `Frequency`는 15·20·30·60·120초 중 하나여야 합니다. 매도(`ask`)는 `Volume`, 매수(`bid`)는 `Price`가 필수입니다. 문서가 매수 `price`를 단가인지 총액인지 구분해 설명하지 않으므로 SDK는 값을 변환하지 않고 그대로 전달합니다. `TWAPOrder.UUID`는 요청·취소 응답의 `AlgoOrderID`와 같고 `TWAPOrders`는 `next_key` cursor로 페이지를 넘깁니다.
- 공식 요청 제한 안내에는 TWAP 분류가 없어 SDK는 TWAP 요청을 `order-create`, 취소를 `order-cancel`, 조회를 `private-other` bucket에 넣는다고 가정합니다. 거래소가 별도 한도를 공지하면 조정해야 합니다.
- TWAP 문서의 Python 예제는 JWT를 HS512로 서명하지만 인증 문서와 나머지 API 예제는 HS256입니다. SDK는 모든 요청을 HS256으로 서명하므로 TWAP 호출에서 `jwt_verification` 오류가 나면 이 차이를 먼저 확인해야 합니다.

### 계정·지갑 (Read 권한)

| 메서드 | API | 권한 |
|---|---|---|
| `WalletStatuses` | `GET /v1/status/wallet` | Read |
| `APIKeys` | `GET /v1/api_keys` | Read |

`WalletStatuses`는 자산·네트워크별 지갑 상태(`WalletState`)와 블록 동기화 상태(`BlockState`)를 반환합니다. `APIKeys`는 API Key 식별자(`AccessKey`)와 만료 일시만 조회하며 Secret Key는 포함하지 않고 키를 만들거나 지우지 않으므로 Read 권한으로 호출합니다.

### 입금 (Read, Transfer 권한)

| 메서드 | API | 권한 |
|---|---|---|
| `Deposits` | `GET /v1/deposits` | Read |
| `KRWDeposits` | `GET /v1/deposits/krw` | Read |
| `DepositInfo` | `GET /v1/deposit` | Read |
| `DepositAddresses` | `GET /v1/deposits/coin_addresses` | Read |
| `DepositAddressInfo` | `GET /v1/deposits/coin_address` | Read |
| `GenerateDepositAddress` | `POST /v1/deposits/generate_coin_address` | Transfer |
| `DepositKRW` | `POST /v1/deposits/krw` | Transfer |

- 입금 주소 생성과 원화 입금 요청은 자산이 거래소 밖으로 나가지 않는 계정 변경이므로 `credential.PermissionTransfer`가 필요합니다. Withdraw 권한으로는 호출할 수 없습니다.
- 목록 조회의 `Page`는 1부터 시작하고 `Limit`은 최대 100이며 0이면 거래소 기본값을 씁니다. `UUIDs`와 `TxIDs`는 `uuids[]`, `txids[]` 반복 키로 전송하고 같은 순서로 서명합니다. `DepositInfo`는 `Currency`와 함께 `UUID`와 `TxID` 중 정확히 하나를 받습니다.
- 거래소가 입금 상태 전체를 enum으로 고정하지 않으므로 `State`는 대문자와 밑줄 형식만 검증하고 값은 거래소가 판단합니다. 코인 입금 상태는 `DepositState...` 상수, 원화 입금 상태는 `DepositStateKRWProcessing`, `DepositStateKRWAccepted`, `DepositStateKRWCancelled`입니다.
- `DepositKRW`의 `TwoFactorType`에는 공식 문서가 안내하는 `TwoFactorKakao`를 씁니다. 다른 값은 소문자 토큰 형식만 확인하고 거래소가 판단합니다.
- 입금 주소 생성과 원화 입금 요청은 전송 결과가 불명확하면 `DepositAddressInfo`나 `KRWDeposits`로 접수 여부를 확인한 뒤에만 다시 요청해야 합니다.

### 출금 (Read, Withdraw 권한)

| 메서드 | API | 권한 |
|---|---|---|
| `Withdrawals` | `GET /v1/withdraws` | Read |
| `KRWWithdrawals` | `GET /v1/withdraws/krw` | Read |
| `WithdrawalInfo` | `GET /v1/withdraw` | Read |
| `WithdrawChance` | `GET /v1/withdraws/chance` | Read |
| `WithdrawAddresses` | `GET /v1/withdraws/coin_addresses` | Read |
| `WithdrawCoin` | `POST /v1/withdraws/coin` | Withdraw |
| `CancelWithdrawCoin` | `DELETE /v1/withdraws/coin` | Withdraw |
| `WithdrawKRW` | `POST /v1/withdraws/krw` | Withdraw |

- 출금 요청과 취소는 자산이 거래소 밖으로 나가므로 명시적으로 `credential.PermissionWithdraw`를 부여한 Descriptor만 호출할 수 있습니다. Read, Trade, Transfer 권한으로는 거부됩니다.
- `WithdrawCoin`은 거래소에 등록된 출금 허용 주소(`WithdrawAddresses`)로만 요청해야 하며 `ReceiverType`이 `corporation`이면 `ReceiverCorpKoName`과 `ReceiverCorpEnName`이 필수입니다. 출금 요청은 HTTP 201로 응답하고 `Withdrawal.State`는 문서 예시처럼 `processing` 같은 소문자일 수 있어 SDK는 거래소 값을 정규화하지 않습니다.
- 출금 요청의 전송 결과가 불명확하면 `UNKNOWN_EXECUTION_STATE`이며 자동 재시도하지 않습니다. 중복 출금을 막으려면 `WithdrawalInfo`나 `Withdrawals`로 접수 여부를 확인한 뒤에만 다시 요청해야 합니다.
- 출금 대상 거래소의 트래블룰 개인정보 처리 동의가 필요하면 거래소가 HTTP 422와 `travel_rule_consent_required`를 반환합니다. SDK는 이를 검증 오류(`trade.ErrValidation`)로 분류하고 `errors.As(err, &consent)`로 `*bithumb.TravelRuleConsentError`(`ExchangeName`, `ConsentURL`)를 꺼낼 수 있게 합니다. 동의 URL은 로그에 남지 않도록 `Error()` 문자열에 넣지 않으며 동의를 마친 뒤에 출금을 다시 요청해야 합니다.
- 출금 목록의 `State`는 `PROCESSING`, `DONE`, `CANCELLED`(`WithdrawalState...` 상수)만 허용합니다. `CancelWithdrawCoin`은 처리 중인 출금만 취소할 수 있습니다.
- `WithdrawKRW`의 `TwoFactorType`은 `DepositKRW`와 같은 규칙입니다.

## JWT와 쿼리 해시

인증 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 기준으로 서명합니다.

1. 요청마다 UUID v4 nonce를 생성합니다.
2. 현재 Unix millisecond timestamp를 기록합니다.
3. 파라미터가 있으면 URL 인코딩 전 `key=value` 문자열을 순서대로 연결하여 SHA-512 hex 해시를 만듭니다.
4. `access_key`, `nonce`, `timestamp`, `query_hash`, `query_hash_alg=SHA512` payload를 HS256으로 서명합니다.
5. `Authorization: Bearer <JWT>` 헤더로 전송합니다.

POST 주문의 JSON 필드와 해시 입력은 하나의 ordered parameter에서 만듭니다. `states[]` 같은 반복 필드는 순서를 유지하고 `next_key`처럼 `+`, `/`, `=`가 들어갈 수 있는 값은 HTTP URL에서만 percent encoding합니다.

`NonceSource`와 `Now`는 결정적인 테스트를 위한 주입 지점입니다. 운영에서는 기본 암호학적 난수 nonce와 시스템 시계를 사용해야 합니다.

## 요청 제한과 송신 경로

SDK 기본 로컬 제한은 공식 안내의 공개 분류별 150회/초, private 분류별 140회/초입니다. 제한은 IP 기준이므로 선택한 송신 route별로 관리합니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `bithumb:route:<route>:public-<category>:1second` | 150회/초 | 공개 캔들·호가·현재가·체결·기타 분류별 |
| `bithumb:route:<route>:private-other:1second` | 140회/초 | 자산·주문 조회 등 private 기타 |
| `bithumb:route:<route>:order-create:1second` | 140회/초 | 단건 주문 요청 |
| `bithumb:route:<route>:order-cancel:1second` | 140회/초 | 단건 주문 취소 접수 |
| `bithumb:route:<route>:public-notice:1second` | 1회/초 | 공지사항 조회 |
| `bithumb:route:<route>:order-batch:1second` | 20회/초 | 다건 주문 생성·취소 |

각 요청은 공식 API 분류에 대응하는 bucket 하나를 소비합니다. 제한값은 `Config`의 `PublicRequestsPerSecond`, `PrivateRequestsPerSecond`, `OrderRequestsPerSecond`로 더 보수적으로 조정할 수 있습니다. 공지사항 조회(공식 IP별 1회/초)와 다건 주문 생성·취소(공식 20회/초, 둘이 한 bucket 공유)는 별도 한도이므로 `NoticeRequestsPerSecond`, `BatchOrderRequestsPerSecond`로 따로 조정하며 다른 bucket과 섞지 않습니다. 두 값은 0이면 공식 기본값을 쓰고 음수이면 `New`가 거부합니다. 입금·출금·API Key 조회는 공식 분류상 private 기타에 속해 `private-other` bucket을 씁니다.

각 route에는 실제 공인 송신 IP가 정확히 연결되어 있어야 하며 SDK bucket도 그 route를 기준으로 분리됩니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능입니다.

## 주문 안전 계약

- 지정가 `limit`, 매수 시장가 `price`, 매도 시장가 `market`, KRW 마켓 최유리 `best`를 지원합니다.
- `best` 매수는 총액 `Price`, `best` 매도는 수량 `Volume`, 그리고 `ioc` 또는 `fok`가 필요합니다.
- `ClientOrderID`는 영문자, 숫자, `_`, `-`만 사용하며 길이는 1~36자입니다.
- 현재 v2 생성 응답의 `stp_type`을 보존합니다. 빗썸의 현재 STP 동작은 거래소가 정한 `cancel_taker`입니다.
- 빗썸은 주문 식별자를 둘 다 받으면 하나를 우선하지만, SDK는 잘못된 주문을 조회·취소하지 않도록 정확히 하나만 받습니다.
- 계정 전체 목록 조회는 의도하지 않은 범위 확장을 막기 위해 `AllMarkets: true`를 명시해야 합니다.
- 주문 이력에서 시작과 종료를 함께 지정하면 최대 7일 범위를 검증하며, 시간은 Unix millisecond로 전송합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, HTTP 5xx는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 고유한 `ClientOrderID`를 사용하고 불명확한 결과는 `OrderInfo`로 확인해야 합니다.

## WebSocket

`StreamClient`는 현재 공식 분리 규칙에 따라 public v1과 private v2 endpoint를 사용합니다.

| 연결 | Endpoint | 스트림 |
|---|---|---|
| Public v1 | `wss://ws-api.bithumb.com/websocket/v1` | `ticker`, `trade`, `orderbook` |
| Private v2 | `wss://ws-api.bithumb.com/websocket/v2/private` | `myOrder`, `myAsset` |

```go
streams, err := bithumb.NewStreamClient(bithumb.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streams.PublicStream(
	bithumb.StreamRequest{
		Types: []bithumb.StreamDataType{
			{Type: "ticker", Codes: []string{"KRW-BTC"}, OnlyRealtime: true},
			{Type: "orderbook", Codes: []string{"KRW-BTC"}, Level: "1000"},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()
```

public DEFAULT 응답은 `StreamTicker`, `StreamTrade`, `StreamOrderBook`으로 decode할 수 있습니다. private v2 DEFAULT 응답은 의미가 분리된 `MyOrderEvent`의 `order_*`, `trade_*`, 누적 체결·수수료·취소 필드와 `MyAssetEvent`의 자산 목록으로 제공합니다. `SIMPLE` 형식도 envelope를 분류하지만 축약 필드는 `StreamMessage.Payload`에서 사용자 구조체로 decode해야 합니다.

private 연결은 REST와 같은 HS256 JWT를 `Authorization` handshake 헤더에 넣되 query hash는 포함하지 않습니다. 모든 연결과 재연결은 세션에서 선택한 송신 경로에 고정되며, 재연결마다 다음 작업을 새로 수행합니다.

1. Secret Provider에서 Access Key와 Secret Key 조회
2. 새 nonce와 Unix millisecond timestamp로 JWT 생성
3. private v2 endpoint 연결
4. 새 ticket을 포함한 현재 구독 요청 전송
5. 사용한 자격증명 byte slice 파기

고정 ticket을 직접 지정하지 않으면 public/private 모두 재연결마다 새 ticket을 생성합니다. private 요청은 생성 시 read 권한과 route 허용 목록을 Secret 조회보다 먼저 검사합니다. `myOrder`는 `Codes`를 생략해 전체 마켓을 구독할 수 있고 `myAsset`은 codes를 받지 않습니다.

서버 idle timeout은 약 120초입니다. SDK 기본값은 30초마다 WebSocket PING frame을 보내고 10초 안에 PONG을 받지 못하면 같은 route로 재연결합니다. 공식 연결 제한은 public/private 공통 IP당 초당 10회이며, 반복 초과 시 10분간 차단될 수 있습니다. 한 프로세스에 여러 세션이 있으면 연결·재연결 합계를 운영 메트릭으로 감시해야 합니다.

서버의 `permessage-deflate`는 선택 기능입니다. 현재 공통 connector는 압축을 요청하지 않으며 서버는 같은 JSON을 비압축으로 전송합니다. Private v1은 2026년 9월 30일 종료 예정이므로 SDK는 deprecated endpoint를 제공하지 않습니다.

### Spot 로컬 오더북 snapshot

빗썸 public v1 `orderbook`은 `SNAPSHOT`과 `REALTIME` 모두 최대 15개의 현재 매도·매수 호가 쌍을 제공합니다. 공식 응답에는 sequence가 없으므로 REST snapshot이나 임의의 연속 번호를 결합하지 않습니다. `LocalOrderBook`은 각 메시지의 마켓·microsecond timestamp·모아보기 `level`·정렬·가격·수량을 독립적으로 검증하고 최신 snapshot view로 전달합니다.

```go
public, err := streams.PublicStream(
	bithumb.StreamRequest{
		Types: []bithumb.StreamDataType{{
			Type:  "orderbook",
			Codes: []string{"KRW-BTC"},
			Level: "1000",
		}},
		Format: bithumb.StreamFormatDefault,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := bithumb.NewLocalOrderBook(bithumb.LocalOrderBookConfig{
	Market:        "KRW-BTC",
	EgressRouteID: "seoul-b",
	ViewDepth:     15,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view bithumb.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- 로컬 오더북과 WebSocket의 `EgressRouteID`가 다르면 연결 전에 거부합니다.
- typed 구조체가 전체 필드명을 사용하므로 `DEFAULT` 응답 형식만 허용합니다.
- 각 이벤트를 완전한 snapshot으로 취급해 이전 가격 단계를 병합하지 않고 통째로 교체합니다.
- 모아보기 `level` 기본값은 1이며 수신한 실제 값을 `LocalOrderBookView.Level`에 보존합니다.
- 네트워크 재연결 시 같은 송신 경로와 새 ticket으로 다시 구독하며 다음 snapshot부터 `Generation`이 증가한 view를 제공합니다.
- sequence가 없으므로 탐지할 수 없는 gap count를 만들지 않습니다. `SnapshotID`, `Generation`, `Timestamp`, `StreamType`으로 수신과 재연결을 관측합니다.

## 공통 Spot API

`NewUnifiedSpot`은 native 클라이언트를 `unified.SpotClient`로 변환합니다. 공통 `BTC/KRW` 마켓은 Bithumb의 `KRW-BTC`로 변환하며, 시장가 매수의 `QuoteAmount`는 `order_type=price`의 `price`, 시장가 매도의 `Quantity`는 `order_type=market`의 `volume`으로 전송합니다.

미체결 주문은 v2 `next_key` cursor가 끝날 때까지 조회하며 모든 페이지에 같은 요청별 송신 경로를 전달합니다. Bithumb v1 주문 상세의 `uuid`와 v2 주문 생성·목록·취소의 `order_id` 차이는 공통 `Order.ID`로 정규화합니다.

## 공식 기준 문서

- [Bithumb Developer Docs](https://apidocs.bithumb.com/docs/빗썸-developer-docs)
- [인증 토큰 생성](https://apidocs.bithumb.com/docs/인증-토큰-생성하기)
- [API 요청 수 제한](https://apidocs.bithumb.com/docs/api-요청-수-제한-안내)
- [주문 요청](https://apidocs.bithumb.com/reference/주문-요청)
- [개별 주문 조회](https://apidocs.bithumb.com/reference/개별-주문-조회)
- [주문 취소 접수](https://apidocs.bithumb.com/reference/주문-취소-접수)
- [대기 주문 목록 조회](https://apidocs.bithumb.com/reference/대기-주문-목록-조회)
- [종료 주문 목록 조회](https://apidocs.bithumb.com/reference/종료-주문-목록-조회)
- [일(Day) 캔들 조회](https://apidocs.bithumb.com/reference/일day-캔들-조회)
- [주(Week) 캔들 조회](https://apidocs.bithumb.com/reference/주week-캔들-조회)
- [월(Month) 캔들 조회](https://apidocs.bithumb.com/reference/월month-캔들-조회)
- [경보제 조회](https://apidocs.bithumb.com/reference/경보제-조회)
- [공지사항 조회](https://apidocs.bithumb.com/reference/공지사항-조회)
- [입출금 수수료 조회](https://apidocs.bithumb.com/reference/입출금-수수료-조회)
- [주문 가능 정보 조회](https://apidocs.bithumb.com/reference/주문-가능-정보)
- [다건 주문 조회](https://apidocs.bithumb.com/reference/다건-주문-조회)
- [다건 주문 요청](https://apidocs.bithumb.com/reference/다건-주문-요청)
- [다건 주문 취소 접수](https://apidocs.bithumb.com/reference/다건-주문-취소-접수)
- [TWAP - 주문 요청](https://apidocs.bithumb.com/reference/twap-주문-요청)
- [TWAP - 주문 내역 조회](https://apidocs.bithumb.com/reference/twap-주문내역-조회)
- [TWAP - 주문 취소](https://apidocs.bithumb.com/reference/twap-주문-취소)
- [입출금 현황](https://apidocs.bithumb.com/reference/입출금-현황)
- [API 키 리스트 조회](https://apidocs.bithumb.com/reference/api-키-리스트-조회)
- [코인 입금 리스트 조회](https://apidocs.bithumb.com/reference/입금-리스트-조회)
- [원화 입금 리스트 조회](https://apidocs.bithumb.com/reference/원화-입금-리스트-조회)
- [개별 입금 조회](https://apidocs.bithumb.com/reference/개별-입금-조회)
- [입금 주소 생성 요청](https://apidocs.bithumb.com/reference/입금-주소-생성-요청)
- [전체 입금 주소 조회](https://apidocs.bithumb.com/reference/전체-입금-주소-조회)
- [개별 입금 주소 조회](https://apidocs.bithumb.com/reference/개별-입금-주소-조회)
- [원화 입금](https://apidocs.bithumb.com/reference/원화-입금)
- [코인 출금 리스트 조회](https://apidocs.bithumb.com/reference/출금-리스트-조회)
- [원화 출금 리스트 조회](https://apidocs.bithumb.com/reference/원화-출금-리스트-조회)
- [개별 출금 조회](https://apidocs.bithumb.com/reference/개별-출금-조회)
- [출금 가능 정보](https://apidocs.bithumb.com/reference/출금-가능-정보)
- [가상 자산 출금 요청](https://apidocs.bithumb.com/reference/가상-자산-출금-요청)
- [가상 자산 출금 취소](https://apidocs.bithumb.com/reference/가상-자산-출금-취소)
- [원화 출금 요청](https://apidocs.bithumb.com/reference/원화-출금-요청)
- [출금 허용 주소 리스트 조회](https://apidocs.bithumb.com/reference/출금-허용-주소-리스트-조회)
- [WebSocket 기본 정보](https://apidocs.bithumb.com/reference/기본-정보)
- [WebSocket 호가](https://apidocs.bithumb.com/reference/호가-orderbook)
- [WebSocket 연결 관리](https://apidocs.bithumb.com/reference/연결-관리)
- [Private v2 MyOrder](https://apidocs.bithumb.com/reference/내-주문-및-체결-myorder)
- [Private v2 MyAsset](https://apidocs.bithumb.com/reference/내-자산-myasset)
- [Private v2 마이그레이션](https://apidocs.bithumb.com/reference/private-v2-마이그레이션-가이드)
