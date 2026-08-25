# Live smoke 운영 검증

## 목적

`smoke.SpotReadRunner`는 실제 거래소의 Spot 읽기 API와 지정 EIP를 한 번에 검증합니다. 자동 단위 테스트가 확인할 수 없는 EC2 네트워크 설정, EIP 연결 관계, 거래소 접근 정책과 실제 응답 계약을 운영 환경에서 확인하기 위한 도구입니다.

읽기 smoke는 주문을 생성하거나 취소하지 않습니다. private 검사를 켜도 잔고 조회만 수행합니다. 거래 smoke는 주문이 발생하므로 별도의 명시적 승인과 제한된 계정이 필요하며 이 실행기에 포함하지 않습니다.

## 검사 순서

모든 거래소 요청에는 같은 `egressRouteId`와 검사 제한 시간이 전달됩니다.

1. 외부 IP 확인 endpoint에서 실제 공인 송신 IP 확인
2. 마켓 목록에서 대상 Spot 마켓과 native symbol 확인
3. 현재가와 양수 가격 확인
4. 양쪽 호가와 가격·수량 확인
5. 최근 체결의 가격·수량·방향 확인
6. 1분 캔들의 OHLC 범위와 거래량 확인
7. 선택적으로 private 잔고 필드 확인

EIP 검사를 통과하려면 route에 `ExpectedPublicIP`가 반드시 설정돼 있어야 합니다. 실제 관측 IP와 기대 EIP가 다르면 거래소 API가 정상이어도 전체 결과는 실패합니다.

## 사용 예시

CLI는 Binance, Bitget, Upbit, Bybit, OKX, Coinbase Advanced, Kraken, Bithumb, Coinone, Korbit, KuCoin, Gate.io, MEXC와 Crypto.com의 공통 Spot 어댑터를 선택할 수 있습니다. 먼저 [public 예제 설정](../examples/live-smoke/public.example.json)을 복사해 실제 EC2 private IP와 연결된 EIP로 바꿉니다.

```bash
go run ./cmd/livesmoke -config ./live-smoke.json
```

설정 파일에는 평문 자격증명을 넣을 수 없습니다. `includeBalances`가 `false`이면 `credentials` 항목도 거부하므로 공개 검사 과정에서 환경 Secret을 읽지 않는 것이 보장됩니다.

private 잔고까지 검사할 때는 [private read 예제 설정](../examples/live-smoke/private-read.example.json)처럼 환경변수 이름만 지정한 뒤 값을 프로세스 환경으로 전달합니다.

```bash
export PROVEN_BINANCE_API_KEY='운영 Secret 저장소에서 주입'
export PROVEN_BINANCE_SECRET_KEY='운영 Secret 저장소에서 주입'
go run ./cmd/livesmoke -config ./private-read.json > ./evidence.json
```

Bitget, OKX와 KuCoin은 `passphraseEnv`도 필요합니다. CLI 인자, JSON 설정과 결과 파일에는 실제 Secret을 넣지 않습니다.

라이브러리에서 직접 실행하려면 다음과 같이 구성합니다.

```go
runner, err := smoke.NewSpotReadRunner(smoke.SpotReadConfig{
	Client:           unifiedSpotClient,
	EgressVerifier:   registry,
	Market:           unified.Market{Base: "BTC", Quote: "USDT"},
	EgressRouteID:    "seoul-b",
	CheckTimeout:     10 * time.Second,
	IncludeBalances:  true,
})
if err != nil {
	return err
}

report, runErr := runner.Run(ctx)
encoder := json.NewEncoder(os.Stdout)
encoder.SetIndent("", "  ")
if err := encoder.Encode(report); err != nil {
	return err
}
return runErr
```

`Client`에는 각 거래소의 `NewUnifiedSpot`으로 만든 공통 Spot 어댑터를 전달합니다. `IncludeBalances`가 `true`이면 read 권한과 선택 route가 허용된 자격증명이 클라이언트에 설정돼 있어야 합니다.

CLI 설정의 `routes`에는 여러 private-IP/EIP 쌍을 등록할 수 있고 `egressRouteId`로 이번 실행에 사용할 하나를 선택합니다. 모든 local private IP는 실행 인스턴스의 네트워크 인터페이스에 실제로 할당돼 있어야 합니다.

## 증적 보안

JSON 증적에는 다음 정보만 기록합니다.

- 거래소, 상품, 마켓과 route ID
- 검사 시작·종료 시각과 소요 시간
- native market과 응답 항목 수
- local private IP, 기대 EIP와 관측 EIP
- 공통 오류 분류, 거래소 오류 코드와 HTTP 상태

원본 응답, 잔고 수량, 체결 가격, API Key, Secret, Passphrase와 거래소 오류 메시지는 기록하지 않습니다. 거래소 API 검사 하나가 실패해도 가능한 나머지 검사를 계속 실행하므로 한 결과에서 전체 상태를 확인할 수 있습니다. 단, EIP 검사가 실패하면 의도하지 않은 공인 IP로 요청을 보내지 않도록 모든 거래소 검사를 `skipped` 처리합니다.

## 실제 주문 smoke 안전 계약

`smoke.SpotTradeRunner`는 실제 계정의 생성-조회-취소 lifecycle을 검증합니다. 다음 조건을 전부 만족하지 않으면 주문 API를 호출하지 않습니다.

- `Confirmation`이 `smoke.RealOrderConfirmation`과 정확히 일치
- `Price × Quantity`가 `MaxNotional` 이하
- 시장가가 아닌 post-only 지정가 주문
- 주문 직전에 선택 EIP가 기대 공인 IP와 일치
- 매수가는 현재 최우선 매도호가보다 낮고 매도가는 현재 최우선 매수호가보다 높음
- 비어 있지 않은 고유 `ClientOrderID` 사용

```go
runner, err := smoke.NewSpotTradeRunner(smoke.SpotTradeConfig{
	Client:           unifiedSpotClient,
	EgressVerifier:   registry,
	Market:           unified.Market{Base: "BTC", Quote: "USDT"},
	EgressRouteID:    "seoul-b",
	CheckTimeout:     10 * time.Second,
	Side:             unified.SideBuy,
	Price:            "50000",
	Quantity:         "0.0001",
	MaxNotional:      "10",
	ClientOrderID:    "proven-smoke-20260825-001",
	Confirmation:     smoke.RealOrderConfirmation,
})
if err != nil {
	return err
}
report, err := runner.Run(ctx)
```

SDK는 주문 생성 mutation을 자동 재시도하지 않습니다. 생성 결과가 `UNKNOWN_EXECUTION_STATE`이면 같은 `ClientOrderID`로 조회한 뒤 취소를 시도합니다. 생성 이후 조회가 실패하거나 실행 context가 취소돼도, 정리 단계는 원래 context의 값만 상속하고 취소 신호와 분리된 제한 시간 context로 주문 취소와 최종 상태 조회를 시도합니다.

post-only는 주문이 접수되는 순간 taker 체결을 막지만, 호가에 올라간 뒤 시장이 이동하면 maker 체결 가능성이 있습니다. 따라서 충분히 비관통하는 가격, 거래소 최소 주문금액에 가까운 수량, 전용 하위 계정과 제한된 자산을 사용해야 합니다. 이 기능을 호출하는 것 자체가 실제 거래 승인에 해당하며 자동화된 기본 CLI에는 연결하지 않습니다.

주문 ID, 사용자 주문 ID, 가격, 수량과 계정 값은 결과 JSON에 기록하지 않습니다. 최종 조회가 `canceled`이고 체결 수량이 정확히 0일 때만 `cancellationConfirmed`가 `true`가 됩니다.

## 상태 갱신 기준

지원 매트릭스의 `live_read_smoke`는 실행기가 존재한다는 이유만으로 완료 처리하지 않습니다. 실제 배포 대상 인스턴스에서 해당 거래소·상품·EIP 조합의 JSON 결과가 `passed: true`이고, 실행 시각과 설정 변경 이력을 함께 보관했을 때만 `implemented`로 변경합니다.
