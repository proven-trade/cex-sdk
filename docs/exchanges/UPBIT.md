# Upbit Spot 어댑터

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Upbit 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

Secret Key는 Base64 디코딩하지 않고 HS512 서명 키로 직접 사용합니다. `credential.Descriptor.AccountID`에는 개별 Access Key가 아니라 요청 제한을 공유하는 업비트 계정 pocket 식별자를 넣어야 합니다. 같은 pocket의 여러 키가 같은 요청 제한을 공유하기 때문입니다.

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 |
|---|---|
| 마켓 | `Markets` |
| 공개 시세 | `Tickers`, `OrderBooks`, `RecentTrades`, `MinuteCandles` |
| 계정 | `Accounts` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `ClosedOrders` |

주문 생성은 `limit`, 매수 시장가 `price`, 매도 시장가 `market`, 최유리 지정가 `best`를 지원합니다. `best` 매수는 총액 `Price`, 매도는 수량 `Volume`, 그리고 `ioc` 또는 `fok`가 필요합니다. 주문 가능 정보, 일괄 취소, 입출금, 포켓 간 자산 이전 같은 나머지 REST API와 추가 WebSocket 채널은 아래 "추가 API와 필요 권한"에 정리했습니다.

가격, 수량, 금액은 `float64`로 변환하지 않습니다. private 응답은 거래소가 제공한 문자열을 유지하고 공개 시세의 JSON 숫자는 `Decimal` 문자열 타입으로 손실 없이 보존합니다.

## 추가 API와 필요 권한

아래 메서드도 모두 `*Client`에 있으며 같은 규칙을 따릅니다. 요청 구조체의 `validate()`가 전송 전에 입력을 검증하고, 응답의 `Raw`에 원본 JSON을 보존합니다. 금액과 수량은 문자열로 받아 그대로 전송하고 응답은 `Decimal`로 보존합니다. SDK 권한(`credential.Descriptor.Permissions`)과 송신 경로 허용 목록은 Secret 조회와 전송 전에 검사하며, 필요한 권한이 없으면 `trade.ErrAuthorization`으로 거부합니다. 상태를 바꾸는 요청(Mutation)은 자동 재시도하지 않고 전송 오류, 응답 본문 읽기 실패, HTTP 5xx를 `trade.ErrUnknownExecutionState`로 반환합니다. 조회(Read)만 같은 route에서 제한적으로 재시도합니다.

표의 "SDK 권한"은 `credential.Permission` 값이고 "업비트 API Key 권한"은 API Key 발급 화면의 권한 이름입니다. SDK 권한을 부여해도 업비트 API Key에 해당 권한이 없으면 거래소가 `out_of_scope`로 거부하며 SDK는 이를 `trade.ErrAuthorization`으로 분류합니다. 포켓 API는 API Key를 발급한 포켓(메인포켓 또는 서브포켓)도 맞아야 합니다. 요청 그룹이 비어 있으면 `default`(30회/초)입니다.

### 시세 (공개 API, 인증·권한 불필요)

| 메서드 | API | 요청 그룹 |
|---|---|---|
| `SecondCandles` | `GET /v1/candles/seconds` | `candle` |
| `DayCandles` | `GET /v1/candles/days` | `candle` |
| `WeekCandles`, `MonthCandles`, `YearCandles` | `GET /v1/candles/weeks`, `GET /v1/candles/months`, `GET /v1/candles/years` | `candle` |
| `QuoteTickers` | `GET /v1/ticker/all` | `ticker` |
| `OrderBookInstruments` | `GET /v1/orderbook/instruments` | `orderbook` |

- 캔들 `Count`는 0(거래소 기본값)이거나 1~200이고, `To`는 조회 종료 시각(ISO 8601)입니다. 초 캔들은 요청 시점 기준 최대 3개월 이전 데이터만 제공하며 응답에 `unit`이 없어 `Candle.Unit`이 0입니다.
- `DayCandles`의 `ConvertingPriceUnit`에 통화 코드(예: `KRW`)를 지정하면 `ConvertedTradePrice`에 환산 종가가 담깁니다. 주·월·연 캔들(`PeriodCandle`)은 집계 기간의 시작일 `FirstDayOfPeriod`를 함께 반환합니다.
- `QuoteTickers`는 마켓(호가 자산) 통화 코드 목록(`KRW`, `BTC`, `USDT`)에 속한 모든 페어의 현재가를 반환합니다. `OrderBookInstruments`는 페어별 호가 단위 `TickSize`와 `SupportedLevels`를 반환하며, `0`은 기본 호가 단위이고 호가 모아보기는 원화 마켓에서만 지원합니다.

### 주문 (Read, Trade 권한)

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 | 요청 그룹 |
|---|---|---|---|---|
| `OrderChance` | `GET /v1/orders/chance` | Read | 주문조회 | |
| `OrdersByIDs` | `GET /v1/orders/uuids` | Read | 주문조회 | |
| `TestOrder` | `POST /v1/orders/test` | Trade | 주문하기 | `order-test` |
| `CancelOrdersByIDs` | `DELETE /v1/orders/uuids` | Trade | 주문하기 | |
| `CancelOpenOrders` | `DELETE /v1/orders/open` | Trade | 주문하기 | `order-cancel-all` |
| `CancelAndNewOrder` | `POST /v1/orders/cancel_and_new` | Trade | 주문하기 | `order` |

- `OrderChance`는 페어의 수수료율, 주문 정책, 호가·기준 자산 잔고를 반환합니다. 더 이상 쓰이지 않는 `order_types`, `price_unit`은 `Raw`에만 남습니다.
- `OrdersByIDs`는 `UUIDs`와 `Identifiers` 중 하나만 최대 100개까지 받고, `CancelOrdersByIDs`는 하나만 최대 20개까지 받습니다. 취소는 접수일 뿐이며 일부만 실패할 수 있으므로 `BatchCancelResult.Failed`를 확인하고 최종 상태는 `OrderInfo`로 다시 조회해야 합니다.
- `CancelOpenOrders`는 체결 대기(`wait`) 주문을 최대 300개까지 일괄 취소 접수하며 예약(`watch`) 주문은 대상이 아닙니다. `Pairs`와 `QuoteCurrencies`는 함께 쓸 수 없고, 둘 다 비우면 전체 마켓이 대상이 되므로 실수로 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다. `ExcludedPairs`는 어느 경우에나 쓸 수 있으며 `Pairs`, `ExcludedPairs`는 각각 최대 20개입니다. 요청 그룹 한도가 2초당 1회입니다.
- `TestOrder`는 `PlaceOrder`와 같은 요청을 실제 주문 없이 검증하며 체결되지 않습니다.
- `CancelAndNewOrder`는 `PrevOrderUUID`와 `PrevOrderIdentifier` 중 하나로 기존 주문을 취소하고 같은 페어·같은 방향의 신규 주문을 만듭니다. 취소 전에 기존 주문이 전량 체결되면 신규 주문은 만들어지지 않고 `NewOrderUUID`가 비어 있습니다. 전송 결과가 불명확하면 `OrderInfo`로 두 주문의 상태를 확인한 뒤에만 다시 요청해야 합니다.

### 계정 (Read 권한)

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 |
|---|---|---|---|
| `WalletStatuses` | `GET /v1/status/wallet` | Read | 불필요(인증만 필요) |
| `APIKeys` | `GET /v1/api_keys` | Read | 불필요(인증만 필요) |

`WalletStatuses`는 통화·네트워크별 입출금 서비스 상태(`WalletState`)와 블록 정보를 반환합니다. 실시간 상태를 보장하지 않고 수 분 늦게 반영될 수 있으므로 거래 전략이 아닌 참고용으로만 써야 합니다. `APIKeys`는 Access Key와 만료 일시만 반환하며 Secret Key는 포함하지 않고 키를 만들거나 지우지 않으므로 Read 권한으로 호출합니다.

### 입금 (Read, Transfer 권한)

업비트 입금하기 권한은 자산이 거래소 밖으로 나가지 않는 계정 변경이므로 SDK에서는 `credential.PermissionTransfer`로 매핑합니다. `credential.PermissionWithdraw`는 출금 API에만 필요합니다.

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 |
|---|---|---|---|
| `DepositChance` | `GET /v1/deposits/chance/coin` | Read | 입금조회 |
| `DepositAddress` | `GET /v1/deposits/coin_address` | Read | 입금조회 |
| `DepositAddresses` | `GET /v1/deposits/coin_addresses` | Read | 입금조회 |
| `DepositInfo` | `GET /v1/deposit` | Read | 입금조회 |
| `Deposits` | `GET /v1/deposits` | Read | 입금조회 |
| `GenerateDepositAddress` | `POST /v1/deposits/generate_coin_address` | Transfer | 입금하기 |
| `DepositKRW` | `POST /v1/deposits/krw` | Transfer | 입금하기 |

- `DepositChance`, `DepositAddress`, `GenerateDepositAddress`는 `Currency`와 네트워크 식별자 `NetType`을 모두 필수로 받습니다. `DepositChance`의 서비스 상태는 수 분 늦게 반영될 수 있으므로 거래 전략이 아닌 참고용으로만 써야 합니다. `IsDepositPossible`이 `false`이면 `DepositImpossibleReason`에 이유가 담깁니다.
- 주소 생성은 비동기입니다. `GenerateDepositAddressResult.Pending()`이 `true`이면 접수만 된 것이므로 잠시 뒤 `DepositAddress`로 다시 조회해야 하며, 생성 전 조회에서는 `Address`가 빈 문자열입니다. 이미 생성된 주소가 있으면 최초 생성된 주소를 그대로 반환합니다. `SecondaryAddress`는 Destination Tag, Memo처럼 2차 주소가 필요한 자산에서만 채워집니다. 자동 재시도하지 않으므로 결과가 불명확하면(`trade.ErrUnknownExecutionState`) 먼저 `DepositAddress`나 `DepositAddresses`로 확인합니다.
- `DepositKRW`는 입출금 계좌에서 원화를 입금하며 `TwoFactorType`(`kakao`, `naver`, `hana`) 2차 인증 앱의 승인이 필요합니다. 원화 입금은 `Deposit.NetType`이 비어 있고 완료 전에는 `DoneAt`도 비어 있습니다.
- `DepositInfo`는 `UUID`, `TxID`, `Currency`를 모두 비우면 최신 입금을 반환하며, 대상이 없으면 HTTP 404 `deposit_not_found`가 `trade.APIError`로 반환됩니다(`ExchangeCode`로 구분). `Deposits`의 `Limit`은 0(기본값 100) 또는 1~100이고 `OrderBy`는 `asc`나 `desc`이며 `From`, `To`는 응답의 `uuid`를 쓰는 페이지 이동 커서입니다. 입금 상태 `DepositState`에는 `PROCESSING`, `ACCEPTED`, `CANCELLED`, `REJECTED`, `TRAVEL_RULE_SUSPECTED`, `REFUNDING`, `REFUNDED`가 있습니다.

### 트래블룰 (Read, Transfer 권한)

트래블룰 검증은 `TRAVEL_RULE_SUSPECTED` 상태로 대기 중인 입금의 계정주를 확인해 입금 반영을 재개하는 요청입니다.

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 |
|---|---|---|---|
| `TravelRuleVASPs` | `GET /v1/travel_rule/vasps` | Read | 입금조회 |
| `VerifyTravelRuleByUUID` | `POST /v1/travel_rule/deposit/uuid` | Transfer | 입금하기 |
| `VerifyTravelRuleByTxID` | `POST /v1/travel_rule/deposit/txid` | Transfer | 입금하기 |

- `TravelRuleVASPs`로 계정주 확인을 지원하는 거래소(VASP) 목록을 받고, 자산을 출금한 상대 거래소의 `VASPUUID`를 골라 입금 UUID 또는 TxID로 검증을 요청합니다. `Depositable`은 해당 거래소에서 업비트로 온 입금을 반영할 수 있는지, `Withdrawable`은 해당 거래소로 출금할 수 있는지 나타냅니다.
- `VerifyTravelRuleByUUID`는 `DepositUUID`와 `VASPUUID`를, `VerifyTravelRuleByTxID`는 `VASPUUID`, `TxID`, `Currency`, `NetType`을 받습니다. 같은 입금 건은 10분에 한 번만 요청할 수 있으며 두 요청 모두 자동 재시도하지 않습니다. 결과의 `VerificationResult`가 `verified`이면 검증을 통과한 것이고 `DepositState`로 반영 상태를 확인합니다.

### 출금 (Read, Withdraw 권한)

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 |
|---|---|---|---|
| `WithdrawChance` | `GET /v1/withdraws/chance` | Read | 출금조회 |
| `WithdrawalAddresses` | `GET /v1/withdraws/coin_addresses` | Read | 출금조회 |
| `WithdrawalInfo` | `GET /v1/withdraw` | Read | 출금조회 |
| `Withdrawals` | `GET /v1/withdraws` | Read | 출금조회 |
| `WithdrawCoin` | `POST /v1/withdraws/coin` | Withdraw | 출금하기 |
| `WithdrawKRW` | `POST /v1/withdraws/krw` | Withdraw | 출금하기 |
| `CancelWithdrawal` | `DELETE /v1/withdraws/coin` | Withdraw | 출금하기 |

- 출금 API는 `credential.PermissionWithdraw`가 명시적으로 부여된 `Descriptor`만 호출할 수 있고, 자산이 거래소 밖으로 나가므로 자동 재시도하지 않습니다. 전송 결과가 불명확하면(`trade.ErrUnknownExecutionState`) 다시 요청하기 전에 `Withdrawals`나 `WithdrawalInfo`로 출금이 접수됐는지 확인해야 합니다.
- `WithdrawCoin`의 `Address`는 출금 허용 주소로 미리 등록된 주소여야 하고 `NetType`은 그 주소의 `net_type`을 써야 하며 `WithdrawalAddresses`로 확인합니다. Destination Tag, Memo가 필요한 자산은 `SecondaryAddress`를 반드시 지정합니다. `TransactionType`을 비우면 일반 출금(`default`)이고 `internal`은 업비트 계정 간 바로 출금입니다.
- `WithdrawKRW`는 `TwoFactorType`(`kakao`, `naver`, `hana`) 2차 인증 앱 승인이 필요합니다. 원화 출금 응답의 `State`는 소문자로 반환될 수 있습니다.
- `WithdrawChance`는 디지털 자산이면 `NetType`이 필수이며 출금 수수료, 지갑 상태, 잔고, 출금 한도를 반환합니다. `WithdrawLimit`의 `Onetime`, `Daily`, `RemainingDaily`, `RemainingDailyKRW`는 업비트가 더 이상 쓰지 않는 필드입니다.
- `WithdrawalInfo`는 `UUID`, `TxID`, `Currency`를 모두 비우면 최신 출금을 반환합니다. `Withdrawals`는 `UUIDs`와 `TxIDs`를 함께 쓸 수 없고 각각 최대 100개이며, `Limit`은 0(기본값 100) 또는 1~100입니다. `CancelWithdrawal`은 `Withdrawal.IsCancelable`이 `true`인 디지털 자산 출금만 취소할 수 있습니다.

### 포켓 (Read, Transfer 권한)

포켓은 메인포켓과 사용자가 만든 서브포켓으로 구분되며, API Key를 발급한 포켓에 따라 쓸 수 있는 API가 다릅니다.

| 메서드 | API | SDK 권한 | 업비트 API Key 권한 | API Key 발급 포켓 |
|---|---|---|---|---|
| `Pockets` | `GET /v1/pockets` | Read | 포켓관리 | 메인포켓 |
| `PocketAPIKeys` | `GET /v1/pockets/api_keys` | Read | 포켓관리 | 메인포켓 |
| `PocketAssets` | `GET /v1/pockets/assets` | Read | 포켓관리 | 메인포켓 |
| `CreateUniversalTransfer` | `POST /v1/pockets/universal_transfers` | Transfer | 포켓관리 | 메인포켓 |
| `UniversalTransfers` | `GET /v1/pockets/universal_transfers` | Read | 포켓관리 | 메인포켓 |
| `CreatePocketTransfer` | `POST /v1/pockets/transfers` | Transfer | 자산이전 | 서브포켓 |
| `PocketTransfers` | `GET /v1/pockets/transfers` | Read | 자산이전 | 서브포켓 |

- `Pockets`는 메인포켓과 서브포켓의 UUID, 이름, 종류(`main`, `user_spot_trading`)를 반환합니다. `PocketAPIKeys`는 `UUIDs`로 포켓을 좁히고 `IncludeExpired`로 만료된 키를 포함하며 응답은 Access Key, 부여된 권한 이름 목록, 허용 IP, 생성·만료 일시이며 Secret Key는 포함하지 않습니다. `PocketAssets`는 서브포켓 한 곳의 잔고를 반환하며 없는 포켓이면 HTTP 404가 `trade.APIError`로 반환됩니다.
- `CreateUniversalTransfer`는 메인포켓 API Key로 메인포켓과 서브포켓 사이 자산을 옮깁니다. `From`을 비우면 API Key가 속한 포켓이 보내는 포켓이고 `To`는 필수입니다. `CreatePocketTransfer`는 서브포켓 API Key로 자산을 옮기며 `To`를 비우면 메인포켓으로 이전합니다. 두 요청 모두 `Currency`, `Amount`는 필수이고 `Identifier`는 1~64자의 영문자, 숫자, `_`, `.`, `-`만 허용하며, 자동 재시도하지 않으므로 결과가 불명확하면 `Identifier`로 이전 내역을 조회해 확인합니다.
- `UniversalTransfers`, `PocketTransfers`는 `States`, `UUIDs`, `Identifiers`(각 최대 20개), `StartTime`, `EndTime`, `Currency`, `Limit`(0은 기본값 20, 최대 100), `OrderBy`로 조회합니다. `StartTime`과 `EndTime`을 함께 지정하면 범위가 7일 이하여야 하며 `EndTime`을 비우면 현재 시각입니다. `PocketTransfers`는 `Direction`(`in`, `out`, `all`)으로 서브포켓 기준 방향을 고르고 `UniversalTransfers`는 `From`, `To` 포켓 UUID로 좁힙니다. 이전 상태는 `submitted`, `processing`, `done`, `failed`입니다.

## JWT와 쿼리 해시

인증 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 기준으로 서명합니다.

1. 요청마다 UUID v4 nonce를 생성합니다.
2. 파라미터가 있으면 URL 인코딩 전 쿼리 문자열을 SHA-512 해시합니다.
3. `access_key`, `nonce`, `query_hash`, `query_hash_alg=SHA512`를 HS512 JWT payload로 만듭니다.
4. `Authorization: Bearer <JWT>` 헤더로 전송합니다.

반복 파라미터는 `states[]=wait&states[]=watch`처럼 순서와 키를 그대로 유지합니다. POST 주문은 JSON 본문의 필드 순서와 쿼리 해시 입력 순서를 같은 ordered parameter에서 생성합니다.

`NonceSource`는 테스트를 위한 주입 지점입니다. 운영에서는 기본 암호학적 난수 UUID 생성기를 사용해야 하며, 고정 nonce를 설정하면 안 됩니다.

## 요청 제한

SDK는 공개 API를 송신 경로별 IP bucket으로, private API를 `AccountID`별 pocket bucket으로 관리합니다.

| 그룹 | 로컬 제한 | 범위 |
|---|---:|---|
| `market`, `ticker`, `orderbook`, `trade`, `candle` | 각 8회/초(기본값, 거래소 한도 10회/초) | IP route |
| `default` | 30회/초 | 계정 pocket |
| `order` | 12회/초 | 계정 pocket |
| `order-test` | 8회/초 | 계정 pocket |
| `order-cancel-all` | 2초당 1회 | 계정 pocket |

`order`에는 `PlaceOrder`와 `CancelAndNewOrder`, `order-test`에는 `TestOrder`, `order-cancel-all`에는 `CancelOpenOrders`가 속하고, 나머지 private 요청(주문 조회·취소, 계정, 입출금, 포켓)은 `default`를 씁니다. 시세 그룹은 거래소 한도와 같은 10회/초로 두면 창 경계 지터 때문에 지속 부하에서 429가 발생하므로 기본 8회/초로 여유를 둡니다. `Config.PublicRequestsPerSecond`로 1~10 사이에서 조정할 수 있습니다. `Remaining-Req`의 남은 요청 수는 SDK 한도 기준 사용량으로 환산하므로 한도를 낮춰도 여유가 유지됩니다. 2026-08-21 적용된 주문 그룹 상향값 `12회/초`를 반영했습니다. 응답의 `Remaining-Req` 중 `group`과 `sec` 값을 로컬 limiter에 반영합니다. HTTP 429와 418은 `Retry-After`가 있으면 그 기간, 없으면 최소 1초 동안 관련 bucket을 차단합니다.

여러 공인 송신 IP를 사용해도 pocket 단위 제한은 증가하지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 안전한 주문 실패

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, HTTP 5xx는 실제 체결 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다.

가능하면 고유한 `Identifier`를 주문에 넣고, 불명확한 결과가 나오면 `OrderInfo`로 최종 상태를 확인해야 합니다. 주문 목록을 전체 마켓으로 조회하려면 실수로 조회 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다.

## WebSocket

업비트 WebSocket endpoint는 다음과 같습니다.

| 연결 | Endpoint | 제한 측정 단위 |
|---|---|---|
| public 시세 | `wss://api.upbit.com/websocket/v1` | IP |
| private Exchange | `wss://api.upbit.com/websocket/v1/private` | 계정 pocket |

`StreamClient`의 모든 연결과 재연결은 세션 생성 시 선택한 송신 경로에 고정됩니다.

```go
streams, err := upbit.NewStreamClient(upbit.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streams.PublicStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{
			{Type: "ticker", Codes: []string{"KRW-BTC"}},
			{Type: "trade", Codes: []string{"KRW-BTC"}, OnlyRealtime: true},
		},
		Format: upbit.StreamFormatDefault,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message upbit.StreamMessage) error {
	if message.Error != nil {
		return fmt.Errorf("Upbit stream error: %s", message.Error.Message)
	}
	if message.Status != "" {
		return nil
	}
	switch message.Type {
	case "ticker":
		var event upbit.StreamTicker
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleTicker(event)
	case "trade":
		var event upbit.StreamTrade
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleTrade(event)
	default:
		return nil
	}
})
```

public typed 범위는 다음과 같습니다.

- `ticker`: `StreamTicker`
- `trade`: `StreamTrade`
- `orderbook`: `StreamOrderBook`
- `candle.{unit}`: `StreamCandle`

`OnlySnapshot`과 `OnlyRealtime`은 동시에 지정할 수 없습니다. 응답 형식은 `DEFAULT`, `SIMPLE`, `JSON_LIST`, `SIMPLE_LIST`를 지원합니다. 오더북 `StreamOrderBook`은 전체 필드와 축약 필드를 모두 decode하며, 다른 typed 구조체에서 `SIMPLE` 계열을 사용할 때는 `Payload`에서 축약 필드 구조체로 직접 decode합니다.

### Spot 로컬 오더북 snapshot

업비트 오더북은 증분 delta가 아니라 SNAPSHOT과 REALTIME 모두 각 메시지에 완전한 호가 목록을 제공합니다. 공식 응답에는 오더북 sequence가 없으므로 REST snapshot과 임의의 연속 번호를 조합하지 않습니다. `LocalOrderBook`은 각 메시지의 마켓·timestamp·정렬·가격·수량을 독립적으로 검증하고 최신 snapshot view로 전달합니다.

```go
public, err := streams.PublicStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{{
			Type:  "orderbook",
			Codes: []string{"KRW-BTC.5"},
			Level: "10000",
		}},
		Format: upbit.StreamFormatSimpleList,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := upbit.NewLocalOrderBook(upbit.LocalOrderBookConfig{
	Market:        "KRW-BTC",
	EgressRouteID: "seoul-b",
	ViewDepth:     5,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view upbit.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- 로컬 오더북과 WebSocket의 `EgressRouteID`가 다르면 연결 전에 거부합니다.
- 마켓 코드는 대문자로 정규화하며 `.1`, `.5`, `.15`, `.30` 호가 개수 옵션을 검증합니다.
- KRW 마켓의 `Level`은 JSON 숫자로 전송하며, 다른 stream type에는 지정할 수 없습니다.
- 각 이벤트가 완전한 snapshot이므로 이전 상태를 병합하지 않고 통째로 교체합니다.
- 네트워크 재연결 시 같은 송신 경로와 새 ticket으로 다시 구독하며, 다음 완전한 snapshot부터 `Generation`이 증가한 view를 제공합니다.
- 공식 오더북 응답에 sequence가 없으므로 탐지할 수 없는 gap count를 만들지 않습니다. `SnapshotID`, `Generation`, `Timestamp`, `StreamType`으로 수신과 재연결을 관측합니다.

private stream은 handshake 요청의 `Authorization` 헤더에 HS512 JWT를 넣습니다.

```go
private, err := streams.PrivateStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{
			{Type: "myOrder", Codes: []string{"KRW-BTC"}},
			{Type: "myAsset"},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

세션 생성 시 자격증명의 read 권한과 route 허용 목록을 Secret 조회보다 먼저 검사합니다. 실제 연결과 재연결에서는 다음을 매번 새로 수행합니다.

1. Secret Provider에서 Access Key와 Secret Key 조회
2. 중복되지 않는 nonce 생성
3. query hash가 없는 WebSocket handshake용 HS512 JWT 생성
4. `Authorization: Bearer <JWT>` 헤더로 private endpoint 연결
5. 연결별 새 ticket을 포함한 `myOrder`·`myAsset` 구독 요청 전송
6. 사용한 자격증명 byte slice 파기

private typed event는 `MyOrderEvent`와 `MyAssetEvent`입니다. 자격증명과 JWT는 상태 이벤트나 decode 오류에 포함하지 않습니다.

### 연결 관리와 요청 제한

업비트는 120초 동안 송수신이 없으면 idle connection을 종료합니다. SDK는 기본 30초마다 WebSocket PING frame을 보내고 10초 안에 PONG을 받지 못하면 같은 route로 재연결합니다.

- 연결 요청: 초당 최대 5회
- 데이터 요청 메시지: 연결당 초당 최대 5회, 분당 최대 100회
- 인증 없는 연결 요청: IP 단위
- 인증 연결 요청: 계정 pocket 단위
- Origin 헤더가 있는 연결: 별도 10초당 1회 제한

SDK는 브라우저가 아니므로 Origin 헤더를 설정하지 않습니다. 한 세션은 최초 연결 또는 재연결 직후 구독 요청 한 건만 보내며, 구독을 바꾸려면 기존 세션을 종료하고 새 세션을 만듭니다. 여러 세션과 프로세스의 연결 시도 합계는 운영 메트릭에서 감시해야 합니다. `Run` context가 세션 수명을 결정하므로 stream 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [Upbit 인증](https://docs.upbit.com/kr/reference/auth)
- [Upbit 요청 수 제한](https://docs.upbit.com/kr/reference/rate-limits)
- [Upbit WebSocket 사용 안내](https://docs.upbit.com/kr/reference/websocket-guide)
- [Upbit 호가 WebSocket](https://docs.upbit.com/kr/reference/websocket-orderbook)
- [Upbit WebSocket 요청 형식](https://docs.upbit.com/kr/reference/websocket-request-format)
- [Upbit 내 주문 WebSocket](https://docs.upbit.com/kr/reference/websocket-myorder)
- [Upbit 내 자산 WebSocket](https://docs.upbit.com/kr/reference/websocket-myasset)
- [Upbit 주문 생성](https://docs.upbit.com/kr/reference/new-order)
- [Upbit order 요청 수 제한 상향](https://docs.upbit.com/kr/changelog/order-rate-limit-update)
