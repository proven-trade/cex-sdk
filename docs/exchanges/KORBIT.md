# Korbit Spot REST·WebSocket 어댑터

구현 기준은 코빗(Digital X) Open API v2, REST 기본 주소 `https://api.digitalx.miraeasset.com`, public/private WebSocket v2입니다.

## 기본 주소

공식 문서가 Base URLs를 REST `https://api.digitalx.miraeasset.com`, WebSocket `wss://ws-api.digitalx.miraeasset.com/v2/public`·`/v2/private`로 안내하므로 SDK 기본값을 같은 주소로 바꿨습니다. 변경에 앞서 2026-11-19에 두 REST 주소를 직접 확인했습니다.

| 확인 대상 | 결과 |
|---|---|
| `GET https://api.korbit.co.kr/v2/time` | HTTP 200, 리다이렉트 없음 |
| `GET https://api.digitalx.miraeasset.com/v2/time` | HTTP 200, 리다이렉트 없음 |
| `https://docs.korbit.co.kr/` | HTTP 301, `https://docs.digitalx.miraeasset.com/`로 이동 |

이전 주소도 아직 응답하지만 문서 기준이 아니므로 `Config.BaseURL`, `StreamClientConfig.PublicWebSocketURL`, `StreamClientConfig.PrivateWebSocketURL`에 명시했을 때만 사용합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | HMAC-SHA256 | ED25519 |
|---|---|---|
| `APIKey` | API Key | API Key |
| `SecretKey` | HMAC Secret 원문 | PKCS#8 PEM private key 원문 |

API Key의 서명 방식과 `Config.SigningMode`가 일치해야 합니다. 기본값은 `SigningModeHMACSHA256`이며 ED25519 키에는 `SigningModeED25519`를 지정합니다.

`credential.Descriptor.AccountID`에는 요청 제한을 공유하는 코빗 계정의 안정적인 식별자를 넣어야 합니다. `AccountSeq`는 코빗 하위 계정 번호이며 0이면 파라미터를 생략해 거래소 기본값 1을 사용합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 허용 IP에는 해당 route와 연결된 공인 송신 IP를 등록해야 합니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 서버 시각 | `ServerTime` | `GET /v2/time` |
| 공개 시세 | `Tickers`, `OrderBook`, `RecentTrades`, `Candles` | public v2 |
| 상품 규칙 | `CurrencyPairs`, `TickSizePolicy` | public v2 |
| 계정 | `Balances` | `GET /v2/balance` |
| 주문 생성 | `PlaceOrder` | `POST /v2/orders` |
| 주문 상세·취소 | `OrderInfo`, `CancelOrder` | `GET`, `DELETE /v2/orders` |
| 주문·체결 목록 | `OpenOrders`, `OrderHistory`, `MyTrades` | private v2 |
| 공개 stream | `ticker`, `orderbook`, `trade` | public WebSocket v2 |
| 개인 stream | `myOrder`, `myTrade`, `myAsset` | private WebSocket v2 |

위 표는 1차 구현 범위입니다. 자산 상태·공지·시장경보, 수수료 정책·API Key·계정 조회, STP 차단 내역, 가상자산·원화 입출금, 계정 간 이전은 [추가 REST API](#추가-rest-api)에서 카테고리별로 설명합니다.

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 문자열로 보존합니다. 각 조회 항목의 `Raw`에는 해당 `data` 항목의 원본 JSON을 보존합니다. 취소 결과의 `Raw`에는 전체 응답 envelope를 보존합니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 만들고 서명합니다.

1. endpoint 파라미터에 현재 Unix millisecond `timestamp`와 `recvWindow`를 추가합니다.
2. `signature`를 제외한 전체 파라미터를 Go `url.Values.Encode` 규칙으로 URL 인코딩합니다.
3. 그 문자열을 HMAC-SHA256 소문자 hex 또는 ED25519 표준 Base64로 서명합니다.
4. URL 인코딩한 `signature`를 마지막 파라미터로 추가하고 `X-KAPI-KEY` 헤더를 설정합니다.
5. GET·DELETE는 query string, POST는 같은 문자열의 `application/x-www-form-urlencoded` 본문으로 보냅니다.

서명 원문과 실제 전송 파라미터의 정렬·인코딩이 달라지면 인증에 실패합니다. `ReceiveWindow` 기본값은 5,000ms이며 1~60,000ms만 허용합니다. 운영 인스턴스는 NTP로 시각을 동기화하고 `ServerTime`으로 차이를 관측해야 합니다.

Provider가 반환한 자격증명 byte slice와 생성된 서명 bytes는 요청 후 가능한 범위에서 덮어씁니다. Go 문자열과 네트워크 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## 요청 제한과 송신 경로

SDK 기본 로컬 제한은 공개 50회/초, private 일반 50회/초, 주문 생성 30회/초, 주문 취소 30회/초, 입출금·계정 간 이전 5회/초입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `korbit:route:<route>:public:1second` | 50회/초 | 선택한 송신 경로 |
| `korbit:account:<account>:private:1second` | 50회/초 | 계정의 일반 private API |
| `korbit:account:<account>:order-place:1second` | 30회/초 | 계정의 주문 생성 API |
| `korbit:account:<account>:order-cancel:1second` | 30회/초 | 계정의 주문 취소 API |
| `korbit:account:<account>:asset:1second` | 5회/초 | 계정의 가상자산·원화 입출금과 계정 간 이전 API |

응답의 `Ratelimit` 헤더에서 `remaining` 값을 읽어 로컬 사용량이 거래소 관측값보다 작지 않게 보정합니다. `429`는 요청 제한 오류로 분류하며 거래소의 `Retry-After` 처리는 공통 실행 계층을 따릅니다. 제한값은 `Config`에서 더 보수적으로 조정할 수 있습니다. 입출금·계정 간 이전 한도는 `Config.AssetRequestsPerSecond`(기본 `DefaultAssetRequestsPerSecond` = 5)로 지정합니다.

공식 문서의 입출금·계정 간 이전 한도는 `accountSeq`당 5회/초이고 메인 계정의 계정 간 이전과 출금 요청이 같은 한도를 나눠 씁니다. SDK는 `credential.Descriptor.AccountID`당 `asset` bucket 하나로 합산하므로 하위 계정을 번갈아 호출해도 5회/초를 넘기지 않습니다. 한도를 넘기면 오류 대신 대기합니다.

여러 공인 송신 IP를 사용해도 계정 단위 private 제한은 늘어나지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- 모든 신규 주문에 `[0-9A-Za-z.:_-]{1,36}` 형식의 고유한 `ClientOrderID`를 요구합니다.
- 지정가는 `Price`와 `Qty`를 사용하며 GTC·IOC·FOK·post-only를 지원합니다.
- 시장가·최유리호가 매수는 총액 `Amount`, 매도는 수량 `Qty`를 사용합니다.
- 최유리호가 주문은 1~5의 `BestNth`와 `TimeInForce`가 필요합니다.
- 가격 보호를 켜면 선택적으로 1~100의 `PriceProtectionPercent`를 지정할 수 있습니다.
- 주문 조회와 취소는 `OrderID` 또는 `ClientOrderID` 중 정확히 하나만 허용합니다.
- 미체결·주문·체결 목록은 최대 1,000건입니다. 주문·체결 이력은 현재 기준 최근 36시간 안의 시간만 허용합니다.
- 전체 현재가 조회는 실수로 조회 범위를 넓히지 않도록 `TickersRequest.AllSymbols: true`를 명시해야 합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 서버 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 같은 `ClientOrderID`로 무조건 재주문하지 말고 `OrderInfo`를 먼저 조회해 거래소 상태를 확인해야 합니다.

## 추가 REST API

아래 메서드도 `Client`에 있으며 같은 서명·요청 제한·오류 계약을 따릅니다. 표의 `SDK 권한`은 `credential.Descriptor.Permissions`에 있어야 하는 값이고, 없으면 Secret 조회와 전송 전에 `trade.ErrAuthorization`(`credential.ErrPermissionNotAllowed`)으로 거부합니다. `거래소 권한`은 Digital X 개발자 센터에서 API Key에 부여해야 하는 권한이며 SDK 권한과 별개입니다. `bucket`은 위 요청 제한 표의 이름입니다. 수량·금액·수수료는 문자열로 보존하고 각 항목의 `Raw`에는 해당 `data` 항목의 원본 JSON을 보존합니다.

### 공개 정보

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `Currencies` | `GET /v2/currencies` | 없음(public) | 없음 | public |
| `Notices` | `GET /v2/notices` | 없음(public) | 없음 | public |
| `MarketAlerts` | `GET /v2/marketAlerts` | 없음(public) | 없음 | public |

- `Currencies`는 자산별 입출금 상태와 네트워크별 수수료·최소 출금 수량·정밀도를 반환합니다. 최상위 `WithdrawalStatus`, `DepositStatus`, `ConfirmationCount`, `WithdrawalTxFee`는 공식 문서에서 deprecated이므로 가상자산은 `NetworkList`의 같은 이름 필드를 사용해야 합니다. 원화 `krw`는 `NetworkList`가 없어 최상위 값만 제공합니다. 최상위 `ConfirmationCount`는 문자열이고 네트워크별 값은 정수입니다.
- 실제 `/v2/currencies` 응답에 1글자 심볼(`g`, `s`, `a`, `t`, `w`)이 있어 자산 코드 검증은 1~20자의 소문자·숫자를 허용합니다.
- `Notices`는 일반 공지와 Open API 공지를 합친 최신 20건을 최신순으로 반환하며 수정된 적이 없는 공지의 `UpdatedAt`은 nil입니다.
- `MarketAlerts`는 시장경보가 걸린 거래쌍만 반환하며 경보가 없으면 빈 목록입니다.

### 계정·API Key·STP 조회

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `TradingFeePolicy` | `GET /v2/tradingFeePolicy` | `read` | `readOrders` | private |
| `CurrentKeyInfo` | `GET /v2/currentKeyInfo` | `read` | 없음 | private |
| `Accounts` | `GET /v2/accounts` | `read` | 없음 | private |
| `MyPreventedMatches` | `GET /v2/myPreventedMatches` | `read` | `readOrders` | private |

- `TradingFeePolicy`는 `Symbols`를 비우면 전체 거래쌍을 조회하고 최대 100개를 쉼표로 묶어 보냅니다.
- `CurrentKeyInfo`는 요청에 쓴 API Key의 서명 방식, 허용 IP, `AllowedAccountSeqs`와 거래소 권한 이름(`readOrders`, `writeAccountTransfers` 등)을 반환합니다. 권한은 새 값이 추가돼도 문자열로 보존합니다. 계정 간 이전의 출발 계정은 `AllowedAccountSeqs`에 있어야 하므로 이전 전에 확인합니다.
- `Accounts`는 메인(`AccountSeq` 1)·하위 계정을 `accountSeq` 오름차순으로 반환합니다.
- `MyPreventedMatches`는 자전거래 방지(STP)가 중단시킨 taker·maker 충돌을 최신순으로 반환합니다. STP 취소는 주문에서 직접 요청한 취소와 같은 `canceled`·`partiallyFilledCanceled`로 보이므로 이 기록으로만 구분할 수 있습니다. 거래소가 `AccountSeq` 생략을 `BAD_REQUEST`로 거부하므로 SDK는 메인 계정도 1로 명시하도록 요구합니다(1~9,999). API Key의 `AllowedAccountSeqs`에 없는 계정은 `ACCOUNT_SEQ_NOT_ALLOWED`(`trade.ErrAuthorization`)로 거부됩니다. `Limit`은 1~1,000이고 0이면 거래소 기본값(500)을 씁니다. 응답의 `NextCursor`를 같은 `Symbol`·`OrderID`·`AccountSeq` 조건의 `Cursor`로 넘겨 빈 목록과 빈 `NextCursor`가 올 때까지 반복합니다. 한쪽 수량·금액이 정해지지 않는 시장가 주문의 `TakerPreventedQty`·`TakerPreventedAmt`는 빈 문자열입니다.

### 가상자산 입금

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `CoinDepositAddresses` | `GET /v2/coin/depositAddresses` | `read` | `readDeposits` | asset |
| `CoinDepositAddress` | `GET /v2/coin/depositAddress` | `read` | `readDeposits` | asset |
| `CreateCoinDepositAddress` | `POST /v2/coin/depositAddress` | `transfer` | `writeDeposits` | asset |
| `RecentCoinDeposits` | `GET /v2/coin/recentDeposits` | `read` | `readDeposits` | asset |
| `CoinDeposit` | `GET /v2/coin/deposit` | `read` | `readDeposits` | asset |

- 입출금 API는 메인 계정만 지원하므로 `AccountSeq`는 0(생략)이나 1만 허용하고 하위 계정은 전송 전에 거부합니다.
- 주소 조회·생성의 `Network`는 선택이지만 거래소 기본 네트워크는 바뀔 수 있으므로 항상 지정하는 것을 권장합니다. 주소 생성은 이미 주소가 있으면 기존 주소를 반환합니다.
- `SecondaryAddress`는 destination tag·memo이며 없으면 빈 문자열입니다. `RecentCoinDeposits`의 `Limit`은 1~100이고 0이면 거래소 기본값입니다.
- 입금 `Status`는 `pending`, `actionRequired`(서류 제출 대기), `reviewing`, `done`, `refunded`, `failed`입니다. `FilledAt`은 잔고에 반영된 시각이며 기록된 시각이 없으면(진행 중인 입금 포함) nil입니다.
- `CreateCoinDepositAddress`도 변경 요청이므로 자동 재시도하지 않습니다. 결과가 불명확하면 `CoinDepositAddress`로 주소 발급 여부를 확인합니다.

### 가상자산 출금

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `WithdrawableAddresses` | `GET /v2/coin/withdrawableAddresses` | `read` | `readWithdrawals` | asset |
| `WithdrawableAmounts` | `GET /v2/coin/withdrawableAmount` | `read` | `readWithdrawals` | asset |
| `CreateCoinWithdrawal` | `POST /v2/coin/withdrawal` | `withdraw` | `writeWithdrawals` | asset |
| `CancelCoinWithdrawal` | `DELETE /v2/coin/withdrawal` | `withdraw` | `writeWithdrawals` | asset |
| `CoinWithdrawal` | `GET /v2/coin/withdrawal` | `read` | `readWithdrawals` | asset |
| `RecentCoinWithdrawals` | `GET /v2/coin/recentWithdrawals` | `read` | `readWithdrawals` | asset |

- 출금 `Address`는 API 출금용으로 미리 등록한 주소만 허용되며(`UNREGISTERED_WITHDRAWAL_ADDRESS`) `WithdrawableAddresses`로 확인합니다. SDK는 체인별 주소 형식을 추측하지 않고 비어 있거나 공백·제어 문자가 있는 주소만 거부합니다.
- `Network`는 거래소 문서상 선택이지만 기본 네트워크가 바뀔 수 있고 잘못된 네트워크로 보낸 자산은 되돌릴 수 없으므로 SDK는 출금에서 필수로 요구합니다. `Amount`는 수수료를 제외한 출금 수량이며 지수 표기는 거부합니다.
- 출금 API에는 멱등 키가 없습니다. `CreateCoinWithdrawal`의 전송 오류, 서버 오류, 응답 파싱 실패는 `trade.ErrUnknownExecutionState`이며 같은 요청을 다시 보내기 전에 `RecentCoinWithdrawals`로 접수 여부를 먼저 확인해야 합니다.
- 접수 응답의 `Status`가 `pending`이어도 이메일 확인(`actionRequired`)이나 심사(`reviewing`)로 지연될 수 있으므로 `CoinWithdrawal`로 최종 상태를 확인합니다. 잔고 부족은 `NO_BALANCE` 오류로 거부되거나, 접수된 뒤 상태가 `failed`로 나타날 수 있습니다.
- `CancelCoinWithdrawal`은 `actionRequired`·`reviewing` 상태만 취소할 수 있고(`CANNOT_CANCEL_WITHDRAWAL`, `WITHDRAWAL_ALREADY_FINISHED`) 결과의 `Raw`에 전체 응답 envelope를 보존합니다.
- `WithdrawableAmounts`는 `Currency`를 비우면 모든 자산을 목록으로 반환합니다. `Withdrawable`은 출금 가능 수량, `WithdrawalInUse`는 출금 진행 중인 수량입니다.

### 원화 입출금

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `SendKRWDepositPush` | `POST /v2/krw/sendKrwDepositPush` | `transfer` | `writeDeposits` | asset |
| `SendKRWWithdrawalPush` | `POST /v2/krw/sendKrwWithdrawalPush` | `withdraw` | `writeWithdrawals` | asset |
| `RecentKRWDeposits` | `GET /v2/krw/recentDeposits` | `read` | `readDeposits` | asset |
| `RecentKRWWithdrawals` | `GET /v2/krw/recentWithdrawals` | `read` | `readWithdrawals` | asset |

- 푸시 API는 Digital X 앱으로 알림만 보내며 사용자가 앱에서 본인 확인을 마쳐야 입금·출금이 진행됩니다. 반환값의 `Accepted`는 알림 요청이 접수됐다는 뜻이지 입출금 완료가 아니며 `Raw`에 전체 응답 envelope를 보존합니다.
- SDK 권한은 자산이 거래소로 들어오는 입금 푸시를 `transfer`, 거래소 밖으로 나가는 출금 푸시를 `withdraw`로 구분합니다.
- `RecentKRWDeposits`의 `IncludeAll`이 true면 입금 수수료·이벤트 보상 같은 부가 내역도 포함하고 `Type`을 채웁니다. false면 일반 원화 입금만 조회하며 `Type`은 빈 문자열입니다.

### 계정 간 이전

| 메서드 | API | SDK 권한 | 거래소 권한 | bucket |
|---|---|---|---|---|
| `CreateAccountTransfer` | `POST /v2/accountTransfer` | `transfer` | `writeAccountTransfers` | asset |
| `AccountTransfer` | `GET /v2/accountTransfer` | `read` | `readAccountTransfers` | asset |
| `RecentAccountTransfers` | `GET /v2/recentAccountTransfers` | `read` | `readAccountTransfers` | asset |

- 입출금 API와 달리 `AccountSeq`(출발)와 `ToAccountSeq`(도착)를 항상 1~9,999로 명시해야 하며 서로 달라야 합니다. `AccountSeq`는 생성뿐 아니라 조회에서도 API Key의 `AllowedAccountSeqs`에 있어야 하고 아니면 `ACCOUNT_SEQ_NOT_ALLOWED`(`trade.ErrAuthorization`)가 반환됩니다. `Qty`는 소수점 이하 8자리 이하의 양수이며 지수 표기는 거부합니다.
- `RequestKey`는 거래소 문서상 선택이지만 SDK는 필수로 요구합니다. 생략하면 서버가 공개하지 않는 키를 만들어 ID 없이는 결과를 조회할 수 없기 때문입니다. 키는 `[A-Za-z0-9._-]{1,36}`이고 사용자 단위로 유일합니다. 접수 전에 거부된 요청(예: `NO_BALANCE`)은 키를 점유하지 않아 같은 키로 다시 보낼 수 있지만, 접수된 이전은 `failed`로 끝나도 키가 소멸하지 않으므로 새 이전마다 새 키를 만들어 저장합니다.
- 접수는 정산이 아닙니다. `CreateAccountTransfer`가 반환하는 `Status`는 접수 직후 항상 `pending`이고 자산은 비동기로 이동하므로 `AccountTransfer`로 `done`·`failed`를 확인합니다. 거래소 문서의 지침대로 정의되지 않은 상태 값(빈 문자열 포함)은 문자열 그대로 보존하며 아직 최종이 아닌 것으로 취급해야 합니다.
- `UNKNOWN_EXECUTION_STATE`(`trade.ErrUnknownExecutionState`)나 거래소 오류 `DUPLICATE_REQUEST_KEY`를 받으면 같은 `RequestKey`로 `AccountTransfer`를 조회해 접수 여부를 확인한 뒤에만 다시 요청합니다. `DUPLICATE_REQUEST_KEY` 응답에는 이전 ID가 없습니다.
- `AccountTransfer`는 `ID`와 `RequestKey` 중 정확히 하나를 지정합니다. 존재하지 않는 이전과 `AccountSeq`가 출발·도착 어느 쪽도 아닌 이전은 모두 `NOT_FOUND`(`trade.ErrOrderNotFound`)로 반환되어 구분할 수 없습니다.
- `RecentAccountTransfers`는 한 계정의 한쪽 방향(`Direction` `out` 또는 `in`)만 최신순으로 반환하며 `Limit`(1~100)이 필수입니다. 양쪽을 보려면 두 번 호출하고, 방금 제출한 이전은 목록에 아직 없을 수 있으므로 `AccountTransfer`로 직접 확인합니다.

### 변경 요청 공통 계약

`CreateCoinDepositAddress`, `CreateCoinWithdrawal`, `CancelCoinWithdrawal`, `SendKRWDepositPush`, `SendKRWWithdrawalPush`, `CreateAccountTransfer`는 자동 재시도하지 않습니다. 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 응답에 주소·ID가 없는 경우, 거래소 5xx는 실제 처리 여부를 단정할 수 없으므로 `trade.ErrUnknownExecutionState`로 반환합니다. 거래소 문서는 `SERVICE_UNAVAILABLE`을 "접수하지 못했다"고 안내하지만 해당 HTTP 상태를 밝히지 않으므로 SDK는 코드를 따로 다루지 않고 위의 HTTP 상태 기준(5xx면 `UNKNOWN_EXECUTION_STATE`)을 그대로 적용합니다. 어떤 분류로 돌아와도 위의 조회 메서드로 상태를 확인한 뒤 다시 요청합니다.

## 오류 처리

SDK는 HTTP 상태와 `{success,data,error}` envelope를 모두 검사합니다. `NO_BALANCE`, 주문 없음·이미 종료됨·`NOT_FOUND`, `INVALID_USER_STATUS`, `ACCOUNT_SEQ_NOT_ALLOWED`, `TRY_AGAIN`, `EXCEED_TIME_WINDOW`, HTTP 429와 5xx를 공통 `trade.APIError` 범주로 변환하며 거래소의 symbolic message는 `ExchangeCode`에 보존합니다. `DUPLICATE_REQUEST_KEY`, `UNREGISTERED_WITHDRAWAL_ADDRESS`, `WITHDRAWAL_SUSPENDED`, `ACCOUNT_SEQ_NOT_FOUND`처럼 따로 분류하지 않는 코드는 코드가 아니라 HTTP 상태로 분류합니다(일반 4xx는 `trade.ErrValidation`, 401은 인증, 403은 권한, 404는 `trade.ErrOrderNotFound`, 5xx는 서버 오류). 거래소는 오류 코드별 HTTP 상태를 문서화하지 않으므로 어떤 분류이든 `ExchangeCode`로 구분합니다. 인증 헤더, Secret과 서명 원문은 오류에 포함하지 않습니다.

## WebSocket

`StreamClient`는 기본값으로 public `wss://ws-api.digitalx.miraeasset.com/v2/public`과 private `wss://ws-api.digitalx.miraeasset.com/v2/private`를 분리합니다. 이전 `ws-api.korbit.co.kr` 주소는 `StreamClientConfig`에 명시했을 때만 사용합니다. `PublicStream`과 `PrivateStream`을 생성할 때 선택한 송신 경로는 해당 세션의 모든 재연결에서도 고정됩니다.

private handshake는 매 연결 세대마다 Secret Provider를 다시 조회하고 `timestamp`, `recvWindow`를 REST와 같은 방식으로 서명합니다. 서명은 URL query의 마지막 파라미터로 넣고 API Key는 `X-KAPI-KEY` 헤더로 전송합니다. 인증 query가 포함된 endpoint는 오류와 상태 객체에 보존하지 않습니다.

| 구분 | 지원 채널 | 구독 조건 |
|---|---|---|
| public | `ticker`, `orderbook`, `trade` | 거래쌍 1개 이상 |
| private | `myOrder`, `myTrade` | 거래쌍 1개 이상, 선택적 `AccountSeqs` |
| private | `myAsset` | 선택적 `AccountSeqs`, 거래쌍 없음 |

- 구독과 해제는 각 항목에 단조 증가하는 `requestId`를 넣은 JSON 배열로 전송합니다.
- `Subscribe`와 `Unsubscribe`로 실행 중 구독을 변경할 수 있고 현재 구독은 재연결 때 자동 복구합니다.
- 서버의 `success`, `fail`, `error` 제어 응답도 `StreamMessage`로 handler에 전달합니다. 실패한 subscribe ack는 재연결 구독 목록에서 제거합니다.
- 데이터 이벤트의 `snapshot` 여부와 원본 JSON을 보존하며 `StreamMessage.Decode`로 채널별 타입에 변환합니다.
- public `tradeId`는 거래쌍별로 증가하지만 연속성을 보장하지 않고 재연결 후 중복 전송될 수 있으므로 소비자가 거래쌍과 `tradeId`로 중복을 제거해야 합니다.
- public 메시지는 부하 시 유실될 수 있습니다. 호가는 주기적으로 REST `OrderBook` snapshot과 비교하고 재연결 후 새 snapshot을 받은 뒤 다시 신뢰해야 합니다.
- private 메시지는 연결 중에는 유실 대신 소켓이 강제 종료될 수 있지만 구독 직후 상태 snapshot을 보내지 않습니다. 최초 연결과 재연결마다 `Balances`, `OpenOrders`, `OrderHistory`, `MyTrades`로 주문·체결·잔고를 재조정해야 합니다.
- WebSocket control ping을 기본 15초마다 보내고 5초 안에 pong이 없으면 같은 route로 재연결합니다.

한 세션의 handler는 수신 순서대로 호출됩니다. 느린 처리는 사용자 애플리케이션에서 별도 bounded queue로 분리해야 하며, 재조정 중 들어온 이벤트와 REST 결과는 주문 ID·체결 ID·하위 계정을 기준으로 병합해야 합니다.

### Spot 로컬 오더북 snapshot

코빗 `orderbook`은 구독 직후 `snapshot: true`인 최신 호가를 보내고 이후 `false` 또는 `null`인 실시간 전체 호가를 제공합니다. 각 프레임은 양방향 최대 30단계를 포함하며 sequence는 없습니다. `LocalOrderBook`은 매 프레임을 독립 검증해 이전 상태와 병합하지 않고 통째로 교체합니다.

```go
public, err := streams.PublicStream(
	korbit.StreamRequest{Subscriptions: []korbit.StreamSubscription{{
		Channel: korbit.StreamChannelOrderBook,
		Symbols: []string{"btc_krw"},
		Level:   "1000",
	}}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := korbit.NewLocalOrderBook(korbit.LocalOrderBookConfig{
	Symbol:        "btc_krw",
	Level:         "1000",
	EgressRouteID: "seoul-b",
	ViewDepth:     30,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view korbit.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- 로컬 오더북과 WebSocket의 심볼·`Level`·`EgressRouteID`가 다르면 연결 전에 거부합니다.
- 응답에는 구독 `level`이 없으므로 같은 심볼의 orderbook 구독이 여러 개면 서로 구분할 수 없어 사전 거부합니다.
- asks는 낮은 가격부터, bids는 높은 가격부터인 best-first 순서와 가격·수량·선택적 금액을 검증합니다.
- 같은 연결 세대에서 data timestamp가 역행한 프레임은 무시하고, 같은 millisecond의 복수 변경은 유실하지 않도록 같은 timestamp는 허용합니다.
- 재연결 시 같은 송신 경로와 현재 구독으로 복구하고 새 세대의 첫 전체 호가를 받아들입니다. `SnapshotID`, `Generation`, envelope·data timestamp, `Snapshot`으로 상태를 관측합니다.
- `LocalOrderBook.Run`이 대상 구독의 수명주기를 소유하므로 실행 중 해당 구독을 제거하지 않아야 합니다.
- 공식 계약상 public 메시지는 부하 시 누락될 수 있고 sequence가 없어 누락을 탐지할 수 없습니다. 정합성이 매우 중요한 운영에서는 같은 송신 경로의 REST `OrderBook`을 주기적으로 조회해 view와 대조해야 합니다.

## 공통 Spot API

`NewUnifiedSpot`은 native 클라이언트를 `unified.SpotClient`로 변환합니다. 공통 `BTC/KRW`는 Korbit의 소문자 `btc_krw`로 변환하며, 시장가 매수의 `QuoteAmount`는 `amt`, 시장가 매도의 `Quantity`는 `qty`로 전송합니다. `ClientOrderID`를 생략하면 36자 이내의 암호학적 난수 ID를 생성합니다.

Korbit에 native 3분봉이 없으므로 1분봉을 `start`와 `end`로 최대 200개씩 나눠 같은 요청별 송신 경로에서 조회하고 공통 epoch 기준으로 합성합니다. 전체 마켓 미체결 조회는 public `CurrencyPairs` 뒤 각 거래쌍의 private `OpenOrders`를 같은 송신 경로에서 순회합니다.

공통 잠금 잔고는 `tradeInUse + withdrawalInUse`를 decimal 문자열 정밀도로 계산합니다. `pending`, `open`, 부분 체결, 완료, 부분 체결 후 취소, 만료 상태는 공통 주문 상태로 변환합니다.

## 공식 기준

- [Digital X(구 Korbit) Open API 문서](https://docs.digitalx.miraeasset.com/) (`docs.korbit.co.kr`은 이 주소로 301 이동합니다)
- [Open API LLM 문서](https://docs.digitalx.miraeasset.com/llms.txt)
- [REST API](https://docs.digitalx.miraeasset.com/llms/en/rest_api.md)
- [REST 기타 endpoint](https://docs.digitalx.miraeasset.com/llms/en/rest_api/other.md)
- [REST 주문·체결](https://docs.digitalx.miraeasset.com/llms/en/rest_api/trading.md)
- [REST 가상자산 입금](https://docs.digitalx.miraeasset.com/llms/en/rest_api/deposit-crypto.md)
- [REST 가상자산 출금](https://docs.digitalx.miraeasset.com/llms/en/rest_api/withdrawal-crypto.md)
- [REST 원화 입출금](https://docs.digitalx.miraeasset.com/llms/en/rest_api/krw.md)
- [REST 계정 간 이전](https://docs.digitalx.miraeasset.com/llms/en/rest_api/account-transfer.md)
- [WebSocket API](https://docs.digitalx.miraeasset.com/llms/en/websocket_api.md)
- [public WebSocket](https://docs.digitalx.miraeasset.com/llms/en/websocket_api/public.md)
