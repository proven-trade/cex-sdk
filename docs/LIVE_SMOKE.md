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

## 증적 보안

JSON 증적에는 다음 정보만 기록합니다.

- 거래소, 상품, 마켓과 route ID
- 검사 시작·종료 시각과 소요 시간
- native market과 응답 항목 수
- local private IP, 기대 EIP와 관측 EIP
- 공통 오류 분류, 거래소 오류 코드와 HTTP 상태

원본 응답, 잔고 수량, 체결 가격, API Key, Secret, Passphrase와 거래소 오류 메시지는 기록하지 않습니다. 실패가 발생해도 가능한 나머지 검사를 계속 실행하므로 한 결과에서 전체 상태를 확인할 수 있습니다.

## 상태 갱신 기준

지원 매트릭스의 `live_read_smoke`는 실행기가 존재한다는 이유만으로 완료 처리하지 않습니다. 실제 배포 대상 인스턴스에서 해당 거래소·상품·EIP 조합의 JSON 결과가 `passed: true`이고, 실행 시각과 설정 변경 이력을 함께 보관했을 때만 `implemented`로 변경합니다.
