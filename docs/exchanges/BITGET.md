# Bitget v3 UTA 어댑터

## 전제조건

이 어댑터는 Bitget Unified Trading Account의 v3 REST API를 기준으로 합니다. Classic v2 계정용 endpoint는 포함하지 않습니다.

private API를 사용하려면 다음 세 값이 `credential.Provider`의 `credential.Material`에 있어야 합니다.

| 필드 | Bitget 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | HMAC Secret Key |
| `Passphrase` | API Key 생성 시 설정한 Passphrase |

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 상품 범위

| Category | 지원 |
|---|---|
| `SPOT` | 시세, 자산, 주문 |
| `USDT-FUTURES` | 시세, 자산, 포지션, 주문 |
| `MARGIN` | 아직 미지원 |
| `COIN-FUTURES` | 아직 미지원 |
| `USDC-FUTURES` | 아직 미지원 |

## 요청 제한

요청 한 건은 다음 두 제한을 원자적으로 차감합니다.

1. route 단위 전체 제한 `6000/IP/분`
2. endpoint별 문서 제한
   - 공개 시세: `20/IP/초`
   - 조회 private API: `20/UID/초`
   - 주문 생성·취소: `10/UID/초`

각 EIP route는 독립된 IP 제한 bucket을 사용합니다. UID 제한은 route를 변경해도 같은 계정 bucket을 사용하므로 EIP 변경을 UID 제한 우회 수단으로 사용하지 않습니다.

## 서명과 안전한 주문 실패

서명 문자열은 다음 순서로 구성합니다.

```text
timestamp + uppercase(method) + requestPath + optional("?" + sortedQuery) + exactBody
```

최종 문자열에 HMAC SHA-256을 적용하고 결과를 Base64로 인코딩합니다. 서명은 요청 제한 대기가 끝난 뒤 생성되므로 대기 시간 때문에 timestamp가 불필요하게 만료되지 않습니다.

주문 생성·취소의 전송 오류와 Bitget의 `40010`, `40725`, `45001` 응답은 실행 여부가 불명확할 수 있습니다. SDK는 이를 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. `clientOid`를 사용해 `OrderInfo` 또는 private order stream으로 최종 상태를 확인해야 합니다.

## Demo Trading

Demo API Key를 사용할 때 클라이언트 설정에 `DemoTrading: true`를 지정합니다. 모든 REST 요청에 `paptrading: 1` 헤더가 추가됩니다.

## 공식 기준 문서

- [Bitget v3 UTA Quick Start](https://www.bitget.com/api-doc/uta/guide)
- [Bitget v3 UTA Place Order](https://www.bitget.com/api-doc/uta/trade/Place-Order)
- [Bitget v3 UTA Error Code](https://www.bitget.com/api-doc/uta/error-code/restapi)
