# OKX V5 Spot·SWAP 어댑터

## 범위

`exchange/okx` 패키지는 OKX V5의 다음 REST 기능을 제공합니다.

| 상품 | 공개 시세 | 거래 계정 | 포지션 | 주문 |
|---|---:|---:|---:|---:|
| `SPOT` | 지원 | 지원 | 해당 없음 | 현금 거래 지원 |
| `SWAP` | 지원 | 지원 | 지원 | 교차·격리 마진 지원 |

공개 시세는 상품 규칙, 전체 ticker, 호가, 최근 체결, 캔들을 포함합니다. 주문은 생성, 단건 조회, 취소, 미체결 목록, 최근 7일 이력을 포함합니다. Spot margin, 선물 주문, 옵션 주문은 아직 범위에 포함하지 않습니다. 공개 시장 데이터·펀딩 계정 자산·서브계정 API는 아래 "시장 데이터·자산·서브계정 확장 API" 절을 참고합니다. 일괄 주문, 알고(조건부) 주문, 계정 설정 API는 아래 "거래·계정 확장 API" 절을 참고합니다.

## 클라이언트와 지역별 endpoint

기본 REST endpoint는 `https://www.okx.com`입니다. 계정 등록 지역에 따라 OKX가 별도 도메인을 요구하면 공식 안내에 맞는 주소를 `BaseURL`로 지정해야 합니다. SDK는 지역 제한을 우회하거나 임의의 대체 도메인을 선택하지 않습니다.

```go
client, err := okx.New(okx.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
	BaseURL:              "https://www.okx.com",
	DemoTrading:          true,
})
if err != nil {
	return err
}
```

`DemoTrading: true`이면 모든 요청에 `x-simulated-trading: 1` 헤더를 추가합니다. 운영 키와 Demo 키 및 endpoint 설정을 섞지 않아야 합니다.

## 인증과 시간 보정

private API는 `credential.Material`의 다음 세 값을 사용합니다.

| 필드 | OKX 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | Secret Key |
| `Passphrase` | API Key 생성 시 설정한 Passphrase |

서명 원문은 다음과 같습니다.

```text
timestamp + uppercase(method) + requestPathWithSortedQuery + exactJSONBody
```

HMAC SHA-256 결과를 Base64로 인코딩해 `OK-ACCESS-SIGN`에 넣습니다. `OK-ACCESS-TIMESTAMP`는 UTC ISO-8601 밀리초 형식을 사용합니다. 서명은 요청 제한 대기가 끝난 뒤 최종 query와 JSON 바이트를 대상으로 생성합니다.

`ServerTime`은 요청 왕복 시간의 중간값과 서버 timestamp를 비교해 서명 시계 오프셋을 갱신합니다. 운영 환경에서도 NTP 또는 chrony 동기화가 전제입니다.

## 계정 모드와 주문

Spot 현금 주문은 `InstrumentTypeSpot`과 `TradeModeCash`를 사용합니다. Spot 시장가 주문은 `TargetCurrencyBase` 또는 `TargetCurrencyQuote`로 `sz`의 기준 통화를 명시할 수 있습니다.

SWAP 주문은 `TradeModeCross` 또는 `TradeModeIsolated`를 사용합니다. 계정이 long/short 포지션 모드이면 `PositionSideLong` 또는 `PositionSideShort`를 지정하고, net 모드이면 비워 두거나 `PositionSideNet`을 사용합니다. SDK는 계정의 실제 포지션 모드를 임의로 변경하지 않으므로 계정 설정과 요청이 맞지 않으면 OKX 오류를 그대로 정규화해 반환합니다.

```go
reference, err := client.PlaceOrder(
	ctx,
	okx.PlaceOrderRequest{
		InstrumentType: okx.InstrumentTypeSwap,
		InstrumentID:   "BTC-USDT-SWAP",
		TradeMode:      okx.TradeModeCross,
		ClientOrderID:  "strategya0001",
		Side:           okx.SideBuy,
		PositionSide:   okx.PositionSideLong,
		OrderType:      okx.OrderTypeLimit,
		Quantity:       "1",
		Price:          "60000",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

가격과 수량은 `float64`가 아닌 decimal 문자열입니다. `ClientOrderID`는 OKX 규칙에 맞게 영문자와 숫자로 최대 32자까지 허용합니다.

## 과거 캔들

`Candles`(`GET /api/v5/market/candles`)는 최근 1,440개만 반환합니다. 그보다 오래된 구간은 `HistoryCandles`(`GET /api/v5/market/history-candles`)로 조회합니다. 두 메서드는 같은 `CandlesRequest`를 사용하며 요청당 최대 300개입니다.

`After`는 해당 시각보다 오래된 봉, `Before`는 해당 시각보다 최신 봉을 요청합니다. 응답은 최신 봉부터 정렬되므로 응답의 가장 오래된 `Timestamp`를 다음 요청의 `After`로 넘기면 상장 시점까지 거슬러 올라갈 수 있습니다. `Before`가 `After`보다 이르지 않으면 요청 전에 검증 오류를 반환합니다.

`Candle1Second`는 `HistoryCandles`에서만 허용하며 OKX는 최근 3개월만 제공합니다.

```go
after := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
candles, err := client.HistoryCandles(ctx, okx.CandlesRequest{
	InstrumentID: "BTC-USDT", Interval: okx.Candle1Minute, After: &after, Limit: 300,
})
```

## 요청 제한

SDK limiter는 공식 제한의 실제 scope를 분리합니다.

| endpoint 종류 | 로컬 bucket | 제한 |
|---|---|---:|
| 상품 규칙 | route + 상품 유형 | 20/2초 |
| 전체 ticker | route | 20/2초 |
| 호가 | route | 40/2초 |
| 최근 체결 | route | 100/2초 |
| 캔들 | route | 40/2초 |
| 과거 캔들 | route | 20/2초 |
| 잔고·포지션 | 계정 | 10/2초 |
| 주문 생성·취소·단건 조회 | 계정 + 상품 + HTTP method | 60/2초 |
| 미체결 주문 | 계정 | 60/2초 |
| 최근 7일 주문 이력 | 계정 | 40/2초 |

송신 경로를 바꾸면 IP scope만 분리됩니다. 사용자 ID 또는 사용자+상품 scope 제한은 같은 계정 bucket을 공유하므로 다중 송신 IP를 계정 제한 우회 수단으로 사용하지 않습니다.

## 안전한 주문 실패

OKX는 top-level `code`가 `0`이어도 주문 접수 항목의 `sCode`로 실패를 반환할 수 있습니다. SDK는 두 수준을 모두 검사합니다.

주문 생성·취소의 전송 타임아웃, 연결 단절, 읽을 수 없는 응답, HTTP 5xx, `50004`·`50013`·`50026`·`51149`처럼 실행 여부가 불명확한 결과는 `trade.ErrUnknownExecutionState`로 반환하며 자동 재시도하지 않습니다. 고유한 `ClientOrderID`로 `OrderInfo` 또는 향후 private order stream을 조회해 최종 상태를 조정해야 합니다.

## 거래·계정 확장 API

모든 요청은 기존 주문 API와 같은 서명·송신 경로·제한 helper를 사용하며 변경 작업은 자동 재시도하지 않습니다. 권한 열의 Read는 `credential.PermissionRead`, Trade는 `credential.PermissionTrade`입니다.

### 주문

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `BatchPlaceOrders` | `POST /api/v5/trade/batch-orders` | Trade | 300주문/2초, 주문 수만큼 차감 |
| `BatchCancelOrders` | `POST /api/v5/trade/cancel-batch-orders` | Trade | 300주문/2초, 주문 수만큼 차감 |
| `AmendOrder` | `POST /api/v5/trade/amend-order` | Trade | 60/2초 (상품별) |
| `BatchAmendOrders` | `POST /api/v5/trade/amend-batch-orders` | Trade | 300주문/2초, 주문 수만큼 차감 |
| `CancelAllAfter` | `POST /api/v5/trade/cancel-all-after` | Trade | 1/초 (tag별) |
| `ClosePosition` | `POST /api/v5/trade/close-position` | Trade | 20/2초 (상품별) |
| `OrderPrecheck` | `POST /api/v5/trade/order-precheck` | Trade | 5/2초 |
| `Fills` | `GET /api/v5/trade/fills` (최근 3일) | Read | 60/2초 |
| `FillsHistory` | `GET /api/v5/trade/fills-history` (최근 3개월, `InstrumentType` 필수) | Read | 10/2초 |

일괄 요청은 최대 20건이며 초과하면 요청 전에 검증 오류를 반환합니다. OKX는 일부만 성공하면 top-level `code: "2"`로 응답합니다. SDK는 이를 오류로 바꾸지 않고 `PartialSuccess: true`와 항목별 `StatusCode`를 반환하므로 호출자가 항목별로 확인해야 합니다. 전부 실패(`code: "1"`)하면 첫 항목의 `sCode`로 분류한 오류와 항목별 결과를 함께 반환합니다.

`AmendOrderRequest.NewSize`는 남은 수량이 아니라 정정 후 주문 전체 목표 수량입니다. `OrderPrecheck`는 주문을 제출하지 않지만 OKX 문서가 권한을 명시하지 않아 보수적으로 Trade 권한을 요구하고 읽기 작업으로 실행합니다. 다중통화·포트폴리오 마진 계정에서만 동작합니다. `CancelAllAfter`의 `TimeOut`은 0(해제) 또는 10~120초이며 같은 요청을 반복해 카운트다운을 갱신합니다.

### 알고 주문

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `PlaceAlgoOrder` | `POST /api/v5/trade/order-algo` | Trade | 20/2초 (상품별) |
| `CancelAlgoOrders` | `POST /api/v5/trade/cancel-algos` (최대 10건) | Trade | 20주문/2초 |
| `AmendAlgoOrder` | `POST /api/v5/trade/amend-algos` | Trade | 20/2초 (상품별) |
| `AlgoOrderDetail` | `GET /api/v5/trade/order-algo` | Read | 20/2초 |
| `AlgoOrdersPending` | `GET /api/v5/trade/orders-algo-pending` | Read | 20/2초 |
| `AlgoOrderHistory` | `GET /api/v5/trade/orders-algo-history` (`State` 또는 `AlgoID` 필수) | Read | 20/2초 |

`PlaceAlgoOrder`는 `conditional`, `oco`, `trigger`, `move_order_stop`, `twap`을 지원합니다. `chase`·`smart_iceberg`, `attachAlgoOrds`, `triggerParams`는 아직 지원하지 않습니다. 목록 조회에서는 `conditional`과 `oco`만 함께 지정할 수 있습니다.

### 계정

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `AccountConfig` | `GET /api/v5/account/config` | Read | 5/2초 |
| `SetPositionMode` | `POST /api/v5/account/set-position-mode` | Trade | 5/2초 |
| `SetLeverage` | `POST /api/v5/account/set-leverage` | Trade | 20/2초 |
| `LeverageInfo` | `GET /api/v5/account/leverage-info` | Read | 20/2초 |
| `MaxSize` | `GET /api/v5/account/max-size` | Read | 20/2초 |
| `MaxAvailSize` | `GET /api/v5/account/max-avail-size` | Read | 20/2초 |
| `AdjustPositionMargin` | `POST /api/v5/account/position/margin-balance` | Trade | 20/2초 |
| `SetIsolatedMode` | `POST /api/v5/account/set-isolated-mode` | Trade | 5/2초 |
| `TradeFee` | `GET /api/v5/account/trade-fee` | Read | 5/2초 |
| `AccountBills` | `GET /api/v5/account/bills` (최근 7일) | Read | 5/초 |
| `AccountBillsArchive` | `GET /api/v5/account/bills-archive` (최근 3개월) | Read | 5/2초 |
| `PositionsHistory` | `GET /api/v5/account/positions-history` | Read | 10/2초 |
| `AccountPositionRisk` | `GET /api/v5/account/account-position-risk` | Read | 10/2초 |
| `MaxWithdrawal` | `GET /api/v5/account/max-withdrawal` | Read | 20/2초 |

`SetPositionMode`, `SetLeverage`, `SetIsolatedMode`, `AdjustPositionMargin`은 계정 설정·증거금을 바꾸는 Trade 권한 변경 작업입니다. `bills-history-archive`(파일 신청)·`subtypes`, 차입·이자·자동 상환, 계정 모드 변경, `position-builder` 등 나머지 계정 API는 이 절에 포함하지 않습니다.

## 시장 데이터·자산·서브계정 확장 API

모든 요청은 기존 주문 API와 같은 서명·송신 경로·제한 helper를 사용하며 변경 작업은 자동 재시도하지 않습니다. 권한 열의 Read·Transfer·Withdraw는 각각 `credential.PermissionRead`·`PermissionTransfer`·`PermissionWithdraw`입니다. 응답 항목은 `Raw` 원문을 함께 보존합니다. 공개 endpoint의 상품 유형은 `InstrumentTypeSwap`, `InstrumentTypeFutures`, `InstrumentTypeOption`, `InstrumentTypeMargin` 상수를 사용합니다. 이 상수는 공개 데이터 조회 파라미터용이며 주문·계정 API는 계속 Spot·SWAP만 검증합니다.

### 공개 시장 데이터

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `MarkPrices` | `GET /api/v5/public/mark-price` | 없음(공개) | 10/2초 |
| `FundingRates` | `GET /api/v5/public/funding-rate` (`ANY`로 전체 perp) | 없음(공개) | 10/2초 |
| `FundingRateHistory` | `GET /api/v5/public/funding-rate-history` (최대 400건) | 없음(공개) | 10/2초 |
| `OpenInterests` | `GET /api/v5/public/open-interest` | 없음(공개) | 20/2초 |
| `PriceLimits` | `GET /api/v5/public/price-limit` | 없음(공개) | 20/2초 |
| `OrderBookFull` | `GET /api/v5/market/books-full` (최대 5,000단계) | 없음(공개) | 10/2초 |
| `IndexTickers` | `GET /api/v5/market/index-tickers` | 없음(공개) | 20/2초 |
| `IndexCandles` | `GET /api/v5/market/index-candles` | 없음(공개) | 20/2초 |
| `HistoryIndexCandles` | `GET /api/v5/market/history-index-candles` | 없음(공개) | 10/2초 |
| `MarkPriceCandles` | `GET /api/v5/market/mark-price-candles` | 없음(공개) | 20/2초 |
| `HistoryMarkPriceCandles` | `GET /api/v5/market/history-mark-price-candles` | 없음(공개) | 20/2초 |
| `PremiumHistory` | `GET /api/v5/public/premium-history` | 없음(공개) | 20/2초 |
| `EstimatedPrices` | `GET /api/v5/public/estimated-price` (FUTURES·OPTION) | 없음(공개) | 10/2초 |
| `DiscountRateInterestFreeQuota` | `GET /api/v5/public/discount-rate-interest-free-quota` | 없음(공개) | 2/2초 |
| `InterestRateLoanQuotas` | `GET /api/v5/public/interest-rate-loan-quota` | 없음(공개) | 2/2초 |
| `PositionTiers` | `GET /api/v5/public/position-tiers` | 없음(공개) | 10/2초 |
| `InsuranceFunds` | `GET /api/v5/public/insurance-fund` | 없음(공개) | 10/2초 |
| `SystemStatus` | `GET /api/v5/system/status` | 없음(공개) | 1/5초 |
| `Announcements` | `GET /api/v5/support/announcements` | 없음(공개) | 5/2초 |
| `AnnouncementTypes` | `GET /api/v5/support/announcement-types` | 없음(공개) | 1/2초 |

서버 시간(`ServerTime`, `GET /api/v5/public/time`)은 기존 메서드를 그대로 사용합니다. 지수·마크가격 캔들은 `[ts,o,h,l,c,confirm]` 배열을 `PriceCandle`로 decode하며 `Limit` 최대값은 100입니다(거래 캔들의 300과 다름). `PositionTiers`는 SWAP·FUTURES·OPTION에 `InstrumentFamily`, MARGIN에 `InstrumentID` 또는 `Currency`가 필요하고 `InsuranceFunds`는 MARGIN을 제외하면 `InstrumentFamily`가 필요합니다. 요청 전에 이 조건을 검증합니다.

### 펀딩 계정 자산·이체

자산 endpoint는 모두 계정(User ID) 제한 bucket을 사용합니다.

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `AssetCurrencies` | `GET /api/v5/asset/currencies` | Read | 6/초 |
| `AssetBalances` | `GET /api/v5/asset/balances` | Read | 6/초 |
| `NonTradableAssets` | `GET /api/v5/asset/non-tradable-assets` | Read | 6/초 |
| `AssetValuation` | `GET /api/v5/asset/asset-valuation` | Read | 1/초 |
| `AssetTransfer` | `POST /api/v5/asset/transfer` | Transfer | 2/초 (통화별) |
| `AssetTransferStates` | `GET /api/v5/asset/transfer-state` | Read | 10/초 |
| `AssetBills` | `GET /api/v5/asset/bills` (최근 1개월) | Read | 6/초 |
| `AssetBillsHistory` | `GET /api/v5/asset/bills-history` (2021-02 이후, 30초 간격 갱신) | Read | 1/초 |
| `DepositAddresses` | `GET /api/v5/asset/deposit-address` | Read | 6/초 |
| `DepositHistory` | `GET /api/v5/asset/deposit-history` | Read | 6/초 |
| `Withdraw` | `POST /api/v5/asset/withdrawal` | Withdraw | 6/초 |
| `CancelWithdrawal` | `POST /api/v5/asset/cancel-withdrawal` | Withdraw | 6/초 |
| `WithdrawalHistory` | `GET /api/v5/asset/withdrawal-history` | Read | 6/초 |
| `DepositWithdrawStatuses` | `GET /api/v5/asset/deposit-withdraw-status` | Read | 1/2초 |

`DepositAddresses`는 기존 주소 조회이며 주소 생성 endpoint는 제공하지 않습니다. `AssetTransfer`, `Withdraw`, `CancelWithdrawal`은 전송 결과가 불명확해도 재시도하지 않으므로 `ClientID`를 지정해 `AssetTransferStates`·`WithdrawalHistory`로 최종 상태를 확인해야 합니다. `AssetTransferRequest.OmitPositionRisk`는 OKX가 요구하는 문자열 `"true"`로 전송됩니다. `Withdraw`의 수수료는 `Amount`에 포함되지 않으며, 일부 규제 지역 사용자는 `Receiver`(`rcvrInfo`)가 필요합니다. `DepositWithdrawStatuses`는 `WithdrawalID`와 `TxID` 중 정확히 하나만 받으며 `TxID`를 쓰면 `Currency`, `ToAddress`, `Chain`이 필요합니다.

### 견적 기반 코인 전환

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `ConvertCurrencies` | `GET /api/v5/asset/convert/currencies` | Read | 6/초 |
| `ConvertCurrencyPairs` | `GET /api/v5/asset/convert/currency-pair` | Read | 6/초 |
| `ConvertEstimateQuote` | `POST /api/v5/asset/convert/estimate-quote` | Transfer | 1/5초 (통화쌍별) |
| `ConvertTrade` | `POST /api/v5/asset/convert/trade` | Transfer | 10/초 |
| `ConvertHistory` | `GET /api/v5/asset/convert/history` | Read | 6/초 |

견적(`QuoteID`)은 `TTLMillis` 안에서만 유효하며 견적 요청은 거래를 실행하지 않습니다. `ConvertTrade`는 변경 작업이라 자동 재시도하지 않고 `ClientTradeID`로 `ConvertHistory`를 조회해 확인해야 합니다. 견적 요청도 거래 준비 단계이므로 Transfer 권한을 요구하는 변경 작업으로 처리합니다.

### 서브계정

서브계정 API는 마스터 계정 API Key로만 동작합니다.

| 메서드 | endpoint | 권한 | 로컬 제한 |
|---|---|---|---|
| `SubAccounts` | `GET /api/v5/users/subaccount/list` | Read | 20/2초 |
| `ModifySubAccountAPIKey` | `POST /api/v5/users/subaccount/modify-apikey` | Transfer | 1/초 |
| `SetSubAccountTransferOut` | `POST /api/v5/users/subaccount/set-transfer-out` | Transfer | 1/초 |
| `EntrustSubAccounts` | `GET /api/v5/users/entrust-subaccount-list` | Read | 1/초 |
| `SubAccountBalances` | `GET /api/v5/account/subaccount/balances` (거래 계정) | Read | 6/2초 |
| `SubAccountAssetBalances` | `GET /api/v5/asset/subaccount/balances` (펀딩 계정) | Read | 6/2초 |
| `SubAccountMaxWithdrawals` | `GET /api/v5/account/subaccount/max-withdrawal` | Read | 20/2초 |
| `SubAccountBills` | `GET /api/v5/asset/subaccount/bills` | Read | 6/초 |
| `ManagedSubAccountBills` | `GET /api/v5/asset/subaccount/managed-subaccount-bills` | Read | 6/초 |
| `TransferBetweenSubAccounts` | `POST /api/v5/asset/subaccount/transfer` | Transfer | 1/초 |

서브계정 생성·삭제와 API Key 생성·삭제·조회(`create-subaccount`, `apikey`, `delete-apikey`), 서브계정 VIP 대출 할당은 포함하지 않습니다.

### 이 절에서 제외한 항목

- 라이트닝 입출금(`deposit-lightning`, `withdrawal-lightning`)과 소액 자산 전환 `convert-dust-assets`는 현재 공식 REST 문서 본문에서 endpoint를 확인하지 못해 구현하지 않았습니다. 라이트닝 출금은 변경 이력의 예시로만 남아 있습니다.
- `books-rpi`, `history-trades`, `index-components`, `platform-24-volume`, `exchange-rate`, `liquidation-orders`, `convert-contract-coin`, `underlying`, `market-data-history`와 `rubik/stat/*` 통계 endpoint는 이번 범위에 포함하지 않았습니다.

## 요청별 송신 경로 선택

모든 메서드는 `trade.RequestOption`을 받습니다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 선택한 local source IP 전용 연결 풀로 보냅니다. 자격증명에 허용되지 않은 route는 Secret 조회 전에 거부됩니다.

## WebSocket

OKX V5 WebSocket은 채널 종류별 endpoint가 나뉩니다.

| 환경 | public | private | business |
|---|---|---|---|
| 운영 | `wss://ws.okx.com:8443/ws/v5/public` | `wss://ws.okx.com:8443/ws/v5/private` | `wss://ws.okx.com:8443/ws/v5/business` |
| Demo | `wss://wspap.okx.com:8443/ws/v5/public` | `wss://wspap.okx.com:8443/ws/v5/private` | `wss://wspap.okx.com:8443/ws/v5/business` |

- public: ticker, 체결, 호가
- business: 캔들
- private: 계정, 잔고·포지션, 포지션, 주문

지역별 WebSocket 도메인이 필요한 계정은 `PublicStreamURL`, `PrivateStreamURL`, `BusinessStreamURL`을 공식 안내에 맞게 함께 지정해야 합니다.

```go
streams, err := okx.NewStreamClient(okx.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
	DemoTrading:          true,
})
if err != nil {
	return err
}

ticker, err := okx.PublicStreamArgument("tickers", "BTC-USDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	okx.PublicStreamRequest{
		Endpoint:  okx.StreamEndpointPublic,
		Arguments: []okx.StreamArgument{ticker},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message okx.StreamMessage) error {
	if message.Event != "" || message.Pong {
		return nil
	}
	var tickers []okx.Ticker
	if err := message.DecodeData(&tickers); err != nil {
		return err
	}
	return handleTickers(tickers)
})
```

public typed data는 다음 구조체로 decode할 수 있습니다.

- `tickers`: `[]Ticker`
- `trades`: `[]StreamTrade`
- `books`, `books5`, `bbo-tbt`: `[]OrderBook`
- `candle{bar}`: `[]Candle`

캔들은 `CandleStreamArgument`로 인자를 만들고 `StreamEndpointBusiness` 세션에서 구독해야 합니다. public과 business 인자를 같은 연결에 섞으면 생성 시 거부합니다. 동적 `Subscribe`와 `Unsubscribe`가 성공하면 현재 목록을 갱신하고 재연결 때 같은 endpoint와 송신 경로에서 복구합니다.

## Spot·SWAP 로컬 오더북

`LocalOrderBook`은 public `books`, `books5`, `bbo-tbt` channel을 지원합니다. `books`는 최초 400단계 snapshot 뒤의 증분 update를 적용하고, `books5`와 `bbo-tbt`는 각각 5단계와 1단계의 완전 snapshot으로 매번 장부를 교체합니다.

```go
books, err := okx.PublicStreamArgument("books", "BTC-USDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	okx.PublicStreamRequest{
		Endpoint:  okx.StreamEndpointPublic,
		Arguments: []okx.StreamArgument{books},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := okx.NewLocalOrderBook(okx.LocalOrderBookConfig{
	Channel:       "books",
	InstrumentID:  "BTC-USDT",
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view okx.LocalOrderBookView) error {
	return handleBook(view)
})
```

`books` update의 `prevSeqId`가 직전 `seqId`와 다르면 장부를 폐기하고 WebSocket을 같은 송신 경로로 다시 연결해 새 snapshot을 받습니다. 약 60초 동안 변경이 없을 때 오는 빈 heartbeat update의 `prevSeqId == seqId`와 정비 중 `seqId`가 감소하는 공식 예외는 정상 처리합니다. 수량 0은 해당 가격 단계를 삭제합니다.

2026년 6월 23일부터 JSON `books` 계열의 `checksum`은 폐지되어 항상 0이므로 SDK는 무결성 판단에 사용하지 않고 `prevSeqId`·`seqId`만 검증합니다. `SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 sequence 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타냅니다. channel·상품·송신 경로가 public stream과 일치하지 않으면 연결 전에 거부합니다.

private stream은 연결마다 Unix 초 timestamp와 `timestamp + "GET" + "/users/self/verify"` 원문으로 새 서명을 만들어 login합니다.

```go
orders, err := okx.OrderStreamArgument(okx.InstrumentTypeSwap, "BTC-USDT-SWAP")
if err != nil {
	return err
}
positions, err := okx.PositionStreamArgument("BTC-USDT-SWAP")
if err != nil {
	return err
}
private, err := streams.PrivateStream(
	okx.PrivateStreamRequest{
		Arguments: []okx.StreamArgument{
			okx.AccountStreamArgument(),
			okx.BalanceAndPositionStreamArgument(),
			orders,
			positions,
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

private typed data는 `[]Balance`, `[]Position`, `[]Order`, `[]StreamBalanceAndPosition`으로 decode할 수 있습니다. 연결이 끊기면 같은 송신 경로에서 최신 Secret과 timestamp로 다시 login한 뒤 현재 channel 목록을 구독합니다. 로그인이 명시적으로 거절되면 같은 키로 무한 재연결하지 않습니다.

OKX는 연결당 login·subscribe·unsubscribe 요청을 합해 시간당 480회로 제한합니다. SDK는 동적 구독 명령을 기본 8초 간격으로 직렬화하고, 한 operation의 인자를 최대 100개씩 분할하며 64KiB를 넘는 요청은 거부합니다. 프로세스 전체의 IP당 연결 시도 제한 `3/초`와 private channel별 sub-account 연결 수는 운영 메트릭에서 별도로 감시해야 합니다.

연결 유지를 위해 기본 20초마다 application 문자열 `ping`을 보내고 10초 안에 `pong`이 없으면 같은 route로 재연결합니다. stream 수명은 `Run`의 context로 제어하므로 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [OKX V5 API 안내와 인증](https://www.okx.com/docs-v5/en/#overview-rest-authentication)
- [OKX V5 Public Data](https://www.okx.com/docs-v5/en/#public-data-rest-api)
- [OKX V5 Market Data](https://www.okx.com/docs-v5/en/#order-book-trading-market-data)
- [OKX V5 Trading Account](https://www.okx.com/docs-v5/en/#trading-account-rest-api)
- [OKX V5 Place Order](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-place-order)
- [OKX V5 WebSocket](https://www.okx.com/docs-v5/en/#overview-websocket)
- [OKX V5 Public Channels](https://www.okx.com/docs-v5/en/#order-book-trading-market-data-ws-channel)
- [OKX Order Book checksum 폐지 안내](https://www.okx.com/en-gb/help/okx-order-book-channels-checksum-field-deprecation)
- [OKX V5 Private Channels](https://www.okx.com/docs-v5/en/#order-book-trading-account-websocket)
