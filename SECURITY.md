# Security Policy

## Supported versions

보안 수정은 최신 릴리스 라인에 우선 적용됩니다. 오래된 태그를 운영에 사용한다면 최신 릴리스로 재현한 뒤 보고해 주세요.

## Reporting a vulnerability

보안 취약점, credential 노출 가능성, 서명 우회 또는 송신 경로 격리 문제는 공개 issue로 올리지 마세요. GitHub 저장소의 **Security → Report a vulnerability** private advisory를 사용해 주세요.

보고에는 재현 절차, 영향받는 버전, 예상 영향과 가능한 완화책을 포함하되 실제 API secret, access token, 서명된 전체 URL, 계정 식별 정보는 제거해야 합니다. 접수 후 3영업일 안에 확인하고, 영향도와 수정·공개 일정을 private advisory에서 조율합니다.

## Operational guidance

- read와 trade credential을 분리하고 출금 권한은 부여하지 않습니다.
- credential에 허용할 `egressRouteId`와 거래소 API key IP allowlist를 함께 제한합니다.
- 오류, trace, metric label에 Authorization, API key, signature, nonce 또는 signed query를 기록하지 않습니다.
- 실제 거래 smoke는 문서의 금액 상한과 post-only 보호를 적용한 전용 계정에서 실행합니다.
