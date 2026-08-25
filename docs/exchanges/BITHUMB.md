# Bithumb Spot REST·WebSocket 어댑터

구현 기준은 빗썸 Developer Docs 2.1.5와 REST 기본 주소 `https://api.bithumb.com`입니다. 현재 빗썸 주문 API는 v1과 v2가 함께 사용되므로 SDK도 endpoint별 버전을 그대로 유지합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Bithumb 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

Secret Key는 Base64 디코딩하지 않고 HS256 서명 키로 직접 사용합니다. `credential.Descriptor.AccountID`에는 같은 빗썸 계정의 요청 제한을 공유할 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다.

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

SDK 기본 로컬 제한은 공식 안내의 공개 150회/초, private 140회/초, 주문 관련 API 10회/초입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `bithumb:route:<route>:public:1second` | 150회/초 | 선택한 송신 경로 |
| `bithumb:account:<account>:private:1second` | 140회/초 | 빗썸 계정 |
| `bithumb:account:<account>:order:1second` | 10회/초 | 빗썸 계정의 주문 API |

주문 관련 private 요청은 private bucket과 order bucket을 동시에 소비합니다. 제한값은 `Config`의 `PublicRequestsPerSecond`, `PrivateRequestsPerSecond`, `OrderRequestsPerSecond`로 더 보수적으로 조정할 수 있습니다.

여러 공인 송신 IP를 사용해도 계정 단위 제한은 늘어나지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

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

## 현재 제외 범위

주문 가능 정보, 일·주·월 캔들, 다건 주문·취소·조회, TWAP, 입출금은 현재 범위에 포함하지 않습니다.

## 공식 기준 문서

- [Bithumb Developer Docs](https://apidocs.bithumb.com/docs/빗썸-developer-docs)
- [인증 토큰 생성](https://apidocs.bithumb.com/docs/인증-토큰-생성하기)
- [API 요청 수 제한](https://apidocs.bithumb.com/docs/api-요청-수-제한-안내)
- [주문 요청](https://apidocs.bithumb.com/reference/주문-요청)
- [개별 주문 조회](https://apidocs.bithumb.com/reference/개별-주문-조회)
- [주문 취소 접수](https://apidocs.bithumb.com/reference/주문-취소-접수)
- [대기 주문 목록 조회](https://apidocs.bithumb.com/reference/대기-주문-목록-조회)
- [종료 주문 목록 조회](https://apidocs.bithumb.com/reference/종료-주문-목록-조회)
- [WebSocket 기본 정보](https://apidocs.bithumb.com/reference/기본-정보)
- [WebSocket 호가](https://apidocs.bithumb.com/reference/호가-orderbook)
- [WebSocket 연결 관리](https://apidocs.bithumb.com/reference/연결-관리)
- [Private v2 MyOrder](https://apidocs.bithumb.com/reference/내-주문-및-체결-myorder)
- [Private v2 MyAsset](https://apidocs.bithumb.com/reference/내-자산-myasset)
- [Private v2 마이그레이션](https://apidocs.bithumb.com/reference/private-v2-마이그레이션-가이드)
