# HTX Spot REST 어댑터

## 기준

- 공식 문서: [HTX Spot API](https://huobiapi.github.io/docs/spot/v1/en/)
- REST 기본 호스트: `https://api.huobi.pro`
- AWS 최적화 REST 호스트: `https://api-aws.huobi.pro`
- 일반 시세 WebSocket: `wss://api.huobi.pro/ws`
- MBP 증분 WebSocket: `wss://api.huobi.pro/feed`
- 계정·주문 WebSocket: `wss://api.huobi.pro/ws/v2`
- 상품: Spot

공식 문서는 testnet이 중단되었다고 명시한다. 따라서 mock 서버 기반 자동 테스트를 구현 완료 기준으로 사용하고, 실제 계정과 지정 EIP가 필요한 검증은 지원 매트릭스의 별도 smoke 상태로 관리한다.

현재 단계는 `exchange/htx`의 공개 REST 구현이다. private REST, 공통 Spot API, WebSocket과 로컬 오더북은 아래 순서대로 후속 구현하며 전체 REST 완료 전까지 지원 매트릭스의 REST 상태는 `예정`으로 유지한다.

## 구현 범위

### 공개 REST

- 서버 시간과 거래 가능 상품 구현 완료
- 단일·전체 ticker와 최우선 호가 구현 완료
- 호가 snapshot 구현 완료
- 최근·최신 체결과 캔들 구현 완료
- 각 호출의 `egressRouteId` 선택과 route별 연결 풀 구현 완료

| 영역 | 메서드 | API |
|---|---|---|
| 서버 시각 | `ServerTime` | `GET /v1/common/timestamp` |
| 거래쌍 규칙 | `MarketSymbols` | `GET /v1/settings/common/market-symbols` |
| 단일 ticker | `Ticker` | `GET /market/detail/merged` |
| 전체 ticker | `Tickers` | `GET /market/tickers` |
| 호가 snapshot | `OrderBook` | `GET /market/depth` |
| 최신 체결 | `LatestTrade` | `GET /market/trade` |
| 최근 체결 | `RecentTrades` | `GET /market/history/trade` |
| 캔들 | `Candles` | `GET /market/history/kline` |

`MarketSymbols`는 전체 거래쌍, 쉼표로 결합한 복수 거래쌍, `ts` 증분 기준을 지원한다. `OrderBook`의 depth는 공식 허용값 5·10·20 또는 생략만 받고, 가격 집계는 `step0`부터 `step5`까지 지원한다. 최근 체결과 캔들 크기는 생략하거나 1~2000이다.

HTX가 가격·수량을 JSON 숫자와 문자열 양쪽으로 반환하므로 `Decimal` 타입이 원본 표기를 문자열로 보존한다. 큰 체결 ID도 `Scalar`로 받아 정수 범위를 넘는 값이 손실되지 않는다. 호가 배열은 필드 개수를 검증하며 각 거래쌍·ticker·체결·캔들의 `Raw`에 해당 JSON 원본을 보존한다.

## 공개 요청 예제

```go
client, err := htx.New(htx.Config{
	Executor:             executor,
	DefaultEgressRouteID: "seoul-a",
	BaseURL:              htx.DefaultAWSBaseURL,
})
if err != nil {
	return err
}

book, err := client.OrderBook(
	ctx,
	htx.OrderBookRequest{Symbol: "btcusdt", Depth: 20, Type: htx.DepthStep0},
	trade.WithEgressRoute("seoul-b"),
)
```

기본 route와 요청별 route 재정의는 공통 전송 계층의 서로 다른 private IP 연결 풀을 사용한다. `BaseURL`에는 공식 AWS 최적화 호스트도 지정할 수 있고, 이후 private 서명은 실제 설정한 요청 호스트를 기준으로 계산한다.

## 공개 요청 제한과 오류

공식 문서가 public market endpoint별 고정 숫자를 제시하지 않으므로 SDK는 기본 10회/초의 보수적인 로컬 한도를 각 `(route, endpoint)` bucket에 적용한다. `Config.PublicRequestsPerSecond`로 더 낮은 운영값을 지정할 수 있다. HTX가 `X-HB-RateLimit-Requests-Remain`과 `X-HB-RateLimit-Requests-Expire`를 반환하면 로컬 상태를 보정하고, HTTP 429와 `Retry-After`는 해당 route·endpoint를 차단한다.

응답의 HTTP 상태와 `status`, `err-code`, `err-msg`, `code`, `message`를 함께 검사한다. 요청 제한, 인증, 권한, 잔고 부족, 주문 없음, 거래소 장애를 공통 오류로 분류하면서 HTX 원본 코드·메시지와 `request-id`를 보존한다. 성공 HTTP 응답이 비어 있거나 JSON 구조가 깨진 경우 공개 조회는 재시도 가능한 거래소 장애로 분류한다.

### private REST

- 현물 계정 탐색과 잔고
- 주문 생성·단건 조회·취소
- 미체결 주문과 주문·체결 이력
- HMAC SHA-256, Base64 서명과 ASCII 순서 쿼리 정규화
- API Key에 허용되지 않은 route를 Secret 조회 전에 거부
- 주문 mutation의 전송 불명확 상태를 `UNKNOWN_EXECUTION_STATE`로 분류

### 공통 Spot API

- `Base`와 `Quote`를 HTX 소문자 결합 심볼로 변환
- 상품 규칙, ticker, order book, candle, 잔고, 주문 계약 정규화
- 거래소 원본 상태와 응답은 민감 정보를 제외하고 보존

### WebSocket

- gzip JSON public 시세 구독과 서버 ping 응답
- v2 private 인증과 주문·잔고 구독
- 재연결 시 같은 EIP 유지와 현재 구독 자동 복구
- MBP 증분의 sequence를 검증하는 로컬 오더북과 같은 EIP REST snapshot 복구

## 구현 순서

1. 공개 REST, 오류 정규화, 요청 제한과 mock 테스트 완료
2. private REST, signer golden vector와 주문 안전 계약
3. 공통 Spot API와 적합성 테스트
4. public/private WebSocket과 gzip·heartbeat 계약
5. MBP 로컬 오더북과 sequence gap 복구
6. 실제 계정 read-only 및 명시적 소액 주문 smoke

각 코드 단계는 전체 formatter, 생성물 검사, 일반·race 테스트, vet, 한글 주석 검사를 통과한 뒤 별도 커밋으로 푸시한다.

## 운영 제약

- API Key는 공식 정책이 허용하는 IP에 바인딩한다.
- SDK의 여러 EIP 경로는 제한 우회가 아니라 키 허용 목록 분리와 가용성 목적으로만 사용한다.
- 지역 제한을 우회하는 endpoint나 프록시 기능은 제공하지 않는다.
- AWS 호스트 사용 여부는 endpoint 설정으로 명시하며, 서명에는 실제 요청 호스트를 사용한다.
- 입출금 실행은 프로젝트 비목표이므로 구현하지 않는다.
