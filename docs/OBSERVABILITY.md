# REST 오류 관측

`exchange.ExecutorConfig.Observer`는 HTTP 전송 attempt마다 한 번 호출됩니다. 거래소·endpoint·route·시도 번호·소요 시간·HTTP 상태·공통 오류 분류를 기록할 수 있습니다.

```go
executor, err := exchange.NewExecutor(exchange.ExecutorConfig{
    Sender: registry,
    Limiter: limiter,
    Observer: exchange.ExecutionObserverFunc(func(value exchange.ExecutionObservation) {
        // 병렬 호출에 안전한 metrics/logging 구현으로 전달합니다.
        record(value)
    }),
})
```

네트워크 오류뿐 아니라 401·403·429·5xx와 HTTP 200 응답 안의 거래소 오류도 `ErrorCategory`에 반영합니다. 각 native 클라이언트가 기존 오류 분류기를 `Execution.ClassifyResponse`에 연결합니다. OKX 주문 항목의 `sCode`, Coinbase 주문 실패·취소 항목, Kraken Futures `sendStatus`·`cancelStatus`도 관측합니다. 여러 항목이 실패한 응답은 첫 번째 확인된 오류를 대표 분류로 기록합니다.

분류기는 observer가 설정돼 있을 때만 호출합니다. 원본 응답, 호출자의 오류 처리와 기존 재시도 정책을 바꾸지 않습니다. 예를 들어 HTTP 200의 rate-limit envelope를 관측해도 새 자동 재시도는 추가하지 않습니다. 주문 mutation은 자동 재시도하지 않습니다.

관측은 전송 attempt와 envelope 수준입니다. 전송 전 요청 인자 검증 실패나 개별 응답 필드의 후속 변환 오류까지 모두 포괄하는 최종 메서드 호출 지표는 아닙니다. 알 수 없는 일반 실행 오류는 `INTERNAL`로 기록합니다.

observer에는 응답 본문·인증 정보·쿼리 문자열을 전달하지 않습니다. endpoint ID가 생략되면 `METHOD /path`를 사용하므로 동적 주문 ID가 포함되는 경로를 metric label로 그대로 쓰면 값의 종류가 늘어날 수 있습니다. 직접 `Execution`을 구성할 때는 고정된 `EndpointID`를 지정하고 observer에서 필요한 정규화를 적용합니다.

observer는 실행 흐름 안에서 동기 호출됩니다. 짧고 병렬 호출에 안전하게 구현하고, 외부 전송은 별도 버퍼나 수집기에 위임합니다.
