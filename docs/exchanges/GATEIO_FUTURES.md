# Gate.io API v4 무기한 Futures REST 어댑터

구현 기준은 Gate.io API v4 Perpetual Futures REST와 기본 주소 `https://api.gateio.ws/api/v4`입니다. Go 패키지는 `exchange/gateio/futures`이며 USDT·BTC·USD1 결제 통화를 명시적으로 선택합니다.

## 전제조건

private API는 Spot과 같은 Gate.io API Key·Secret 및 `model.ExchangeGateIO` 자격증명 설명자를 사용합니다. `credential.Descriptor.AccountID`에는 Futures UID 요청 제한을 공유하는 계정 식별자를 넣고, API Key의 허용 IP에는 사용할 EIP를 모두 등록해야 합니다.

```go
client, err := futures.New(futures.Config{
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

tickers, err := client.Tickers(
	ctx,
	futures.TickersRequest{
		Settlement: futures.SettlementUSDT,
		Contract:   "BTC_USDT",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 계약 목록 | `Contracts` | `GET /futures/{settle}/contracts` |
| 계약 상세 | `Contract` | `GET /futures/{settle}/contracts/{contract}` |
| 거래 통계 | `Tickers` | `GET /futures/{settle}/tickers` |
| 호가 | `OrderBook` | `GET /futures/{settle}/order_book` |
| 공개 체결 | `RecentTrades` | `GET /futures/{settle}/trades` |
| 캔들 | `Candles` | `GET /futures/{settle}/candlesticks` |
| 계정 | `Account` | `GET /futures/{settle}/accounts` |
| 포지션 | `Positions` | `GET /futures/{settle}/positions` |
| 주문 생성 | `PlaceOrder` | `POST /futures/{settle}/orders` |
| 주문 상세 | `OrderInfo` | `GET /futures/{settle}/orders/{order_id}` |
| 주문 취소 | `CancelOrder` | `DELETE /futures/{settle}/orders/{order_id}` |
| 주문 목록 | `Orders` | `GET /futures/{settle}/orders` |
| 계정 체결 | `MyTrades` | `GET /futures/{settle}/my_trades` |

가격·수량·금액·수수료는 문자열로 보존합니다. 주문·체결의 JSON 정수 ID는 `Identifier`가 부동소수점 변환 없이 문자열로 보존하며, double timestamp는 `Decimal`이 원문 십진 표현을 유지합니다. 목록 항목과 단일 객체의 `Raw`에는 해당 원본 JSON을 보존합니다.

## 주문 계약

- `Settlement`는 `btc`, `usdt`, `usd1` 중 하나를 반드시 선택합니다.
- 계약 수량 `Size`는 매수 양수·매도 음수인 signed decimal 문자열입니다. 계약의 `EnableDecimal`이 false이면 운영 계층에서 정수 수량만 전달해야 합니다.
- 지정가는 양수 `Price`를 요구하며 GTC·IOC·POC·FOK를 지원합니다. 시간 정책을 생략하면 GTC를 적용합니다.
- 시장가는 Gate.io 규칙대로 `price=0`, `tif=ioc`로 전송합니다.
- 단방향 전량 청산은 `Size="0"`, `Close=true`를 사용합니다. 양방향 전량 청산은 `AutoSizeCloseLong` 또는 `AutoSizeCloseShort`와 `ReduceOnly=true`를 사용합니다.
- `ClientOrderID`는 필수이며 `t-` 접두사 뒤 최대 28자의 영문자·숫자·밑줄·하이픈·점만 허용합니다. 상세 조회와 취소는 거래소 주문 ID와 사용자 주문 ID를 모두 받습니다.
- 자기 거래 방지는 미설정, `-`, CO·CN·CB 정책을 지원합니다. 포지션 증거금 방식은 미설정, isolated, cross를 지원합니다.

주문 생성·취소의 전송 오류, 성공 응답 본문 읽기 또는 JSON 해석 실패, 5xx 응답은 실제 처리 여부를 단정하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 자동 재시도하지 말고 고유한 `ClientOrderID`로 상태를 확인해야 합니다. 미체결 주문을 취소한 뒤에는 사용자 주문 ID로 조회할 수 있는 시간이 제한되므로 최종 거래소 주문 ID도 보존해야 합니다.

## 인증과 EIP

private 요청 서명은 Spot과 같은 Gate.io API v4 규칙을 사용합니다. HTTP method, `/api/v4` 전체 경로, 실제 정렬 query, SHA-512 본문 해시, Unix second를 줄바꿈으로 결합하고 API Secret으로 HMAC-SHA-512 서명합니다. 요청 제한 대기 뒤 Secret을 조회하고 최종 요청을 서명하며, 반환된 민감 byte slice는 요청 후 가능한 범위에서 덮어씁니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `gateio:route:<route>:futures-public:<endpoint>:10seconds` | 200회/10초 | 선택한 EIP route와 공개 endpoint |
| `gateio:account:<account>:futures-private:<endpoint>:10seconds` | 200회/10초 | UID와 private endpoint |
| `gateio:account:<account>:futures-order:1second` | 100회/초 | UID의 주문 생성 |
| `gateio:account:<account>:futures-cancel:1second` | 200회/초 | UID의 주문 취소 |

공개 bucket은 요청별 EIP마다 분리되지만 private·주문·취소 bucket은 UID를 기준으로 공유합니다. `X-Gate-RateLimit-*` 응답 헤더가 설정 quota와 일치하면 로컬 제한기의 관측 사용량과 reset 대기를 보정합니다. 계정 등급이나 거래 효율 기반 제한이 더 낮으면 `Config` quota를 보수적으로 재정의해야 합니다.

## 운영 검증

자동 테스트는 공개·private 전체 수명주기, 서명 원문과 JSON 본문 일치, 요청별 route, route 허용 목록 사전 차단, Secret 덮어쓰기, 요청 제한 분리, 정확한 ID·timestamp 해석, 주문 검증과 불명확한 mutation 상태를 검증합니다. 실제 Gate.io Futures 계정과 지정 EIP를 이용한 읽기·주문 smoke는 아직 대기 상태입니다.

## 공식 기준

- [Gate.io API v4 Perpetual Futures](https://www.gate.com/docs/developers/apiv4/en/futures/)
- [Gate.io API v4 인증·요청 제한](https://www.gate.com/docs/developers/apiv4/en/)
