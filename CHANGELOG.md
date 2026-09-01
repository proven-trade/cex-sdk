# Changelog

이 프로젝트의 사용자 영향 변경 사항은 이 파일에 기록합니다. 버전은 Semantic Versioning을 따릅니다.

## Unreleased

이번 변경에는 공개 주문 계약과 상태 의미의 호환되지 않는 변경이 포함되므로 다음 릴리스는 v2입니다.

### Breaking

- 공통 주문 생성은 안전한 사후 조정을 위해 `ClientOrderID`를 필수로 요구합니다.
- 시장가 매수의 견적 자산 주문금액은 `Order.Quantity`가 아니라 `Order.QuoteAmount`로 반환합니다.
- 상태를 확정하지 않는 생성·취소 응답은 `new`·`canceled` 대신 `acknowledged`·`cancel_pending`으로 반환합니다.

### Added

- 공통 주문의 `QuoteAmount`, `acknowledged`, `cancel_pending` 의미
- `MarketInfo` 주문 단위·최소 주문값과 exact-decimal `ValidateOrder`
- 분산 rate-limit backend 계약과 REST 실행 관측 훅
- 읽기 REST의 제한적 재시도와 WebSocket decode 재연결 복구

### Changed

- 로컬 rate limiter는 wall-clock fixed window 대신 rolling window를 사용합니다.
- 실계정 live smoke 전 거래소 상품은 지원 매트릭스에서 `experimental`로 표시합니다.
- REST redirect를 거부하고 전송 오류 문자열에서 서명 URL을 제거합니다.

### Security

- redirect를 통한 거래소 인증 헤더의 다른 origin 전달을 차단했습니다.
- 인증·전송 오류의 원본 URL과 credential-provider 오류가 기본 오류 문자열에 노출되지 않게 했습니다.

## 1.0.1

- 초기 다중 거래소 REST·WebSocket 어댑터와 문서 보완.

## 1.0.0

- 최초 공개 버전.
