# 다중 EIP 진단 예제

EC2의 한 ENI에 secondary private IPv4를 할당하고 각각의 private IP에 EIP를 연결한 뒤 실행합니다.

```bash
go run ./cmd/egressdiag \
  -route seoul-a,10.0.10.21,203.0.113.10 \
  -route seoul-b,10.0.10.22,203.0.113.11
```

각 `-route` 값은 다음 순서입니다.

```text
route-id,local-private-ip,expected-public-ip
```

진단기는 다음을 검사합니다.

1. local private IP가 실제 호스트의 네트워크 인터페이스에 할당되어 있는지 확인합니다.
2. private IP 전용 HTTP transport로 IP 확인 endpoint를 호출합니다.
3. 외부에서 관측한 IP가 기대한 EIP와 같은지 확인합니다.
4. 모든 route 결과를 JSON으로 출력하고 하나라도 실패하면 종료 코드 1을 반환합니다.

기본 확인 endpoint는 `https://checkip.amazonaws.com`입니다. 자체 운영 endpoint를 사용하려면 `-endpoint`를 지정합니다.

```bash
go run ./cmd/egressdiag \
  -endpoint https://egress-check.example.com/ip \
  -route seoul-a,10.0.10.21,203.0.113.10
```

이 명령에는 API Key나 Secret이 필요하지 않습니다.
