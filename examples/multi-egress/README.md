# 다중 송신 IP 진단 예제

호스트 OS에 송신 원본 IPv4를 모두 할당하고 각 주소의 외부 관측 공인 IP를 확인한 뒤 실행합니다. AWS의 secondary private IPv4→EIP 방식과 Vultr의 추가 public IPv4 직접 할당 방식을 함께 지원합니다.

```bash
go run ./cmd/egressdiag \
  -route aws-a,10.0.10.21,203.0.113.10 \
  -route vultr-a,203.0.113.20,203.0.113.20
```

각 `-route` 값은 다음 순서입니다.

```text
route-id,local-source-ip,expected-public-ip
```

진단기는 다음을 검사합니다.

1. local source IP가 실제 호스트의 네트워크 인터페이스에 할당되어 있는지 확인합니다.
2. source IP 전용 HTTP transport로 IP 확인 endpoint를 호출합니다.
3. 외부에서 관측한 IP가 기대한 public IP와 같은지 확인합니다.
4. 모든 route 결과를 JSON으로 출력하고 하나라도 실패하면 종료 코드 1을 반환합니다.

기본 확인 endpoint는 `https://api.ipify.org`입니다. 자체 운영 endpoint를 사용하려면 `-endpoint`를 지정합니다.

```bash
go run ./cmd/egressdiag \
  -endpoint https://egress-check.example.com/ip \
  -route vultr-a,203.0.113.20,203.0.113.20
```

이 명령에는 API Key나 Secret이 필요하지 않습니다.
