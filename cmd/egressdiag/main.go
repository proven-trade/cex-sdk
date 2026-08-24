package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultEndpoint = "https://checkip.amazonaws.com"

type routeFlags []transport.EgressRoute

func (flags *routeFlags) String() string {
	items := make([]string, 0, len(*flags))
	for _, route := range *flags {
		items = append(items, fmt.Sprintf(
			"%s,%s,%s",
			route.ID,
			route.LocalPrivateIP,
			route.ExpectedPublicIP,
		))
	}
	return strings.Join(items, ";")
}

func (flags *routeFlags) Set(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return fmt.Errorf("route 형식은 id,local-private-ip,expected-public-ip 이어야 합니다")
	}

	id := transport.EgressRouteID(strings.TrimSpace(parts[0]))
	localIP := net.ParseIP(strings.TrimSpace(parts[1]))
	expectedIP := net.ParseIP(strings.TrimSpace(parts[2]))
	if id == "" {
		return fmt.Errorf("route ID가 비어 있습니다")
	}
	if localIP == nil || localIP.To4() == nil {
		return fmt.Errorf("route %q의 local private IP가 올바른 IPv4가 아닙니다", id)
	}
	if expectedIP == nil || expectedIP.To4() == nil {
		return fmt.Errorf("route %q의 expected public IP가 올바른 IPv4가 아닙니다", id)
	}

	*flags = append(*flags, transport.EgressRoute{
		ID:               id,
		LocalPrivateIP:   localIP,
		ExpectedPublicIP: expectedIP,
	})
	return nil
}

type routeResult struct {
	RouteID          transport.EgressRouteID `json:"routeId"`
	LocalPrivateIP   net.IP                  `json:"localPrivateIp"`
	ExpectedPublicIP net.IP                  `json:"expectedPublicIp"`
	ObservedPublicIP net.IP                  `json:"observedPublicIp,omitempty"`
	MatchesExpected  bool                    `json:"matchesExpected"`
	CheckedAt        time.Time               `json:"checkedAt,omitempty"`
	Error            string                  `json:"error,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "egress 진단 실패: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("egressdiag", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var routes routeFlags
	endpoint := flags.String("endpoint", defaultEndpoint, "외부에서 송신 IP를 확인할 HTTP endpoint")
	timeout := flags.Duration("timeout", 5*time.Second, "route 한 개의 진단 제한 시간")
	flags.Var(
		&routes,
		"route",
		"진단할 id,local-private-ip,expected-public-ip 형식의 경로이며 여러 번 지정 가능",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(routes) == 0 {
		flags.Usage()
		return fmt.Errorf("route를 한 개 이상 지정해야 합니다")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout은 0보다 커야 합니다")
	}

	registry, err := transport.NewRegistry(routes)
	if err != nil {
		return err
	}
	defer registry.Close()

	results := make([]routeResult, 0, len(routes))
	errorsByRoute := make([]error, 0)
	for _, route := range routes {
		routeCtx, cancel := context.WithTimeout(ctx, *timeout)
		check, checkErr := registry.VerifyPublicIP(routeCtx, route.ID, *endpoint)
		cancel()

		result := routeResult{
			RouteID:          route.ID,
			LocalPrivateIP:   route.LocalPrivateIP,
			ExpectedPublicIP: route.ExpectedPublicIP,
			ObservedPublicIP: check.ObservedPublicIP,
			MatchesExpected:  check.MatchesExpected,
			CheckedAt:        check.CheckedAt,
		}
		if checkErr != nil {
			result.Error = checkErr.Error()
			errorsByRoute = append(errorsByRoute, checkErr)
		}
		results = append(results, result)
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		return fmt.Errorf("진단 결과 JSON 출력: %w", err)
	}
	return errors.Join(errorsByRoute...)
}
