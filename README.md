# Proven Trade SDK

여러 중앙화 거래소(CEX)의 REST/WebSocket API를 하나의 일관된 인터페이스로 제공하고, 요청별로 지정한 AWS Elastic IP를 통해 통신할 수 있게 하는 SDK 프로젝트입니다.

현재 Go 코어, REST·WebSocket 다중 EIP 전송 계층, Binance Spot·USDⓈ-M Futures REST·WebSocket, Bitget v3 UTA REST·WebSocket, Upbit Spot REST·WebSocket, Bybit V5 Spot·Linear REST·WebSocket, OKX V5 Spot·SWAP REST·WebSocket, Coinbase Advanced Trade Spot REST·WebSocket, Kraken Spot·Futures REST·WebSocket, Bithumb Spot REST·WebSocket, Coinone Spot REST·WebSocket, Korbit Spot REST·WebSocket, KuCoin Spot·Futures REST·WebSocket, Gate.io Spot REST·WebSocket·공통 API와 Futures REST·WebSocket, MEXC Spot REST·Protobuf WebSocket·로컬 오더북·공통 API, HTX Spot REST·WebSocket·로컬 오더북·공통 API, Crypto.com Exchange v1 Spot REST·public WebSocket·공통 API가 구현되어 있습니다.

## 문서

- [프로젝트 기획서](docs/PROJECT_PLAN.md)
- [거래소 지원 매트릭스](docs/SUPPORT_MATRIX.md)
- [Live smoke 운영 검증](docs/LIVE_SMOKE.md)
- [공통 WebSocket 연결 계층](docs/STREAMS.md)
- [Binance Spot WebSocket](docs/exchanges/BINANCE_WEBSOCKET.md)
- [Binance USDⓈ-M Futures REST·WebSocket](docs/exchanges/BINANCE_USDM.md)
- [Bybit V5 Spot·Linear REST](docs/exchanges/BYBIT.md)
- [OKX V5 Spot·SWAP REST](docs/exchanges/OKX.md)
- [Coinbase Advanced Trade Spot](docs/exchanges/COINBASE.md)
- [Kraken Spot REST·WebSocket v2](docs/exchanges/KRAKEN.md)
- [Kraken Futures REST·WebSocket v1](docs/exchanges/KRAKEN_FUTURES.md)
- [Bithumb Spot REST·WebSocket](docs/exchanges/BITHUMB.md)
- [Coinone Spot REST·WebSocket](docs/exchanges/COINONE.md)
- [Korbit Spot REST·WebSocket](docs/exchanges/KORBIT.md)
- [KuCoin Spot REST·WebSocket](docs/exchanges/KUCOIN.md)
- [KuCoin Futures REST·WebSocket](docs/exchanges/KUCOIN_FUTURES.md)
- [Gate.io API v4 Spot REST·WebSocket](docs/exchanges/GATEIO.md)
- [Gate.io API v4 Futures REST·WebSocket](docs/exchanges/GATEIO_FUTURES.md)
- [MEXC Spot V3 REST·Protobuf WebSocket](docs/exchanges/MEXC.md)
- [HTX Spot REST·WebSocket](docs/exchanges/HTX.md)
- [Crypto.com Exchange v1 Spot](docs/exchanges/CRYPTOCOM.md)

## 현재 기준

- 구현 거래소: Binance, Bitget, Upbit, Bybit, OKX, Coinbase, Kraken, Bithumb, Coinone, Korbit, KuCoin, Gate.io, MEXC, HTX, Crypto.com REST·public WebSocket
- 다음 구현 대상: Crypto.com Exchange v1 private user WebSocket
- 구현 언어: Go
- 네트워크: 단일 ENI의 여러 secondary private IPv4와 EIP 1:1 연결
- IP 선택: 클라이언트 기본값과 요청별 `egressRouteId` 재정의

## 구현 상태

아래 표는 요약입니다. 상품군별 구현·자동 테스트·실계정 smoke 상태의 기준 정보는 [자동 생성 지원 매트릭스](docs/SUPPORT_MATRIX.md)를 확인합니다.

| 영역 | 상태 |
|---|---|
| 공통 오류·자격증명·요청 옵션 | 구현됨 |
| private IP별 HTTP 연결 풀과 요청별 EIP 선택 | 구현됨 |
| 다차원 요청 제한기 | 구현됨 |
| Binance Spot REST | 공개 시세, 계정, 주문 생성·조회·취소·목록 구현됨 |
| Binance USDⓈ-M Futures REST | 공개 시세, 계정 V3, 포지션 V3, 주문 구현됨 |
| Binance USDⓈ-M Futures WebSocket | 분리 public·market 시세, listenKey private stream·로컬 오더북 자동 `pu` 갭 복구 구현됨 |
| Bitget v3 UTA | Spot·USDT-M 공개 시세, 자산, 포지션, 주문 구현됨 |
| Upbit Spot REST | 공개 시세, 잔고, 주문 생성·조회·취소·목록 구현됨 |
| 공통 Spot API·적합성 테스트 | Binance·Bitget·Upbit·Bybit·OKX·Coinbase·Kraken·Bithumb·Coinone·Korbit·KuCoin·Gate.io·MEXC·HTX 구현됨 |
| Spot live smoke CLI | 12개 Spot 어댑터의 지정 EIP·공개 조회·선택적 잔고 JSON 증적 구현됨 |
| Spot 주문 smoke 안전 계약 | post-only·금액 상한·EIP/호가 선검사·취소 정리 구현됨 |
| 공통 WebSocket 연결 계층 | route 고정, 재연결, 재구독 훅, heartbeat 구현됨 |
| Binance Spot WebSocket | public market·private user data stream·로컬 오더북 자동 갭 복구 구현됨 |
| Bitget v3 UTA WebSocket | Spot·USDT Futures public, UTA private stream·Spot 로컬 오더북 자동 갭 복구 구현됨 |
| Upbit Spot WebSocket | public 시세·private 내 주문·자산 stream·완전 snapshot 로컬 오더북 구현됨 |
| Bybit V5 REST | Spot·Linear 공개 시세, 통합 잔고, 포지션, 주문 구현됨 |
| Bybit V5 WebSocket | Spot·Linear public, Unified private stream·snapshot/delta 로컬 오더북 자동 복구 구현됨 |
| OKX V5 REST | Spot·SWAP 공개 시세, 거래 계정, 포지션, 주문 구현됨 |
| OKX V5 WebSocket | public 시세·business 캔들·private 계정 stream·로컬 오더북 자동 복구 구현됨 |
| Coinbase Advanced Trade REST | Spot 공개 시세, 계정, 주문·체결 구현됨 |
| Coinbase Advanced Trade WebSocket | public 시세·private user 주문 stream·level2 로컬 오더북 자동 갭 복구 구현됨 |
| Kraken Spot REST | 공개 시세, 계정, 주문·체결 구현됨 |
| Kraken Spot WebSocket v2 | public 시세·상품 규칙, private 주문·체결·잔고 stream·CRC32 로컬 오더북 자동 복구 구현됨 |
| Kraken Futures REST | 공개 시세·캔들, 지갑, 포지션, 주문·체결 구현됨 |
| Kraken Futures WebSocket v1 | public 시세·호가·체결, private 지갑·주문·체결·포지션 stream·로컬 오더북 자동 갭 복구 구현됨 |
| Bithumb Spot REST | 공개 시세, 잔고, v1 상세·v2 주문 생성·취소·목록 구현됨 |
| Bithumb WebSocket | public v1 시세·private v2 주문·자산 stream·완전 snapshot 로컬 오더북 구현됨 |
| Coinone Spot REST | public v2 시세, v2.1 잔고·주문·체결 구현됨 |
| Coinone WebSocket | public 시세·private 주문·자산 stream·source ID 검증 로컬 오더북 구현됨 |
| Korbit Spot REST | 공개 시세·상품 규칙, 잔고, 주문·체결 구현됨 |
| Korbit WebSocket | public 시세·private 주문·체결·자산 stream·완전 snapshot 로컬 오더북 구현됨 |
| KuCoin Classic Spot REST | 상품 규칙, 공개 시세, 계정, HF 주문 생성·조회·취소·미체결 목록 구현됨 |
| KuCoin Spot WebSocket | Classic public 시세·체결과 private 주문·잔고 stream, Pro Increment Best 500 로컬 오더북 자동 갭 복구 구현됨 |
| KuCoin Classic Futures REST | 계약 규칙, 공개 시세, 계정·포지션, 주문·체결 구현됨 |
| KuCoin Futures WebSocket | Classic public 시세·호가·캔들·체결과 private 주문·잔고·포지션 stream, Pro Increment Best 500 로컬 오더북 자동 갭 복구 구현됨 |
| Gate.io API v4 Spot REST | 거래쌍 규칙, 공개 시세, 계정, 주문 생성·조회·취소·미체결·체결 구현됨 |
| Gate.io API v4 Spot WebSocket | public 시세·호가·체결, private 주문·체결·잔고 stream·V2 로컬 오더북 자동 갭 복구 구현됨 |
| Gate.io 공통 Spot API | 공통 마켓·시세·잔고·주문 계약과 적합성 테스트 구현됨 |
| Gate.io API v4 Futures REST | 계약 규칙, 공개 시세, 계정·포지션, 주문·체결 구현됨 |
| Gate.io API v4 Futures WebSocket | public 시세·호가·캔들·체결, private 주문·체결·잔고·포지션 stream·V2 로컬 오더북 자동 갭 복구 구현됨 |
| MEXC Spot V3 REST·WebSocket·공통 API | 공개 시세, API Key 허용 거래쌍, 계정·주문 REST, public/private Protobuf stream, version 로컬 오더북, 공통 Spot 계약과 요청별 EIP 구현됨 |
| HTX Spot REST·공통 API | 공개 시세, 계정·잔고, 주문 생성·조회·취소, 미체결·주문·체결 이력, 공통 Spot 계약과 요청별 EIP 구현됨 |
| HTX Spot public WebSocket | gzip JSON ticker·호가·BBO·체결·캔들, 서버 ping 응답, 동적 구독과 같은 EIP 재연결 복구 구현됨 |
| HTX Spot private WebSocket | v2 HMAC 인증·재인증, 주문·체결·계정 stream, plain JSON ping 응답, 요청 제한과 같은 EIP 구독 복구 구현됨 |
| HTX Spot MBP 로컬 오더북 | `/feed` 5·20·150단계 refresh·증분 정렬, sequence gap 재동기화와 같은 EIP 복구 구현됨 |
| Crypto.com Exchange v1 Spot REST | 공개 시세, 계정 잔고, 주문 생성·조회·취소, 미체결·주문·체결 이력과 요청별 EIP 구현됨 |
| Crypto.com 공통 Spot API | 상품·시세·호가·체결·캔들·잔고·주문 계약과 요청별 EIP 전달 적합성 구현됨 |
| Crypto.com public WebSocket | ticker·체결·캔들·10/50단계 호가, heartbeat, 동적 구독과 같은 EIP 재연결 복구 구현됨 |

## 요청별 EIP 선택

AWS에서는 EIP 자체가 아니라 EIP에 연결된 secondary private IPv4를 소켓의 local address로 사용합니다. 아래처럼 private IP별 route를 한 번 등록하면 각 route가 독립된 연결 풀을 갖습니다.

```go
routes := []transport.EgressRoute{
	{
		ID:               "seoul-a",
		LocalPrivateIP:   net.ParseIP("10.0.10.21"),
		ExpectedPublicIP: net.ParseIP("203.0.113.10"),
	},
	{
		ID:               "seoul-b",
		LocalPrivateIP:   net.ParseIP("10.0.10.22"),
		ExpectedPublicIP: net.ParseIP("203.0.113.11"),
	},
}

registry, err := transport.NewRegistry(routes)
if err != nil {
	return err
}
defer registry.Close()

limiter, err := ratelimit.New()
if err != nil {
	return err
}
executor, err := exchange.NewExecutor(exchange.ExecutorConfig{
	Sender:  registry,
	Limiter: limiter,
})
if err != nil {
	return err
}

client, err := binance.New(binance.Config{
	Executor:             executor,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

ticker, err := client.TickerPrice(
	ctx,
	binance.TickerPriceRequest{Symbol: "BTCUSDT"},
	trade.WithEgressRoute("seoul-b"),
)
```

private API는 `credential.Descriptor`와 Secret 저장소를 읽는 `credential.Provider`를 추가합니다. 자격증명에 허용하지 않은 route는 Secret을 읽거나 네트워크 요청을 보내기 전에 거부됩니다.

## 개발 명령

```bash
go run ./cmd/supportgen -check
go test ./...
go test -race ./...
go vet ./...
```

## 다중 EIP 진단

EC2에 연결한 EIP와 private IP의 실제 송신 관계는 `egressdiag`로 확인합니다.

```bash
go run ./cmd/egressdiag \
  -route seoul-a,10.0.10.21,203.0.113.10 \
  -route seoul-b,10.0.10.22,203.0.113.11
```

자세한 내용은 [다중 EIP 진단 예제](examples/multi-eip/README.md)를 참고합니다.

## WebSocket 연결별 EIP 선택

`stream.Session`은 WebSocket 최초 연결과 모든 재연결을 하나의 `egressRouteId`에 고정합니다. 임시 인증 endpoint 갱신, 연결 직후 재구독, 지수 backoff, `Retry-After`, ping과 상태 관측을 지원합니다.

```go
connector, err := stream.NewWebSocketConnector(stream.ConnectorConfig{
	HTTPClients: registry,
})
if err != nil {
	return err
}

session, err := stream.NewSession(stream.SessionConfig{
	Connector:     connector,
	EgressRouteID: "seoul-b",
	Request: stream.DialRequest{
		Endpoint: "wss://stream.example.com/ws",
	},
})
if err != nil {
	return err
}
defer session.Close()
```

이미 연결된 WebSocket의 route는 변경할 수 없습니다. 자세한 수명주기와 private stream 인증 규칙은 [공통 WebSocket 연결 계층 문서](docs/STREAMS.md)를 참고합니다.

## Binance Spot 1차 범위

- 연결 확인과 서버 시간 보정
- 거래소 상품 정보와 동적 요청 제한 규칙
- 현재가, 호가, 최근 체결, 최우선 호가, 캔들
- 계정 잔고
- 주문 생성, 단건 조회, 취소, 미체결 목록, 주문 이력
- 공식 HMAC SHA-256 서명 벡터
- IP·계정 단위 요청 제한과 `Retry-After` 반영
- 주문 mutation의 불명확한 네트워크 결과를 `UNKNOWN_EXECUTION_STATE`로 분류

현재 지원 범위는 개발 중인 초기 API이며 아직 안정 버전 호환성을 보장하지 않습니다.

## Binance USDⓈ-M Futures REST·WebSocket 범위

- 서버 시간 보정과 계약 정보
- 현재가, 호가, 최근 체결, 캔들
- 계정 V3 자산과 포지션 V3 위험 정보
- 주문 생성, 조회, 취소, 미체결 목록, 주문 이력
- 단방향·헤지 모드 `positionSide`, `reduceOnly`, `closePosition`
- 지정가, 시장가, Stop, Take Profit, Trailing Stop 주문 검증
- IP 요청 weight와 계정 주문 count 분리
- HTTP 503 응답 문구별 불명확한 실행 상태 분류
- 2026년 분리 진입점 기반 aggregate trade·마크가·캔들·ticker·최우선 호가·호가 stream
- public·market 동적 구독과 같은 EIP 재연결 시 구독 자동 복구
- REST listenKey 발급·갱신과 private 계정·포지션·주문·마진콜 stream
- listenKey 만료·갱신 실패 시 같은 EIP로 새 키를 발급하는 자동 재연결
- 같은 EIP의 REST snapshot과 diff depth를 결합한 로컬 오더북·`pu` sequence gap 자동 복구

세부 계약과 주의사항은 [Binance USDⓈ-M Futures 문서](docs/exchanges/BINANCE_USDM.md)를 참고합니다.

## Binance Spot WebSocket 1차 범위

- public combined market stream과 동적 구독·구독 해제
- 재연결 시 현재 구독 목록 자동 복구
- aggregate trade, trade, kline, ticker, book ticker, depth typed event
- WebSocket API HMAC 서명 기반 private user data 구독
- 계정 잔고, 잔고 변화, 주문·체결, 외부 잠금 typed event
- 연결별 EIP 선택과 자격증명 route 허용 검사
- 초당 수신 메시지 제한을 고려한 구독 명령 직렬화
- 같은 EIP의 REST snapshot과 diff depth를 결합한 로컬 오더북·sequence gap 자동 복구

세부 계약과 주의사항은 [Binance Spot WebSocket 문서](docs/exchanges/BINANCE_WEBSOCKET.md)를 참고합니다.

## Bitget v3 UTA 1차 범위

- Spot·USDT-M Futures 상품 정보, 현재가, 호가, 최근 체결, 캔들
- 통합 계정 자산과 USDT-M Futures 포지션
- Spot·USDT-M Futures 주문 생성, 조회, 취소, 미체결 목록, 주문 이력
- HMAC SHA-256 및 Base64 서명과 Passphrase 인증
- 전체 `6000/IP/분` 및 endpoint별 `IP 또는 UID/초` 요청 제한
- Demo Trading의 `paptrading: 1` 헤더 옵션
- 불명확한 주문 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- Spot `books` snapshot과 `pseq`·`seq` 기반 로컬 오더북·같은 EIP 자동 갭 복구

세부 계약과 주의사항은 [Bitget v3 UTA 문서](docs/exchanges/BITGET.md)를 참고합니다.

## Upbit Spot 1차 범위

- 전체 마켓, 현재가, 호가, 최근 체결, 분봉
- 계정 잔고
- 주문 생성, 단건 조회, 취소, 미체결 목록, 종료 목록
- HS512 JWT와 SHA-512 `query_hash`, 요청별 고유 nonce
- IP 단위 공개 API와 계정 pocket 단위 private API 요청 제한
- `Remaining-Req`, HTTP 429·418 제한 상태 반영
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- 완전 snapshot 기반 로컬 오더북, `SIMPLE_LIST`, level·1/5/15/30단계 구독 지원

세부 계약과 주의사항은 [Upbit Spot 문서](docs/exchanges/UPBIT.md)를 참고합니다.

## Bybit V5 Spot·Linear 1차 범위

- 서버 시간 보정과 Spot·Linear 상품 정보
- 현재가, 호가, 최근 체결, 캔들
- Unified 계정 잔고와 Linear 포지션
- 주문 생성, 단건 조회, 취소, 미체결 목록, 주문 이력
- HMAC SHA-256 서명과 `X-BAPI-*` 인증 헤더
- route 단위 IP 제한과 계정 단위 private endpoint 제한
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- Spot·Linear ticker, 호가, 공개 체결, 캔들 WebSocket
- Unified 주문, 체결, Linear 포지션, 지갑 private WebSocket
- 연결별 EIP 고정, application heartbeat, 재인증·재구독
- Spot·Linear snapshot/delta 로컬 오더북과 update ID gap 시 같은 EIP 자동 재연결

세부 계약과 주의사항은 [Bybit V5 문서](docs/exchanges/BYBIT.md)를 참고합니다.

## OKX V5 Spot·SWAP 1차 범위

- 서버 시간 보정과 Spot·SWAP 상품 정보
- 전체 ticker, 호가, 최근 체결, 캔들
- 거래 계정 잔고와 SWAP 포지션
- 주문 생성, 단건 조회, 취소, 미체결 목록, 최근 7일 이력
- UTC ISO-8601 timestamp, HMAC SHA-256, Base64 서명, Passphrase 인증
- Demo Trading 헤더와 지역별 `BaseURL` 재정의
- IP·사용자·사용자+상품 단위 요청 제한 분리
- 주문별 `sCode` 오류와 불명확한 mutation 결과 정규화
- public ticker·호가·체결, business 캔들 WebSocket
- private 계정·잔고/포지션·주문 WebSocket
- 연결별 EIP 고정, application heartbeat, 재로그인·재구독
- `books` prevSeqId/seqId 로컬 오더북과 `books5`·`bbo-tbt` snapshot, 같은 EIP 자동 갭 복구

세부 계약과 주의사항은 [OKX V5 문서](docs/exchanges/OKX.md)를 참고합니다.

## Coinbase Advanced Trade Spot 1차 범위

- 공개 서버 시간, Spot 상품, 현재가, 호가, 최근 체결, 캔들
- cursor 기반 계정 잔고, 주문 목록, 체결 목록
- 시장가 IOC와 지정가 GTC 주문 생성, 단건 조회, 최대 100건 일괄 취소
- CDP ECDSA P-256 key 기반 요청별 ES256 JWT
- 공개 REST의 1초 cache를 우회하는 `Cache-Control: no-cache`
- route·계정 단위의 보수적 로컬 요청 제한과 설정별 재정의
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public ticker·ticker batch·체결·호가·캔들·상품 상태 WebSocket
- private user 주문 WebSocket과 자동 heartbeat
- 연결별 EIP 고정, 새 JWT 재인증, 재구독
- level2 snapshot·절대 수량 update 로컬 오더북과 sequence gap 시 같은 EIP 자동 재연결

세부 계약과 자격증명 저장 형식은 [Coinbase Advanced Trade 문서](docs/exchanges/COINBASE.md)를 참고합니다.

## Kraken Spot REST·WebSocket v2 1차 범위

- 서버 시간, Spot 상품 규칙, ticker, L2 호가, 최근 체결, OHLCV
- 자산별 총 잔고
- 시장가·지정가 주문 생성, 주문 조회·취소, 미체결·종료 주문 목록
- 계정 체결 이력
- Base64 secret 기반 SHA-256과 HMAC-SHA-512 `API-Sign`
- 단일 클라이언트에서 동시 요청에도 항상 증가하는 millisecond nonce
- 공개 EIP, private API key counter, 계정+상품 주문 제한 분리
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public ticker·L2 호가·체결·OHLC·상품 규칙 WebSocket
- private 주문·체결 `executions`와 자산 `balances` WebSocket
- 연결별 EIP 고정, 같은 route의 REST token 재발급, 재연결·재구독
- book snapshot·update 로컬 오더북과 CRC32 불일치 시 같은 EIP 자동 재연결

세부 계약과 제한·연결 정책은 [Kraken Spot 문서](docs/exchanges/KRAKEN.md)를 참고합니다.

## Kraken Futures REST·WebSocket v1 1차 범위

- Futures 상품 규칙, 전체 ticker, 전체 L2 호가, 최근 공개 체결, OHLCV 캔들
- cash·margin·multi-collateral 지갑, 열린 포지션
- 시장가·지정가·post-only·IOC·FOK 주문 생성, 취소, 미체결 주문, 최근 주문 상태
- 체결 이력과 `lastFillTime` 비용 차등 적용
- URL query 원문과 nonce를 이용한 SHA-256 및 HMAC-SHA-512 `Authent`
- 단일 클라이언트에서 동시 요청에도 항상 증가하는 millisecond nonce
- 공개 EIP별 제한과 private 계정별 derivatives point pool 분리
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public ticker·ticker lite·L2 호가·체결 WebSocket
- private 잔고·체결·미체결 주문·포지션·계정 원장·운영 알림 WebSocket
- 연결별 EIP 고정, challenge 재발급·재서명, 재연결·재구독
- book_snapshot·단일 레벨 update 로컬 오더북과 sequence gap 시 같은 EIP 자동 재연결

세부 계약과 제한·연결 정책은 [Kraken Futures 문서](docs/exchanges/KRAKEN_FUTURES.md)를 참고합니다.

## Bithumb Spot REST·WebSocket 1차 범위

- 전체 마켓, 현재가, 호가, 최근 체결, 분봉
- 코인별 잔고
- v2 주문 생성·취소·미체결·이력과 v1 주문 상세 조회
- HS256 JWT, millisecond timestamp, SHA-512 원문 쿼리 해시
- 공개 route 150회/초, private 계정 140회/초, 주문 계정 10회/초 제한 분리
- 지정가, 매수·매도 시장가, KRW 최유리 주문 검증
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public v1 ticker·체결·호가 WebSocket
- private v2 내 주문·체결·자산 WebSocket
- 연결별 EIP 고정, 새 HS256 JWT와 ticket을 이용한 재인증·재구독
- 최대 15호가의 완전 snapshot 기반 로컬 오더북과 동일 EIP 재연결 복구

세부 계약과 REST·WebSocket v1/v2 endpoint 구분은 [Bithumb Spot 문서](docs/exchanges/BITHUMB.md)를 참고합니다.

## Coinone Spot REST·WebSocket 1차 범위

- 기준 통화별 마켓, 현재가, 호가, 최근 체결, 캔들
- 통화별 잔고
- 시장가·지정가·스탑 지정가 주문 생성, 상세, 취소, 미종료·종료 목록
- 정확한 JSON bytes의 Base64 payload와 HMAC-SHA512 hex 서명
- 공개 route 1,200회/분, private 포트폴리오 80회/초, 주문 40회/초 제한 분리
- 응답 remaining 헤더를 이용한 로컬 요청 제한 상태 보정
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public 호가·ticker·체결·캔들 WebSocket
- private 내 주문·자산 WebSocket
- DEFAULT·SHORT typed event, 실행 중 구독 변경, 재연결 구독 복구
- 연결별 EIP 고정, 재연결마다 private handshake 재서명
- JSON PING/PONG 기반 30분 세션 만료 갱신
- 최대 16호가의 완전 snapshot과 source ID 기반 최신성 검증 로컬 오더북

세부 인증·주문·요청 제한·연결 계약은 [Coinone Spot 문서](docs/exchanges/COINONE.md)를 참고합니다.

## Korbit Spot REST·WebSocket 1차 범위

- 서버 시각, 전체 거래쌍과 주문 금액·호가 단위 정책
- 현재가, 호가, 최근 체결, 캔들
- 하위 계정별 잔고
- 지정가·시장가·최유리호가 주문 생성, 상세, 취소, 미체결·최근 주문·체결 목록
- HMAC-SHA256 hex 및 PKCS#8 ED25519 Base64 서명
- URL 인코딩 파라미터 원문과 동일한 query·form 본문 전송
- 공개 route 50회/초, private 계정 50회/초, 주문 생성·취소 각 30회/초 제한 분리
- `Ratelimit` 응답 헤더를 이용한 로컬 요청 제한 상태 보정
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 고유한 `ClientOrderID` 강제와 주문 mutation의 불명확한 결과 분류
- public ticker·호가·체결 WebSocket
- private 주문·체결·자산 WebSocket
- 실행 중 구독 변경, 실패 ack 반영, 재연결 구독 복구
- 연결별 EIP 고정, 재연결마다 private handshake 재서명
- public 유실과 private 재연결 구간을 위한 REST 재조정 계약
- 최대 30호가의 완전 snapshot 기반 로컬 오더북과 묶음 level·동일 EIP 검증

세부 인증·주문·요청 제한·연결 계약은 [Korbit Spot 문서](docs/exchanges/KORBIT.md)를 참고합니다.

## KuCoin Spot REST·WebSocket 1차 범위

- 전체 거래쌍 규칙, 현재가, 20·100단계 호가, 최근 체결, 캔들
- Classic 계정 유형별 잔고
- 지정가·시장가 주문 생성, 상세, 취소, 페이지 기반 미체결 목록
- HMAC-SHA256 Base64 요청 서명과 API Key 버전 2 Passphrase 서명
- Public IP, Spot UID, Management UID 30초 weight pool 분리
- `gw-ratelimit-*` 응답 헤더를 이용한 로컬 요청 제한 상태 보정
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 폐기된 active 목록을 제외하고 현재 `active/page` endpoint 사용
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public ticker·증분 호가·5/50단계 호가·캔들·체결 WebSocket
- private 주문 V2·잔고 WebSocket
- 실행 중 구독 변경, 실패 응답 반영, 재연결 구독 복구
- 연결별 EIP 고정과 재연결마다 같은 route를 통한 token 재발급
- KuCoin JSON ping/pong heartbeat와 서버가 발급한 연결 제한 검증
- 현행 Pro `obu.SPOT` Increment Best 500 snapshot/delta 로컬 오더북과 sequence gap 시 같은 EIP 자동 재연결

세부 인증·주문·요청 제한·연결 계약은 [KuCoin Spot 문서](docs/exchanges/KUCOIN.md)를 참고합니다.

## KuCoin Futures REST·WebSocket 1차 범위

- 전체·단일 계약 규칙, 현재가, 20·100단계 호가, 최근 체결, 캔들
- 결제 통화별 계정 요약과 열린 포지션
- 계약 수량 기반 지정가·시장가 주문 생성, 상세, 취소, 미체결·체결 페이지
- 격리·교차 증거금과 단방향·헤지 포지션 방향
- HMAC-SHA256 Base64 요청 서명과 API Key 버전 2 Passphrase 서명
- Public IP와 Futures UID 30초 weight pool 분리
- `gw-ratelimit-*` 응답 헤더를 이용한 로컬 요청 제한 상태 보정
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public Ticker V2·증분 호가·5/50단계 호가·캔들·체결 WebSocket
- private 주문·잔고·전체 또는 단일 계약 포지션 WebSocket
- 실행 중 구독 변경, 실패 응답 반영, 재연결 구독 복구
- 연결별 EIP 고정과 재연결마다 같은 route를 통한 token 재발급
- KuCoin JSON ping/pong heartbeat와 서버가 발급한 연결 제한 검증
- 현행 Pro `obu.FUTURES` Increment Best 500 snapshot/delta 로컬 오더북과 sequence gap 시 같은 EIP 자동 재연결

세부 인증·주문·요청 제한·연결 계약은 [KuCoin Futures 문서](docs/exchanges/KUCOIN_FUTURES.md)를 참고합니다.

## Gate.io API v4 Spot REST·WebSocket·공통 API 범위

- 전체 거래쌍 규칙, 현재가, 호가, 최근 체결, 캔들
- 통화별 Spot 잔고
- 지정가·시장가 주문 생성, 상세, 취소, 거래쌍별 미체결 목록, 내 체결
- SHA-512 본문 해시와 HMAC-SHA-512 hex 요청 서명
- 공개 EIP+endpoint, private UID+endpoint, 주문 UID+거래쌍, 취소 UID 제한 분리
- `X-Gate-RateLimit-*` 응답 헤더를 이용한 로컬 요청 제한 상태 보정
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 고유한 `t-` 사용자 주문 ID 강제와 시장가 매수·매도의 수량 의미 검증
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- public ticker·체결·캔들·최우선 호가·증분 호가 WebSocket
- private 주문·내 체결·Spot 잔고 WebSocket
- 실행 중 구독 변경, 실패 응답 반영, 재연결 구독 복구
- 연결별 EIP 고정과 private 구독마다 새 HMAC-SHA-512 서명
- WebSocket protocol ping/pong heartbeat와 IP당 연결 수 운영 계약
- Spot `spot.obu` 50·400단계 snapshot/증분 로컬 오더북과 update ID gap 시 같은 EIP 자동 재연결
- `unified.SpotClient` 전체 계약과 공통 적합성 테스트

세부 인증·주문·요청 제한·연결 계약은 [Gate.io API v4 Spot 문서](docs/exchanges/GATEIO.md)를 참고합니다.

## Gate.io API v4 Futures REST·WebSocket 범위

- USDT·BTC·USD1 결제 통화별 무기한 계약 규칙과 단일 계약 조회
- 현재가·마크가·지수가, 호가, 공개 체결, 캔들
- 결제 통화별 계정 요약과 격리·교차 및 단방향·양방향 포지션
- signed decimal 계약 수량 기반 지정가·시장가·reduce-only·전량 청산 주문
- 주문 상세·취소·상태별 목록과 계정 체결 페이지
- SHA-512 본문 해시와 HMAC-SHA-512 hex 요청 서명
- public EIP+endpoint, private UID+endpoint, 주문·취소 UID 제한 분리
- 요청별 EIP 선택과 자격증명 route 허용 검사
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- BTC·USDT·USD1 정산 통화별 public ticker·체결·캔들·최우선 호가·증분 호가 WebSocket
- private 주문·계정 체결·잔고·포지션 WebSocket과 계약별 또는 `!all` 구독
- 20ms·100ms 증분 호가, 소수 수량, 실행 중 구독 변경과 실패 응답 rollback
- 연결별 EIP 고정, 재연결 구독 복구와 private 구독마다 새 HMAC-SHA-512 서명
- `X-Gate-Size-Decimal: 1` handshake와 `futures.obu` 50·400단계 로컬 오더북·동일 EIP 갭 복구

세부 계약은 [Gate.io API v4 Futures 문서](docs/exchanges/GATEIO_FUTURES.md)를 참고합니다.

## MEXC Spot V3 범위

- 서버 시각과 API 기본 허용 거래쌍
- 전체·단일·복수 거래쌍 규칙과 주문 정밀도
- 최대 5000단계 호가 snapshot, 최근·합산 체결, 캔들
- 평균가, 24시간 통계, 최근가, 최우선 호가
- public IP+endpoint별 500 weight/10초 제한과 `Retry-After` 차단
- 요청별 EIP 선택과 route별 독립 limiter
- 숫자·문자열·`null`이 혼재하는 식별자 원형과 응답 `Raw` 보존
- HTTP 상태와 MEXC code 기반 공통 오류 정규화
- HMAC-SHA256 소문자 hex private 서명과 API Key 허용 EIP 사전 검사
- API Key 허용 거래쌍, 계정·잔고, 주문 생성·상세·취소·미체결·이력·계정 체결
- UID별 주문 5회/초, 취소·읽기 50회/초, 계정 2회/초의 보수적 제한
- 필수 사용자 주문 ID와 시장가 매수·매도 수량 의미 검증
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류
- 공통 마켓·시세·잔고·주문 계약, 적합성 테스트와 요청별 EIP 전달
- 공통 3분봉 합성, 최대 5개 허용 심볼 묶음의 전체 미체결 조회
- 10ms·100ms 합산 체결, 증분 호가, 최우선 호가와 캔들·부분 호가 Protobuf WebSocket
- private 잔고·체결·주문 Protobuf WebSocket과 API Key 전용 listenKey REST 수명주기
- 연결별 EIP 고정, JSON PING, 실행 중 구독 변경·실패 rollback과 재연결 구독 복구
- listenKey 발급·30분 갱신·WebSocket 재연결을 동일 EIP route로 강제
- REST 최대 5000단계 snapshot과 diff version 범위 결합, 정확한 연속성 검사와 동일 EIP 갭 복구

세부 계약은 [MEXC Spot V3 문서](docs/exchanges/MEXC.md)를 참고합니다.

## 공통 Spot API

`unified.SpotClient`는 Binance, Bitget, Upbit, Bybit, OKX, Coinbase, Kraken, Bithumb, Coinone, Korbit, KuCoin, Gate.io, MEXC, HTX에 같은 메서드 계약을 제공합니다. 마켓은 거래소 문자열 대신 `Base`와 `Quote`로 지정하며, 요청별 EIP 옵션은 native API와 동일하게 전달합니다.

```go
spot, err := upbit.NewUnifiedSpot(client)
if err != nil {
	return err
}

ticker, err := spot.Ticker(
	ctx,
	unified.TickerRequest{
		Market: unified.Market{Base: "BTC", Quote: "KRW"},
	},
	trade.WithEgressRoute("seoul-b"),
)
```

지원 계약과 시장가 주문의 수량 의미는 [공통 Spot API 문서](docs/UNIFIED_SPOT.md)를 참고합니다.
