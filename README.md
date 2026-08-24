# Proven Trade SDK

여러 중앙화 거래소(CEX)의 REST/WebSocket API를 하나의 일관된 인터페이스로 제공하고, 요청별로 지정한 AWS Elastic IP를 통해 통신할 수 있게 하는 SDK 프로젝트입니다.

현재 Go 코어, 다중 EIP 전송 계층과 Binance Spot, Bitget v3 UTA, Upbit Spot REST 1차 API가 구현되어 있습니다.

## 문서

- [프로젝트 기획서](docs/PROJECT_PLAN.md)

## 현재 기준

- 1차 거래소: Binance, Bitget, Upbit
- 구현 언어: Go
- 네트워크: 단일 ENI의 여러 secondary private IPv4와 EIP 1:1 연결
- IP 선택: 클라이언트 기본값과 요청별 `egressRouteId` 재정의

## 구현 상태

| 영역 | 상태 |
|---|---|
| 공통 오류·자격증명·요청 옵션 | 구현됨 |
| private IP별 HTTP 연결 풀과 요청별 EIP 선택 | 구현됨 |
| 다차원 요청 제한기 | 구현됨 |
| Binance Spot REST | 공개 시세, 계정, 주문 생성·조회·취소·목록 구현됨 |
| Bitget v3 UTA | Spot·USDT-M 공개 시세, 자산, 포지션, 주문 구현됨 |
| Upbit Spot REST | 공개 시세, 잔고, 주문 생성·조회·취소·목록 구현됨 |
| 공통 Spot API·적합성 테스트 | Binance·Bitget·Upbit 구현됨 |
| Binance USDⓈ-M·WebSocket·P1 거래소 | 예정 |

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

## Bitget v3 UTA 1차 범위

- Spot·USDT-M Futures 상품 정보, 현재가, 호가, 최근 체결, 캔들
- 통합 계정 자산과 USDT-M Futures 포지션
- Spot·USDT-M Futures 주문 생성, 조회, 취소, 미체결 목록, 주문 이력
- HMAC SHA-256 및 Base64 서명과 Passphrase 인증
- 전체 `6000/IP/분` 및 endpoint별 `IP 또는 UID/초` 요청 제한
- Demo Trading의 `paptrading: 1` 헤더 옵션
- 불명확한 주문 결과를 `UNKNOWN_EXECUTION_STATE`로 분류

세부 계약과 주의사항은 [Bitget v3 UTA 문서](docs/exchanges/BITGET.md)를 참고합니다.

## Upbit Spot 1차 범위

- 전체 마켓, 현재가, 호가, 최근 체결, 분봉
- 계정 잔고
- 주문 생성, 단건 조회, 취소, 미체결 목록, 종료 목록
- HS512 JWT와 SHA-512 `query_hash`, 요청별 고유 nonce
- IP 단위 공개 API와 계정 pocket 단위 private API 요청 제한
- `Remaining-Req`, HTTP 429·418 제한 상태 반영
- 주문 mutation의 불명확한 결과를 `UNKNOWN_EXECUTION_STATE`로 분류

세부 계약과 주의사항은 [Upbit Spot 문서](docs/exchanges/UPBIT.md)를 참고합니다.

## 공통 Spot API

`unified.SpotClient`는 Binance, Bitget, Upbit에 같은 메서드 계약을 제공합니다. 마켓은 거래소 문자열 대신 `Base`와 `Quote`로 지정하며, 요청별 EIP 옵션은 native API와 동일하게 전달합니다.

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
