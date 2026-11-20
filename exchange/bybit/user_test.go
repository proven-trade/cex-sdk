package bybit

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

func TestSubAccountManagement(t *testing.T) {
	t.Parallel()

	readOnly := true
	tests := []struct {
		name   string
		result string
		path   string
		body   string
		call   func(*Client) (any, error)
		want   any
	}{
		{
			name: "CreateSubMember", result: `{"uid":"53888000","username":"xxxxx1","memberType":1,"status":1,"remark":"test"}`,
			path: "/v5/user/create-sub-member",
			body: `{"username":"xxxxx1","password":"Passw0rd1","memberType":1,"switch":1,"isUta":true,"note":"test"}`,
			call: func(client *Client) (any, error) {
				return client.CreateSubMember(context.Background(), CreateSubMemberRequest{
					Username: "xxxxx1", Password: "Passw0rd1", MemberType: SubMemberTypeNormal,
					QuickLogin: true, IsUTA: true, Note: "test",
				})
			},
			want: SubMember{UID: "53888000", Username: "xxxxx1", MemberType: 1, Status: 1, Remark: "test"},
		},
		{
			name: "CreateSubAPIKey",
			result: `{"id":"16651283","note":"testxxx","apiKey":"key","readOnly":0,"secret":"sec",` +
				`"permissions":{"ContractTrade":[],"Spot":[],"Wallet":["AccountTransfer"],"Options":[],"CopyTrading":[],"BlockTrade":[],"Exchange":[],"NFT":[]}}`,
			path: "/v5/user/create-sub-api",
			body: `{"subuid":53888000,"note":"testxxx","readOnly":0,"ips":"1.1.1.1,2.2.2.2","permissions":{"Wallet":["AccountTransfer"]}}`,
			call: func(client *Client) (any, error) {
				return client.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{
					SubUID: 53888000, Note: "testxxx", IPs: []string{"1.1.1.1", "2.2.2.2"},
					Permissions: APIKeyPermissions{Wallet: []string{"AccountTransfer"}},
				})
			},
			want: SubAPIKey{ID: "16651283", Note: "testxxx", APIKey: "key", Secret: "sec", Permissions: APIKeyPermissions{
				ContractTrade: []string{}, Spot: []string{}, Wallet: []string{"AccountTransfer"}, Options: []string{},
				CopyTrading: []string{}, BlockTrade: []string{}, Exchange: []string{}, NFT: []string{},
			}},
		},
		{
			name:   "UpdateSubAPIKey",
			result: `{"id":"1","note":"n","apiKey":"key","readOnly":1,"secret":"","permissions":{"Spot":["SpotTrade"]},"ips":["*"]}`,
			path:   "/v5/user/update-sub-api",
			body:   `{"apikey":"key","readOnly":1,"ips":"*","permissions":{"Spot":["SpotTrade"]}}`,
			call: func(client *Client) (any, error) {
				return client.UpdateSubAPIKey(context.Background(), UpdateSubAPIKeyRequest{
					APIKey: "key", ReadOnly: &readOnly, IPs: []string{},
					Permissions: &APIKeyPermissions{Spot: []string{"SpotTrade"}, BlockTrade: []string{"ignored"}},
				})
			},
			want: SubAPIKey{ID: "1", Note: "n", APIKey: "key", ReadOnly: 1, Permissions: APIKeyPermissions{
				Spot: []string{"SpotTrade"},
			}, IPs: []string{"*"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, assetTransferPermissions, extResult(test.result))
			got, err := test.call(client)
			if err != nil {
				t.Fatalf("%s() error = %v", test.name, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, test.path)
			assertJSONEqual(t, call.Body, test.body)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s() = %+v, want %+v", test.name, got, test.want)
			}
		})
	}
}

func TestSubAccountAcks(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	if _, err := client.FreezeSubMember(context.Background(), 53888001, true); err != nil {
		t.Fatalf("FreezeSubMember() error = %v", err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/user/frozen-sub-member")
	assertJSONEqual(t, call.Body, `{"subuid":53888001,"frozen":1}`)

	client, recorder, _ = newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	if _, err := client.FreezeSubMember(context.Background(), 53888001, false); err != nil {
		t.Fatalf("FreezeSubMember(unfreeze) error = %v", err)
	}
	assertJSONEqual(t, extSingleCall(t, recorder, http.MethodPost, "/v5/user/frozen-sub-member").Body,
		`{"subuid":53888001,"frozen":0}`)

	client, recorder, _ = newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	if _, err := client.DeleteSubAPIKey(context.Background(), "key"); err != nil {
		t.Fatalf("DeleteSubAPIKey() error = %v", err)
	}
	call = extSingleCall(t, recorder, http.MethodPost, "/v5/user/delete-sub-api")
	assertJSONEqual(t, call.Body, `{"apikey":"key"}`)

	client, recorder, _ = newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	if _, err := client.DeleteSubAPIKey(context.Background(), ""); err != nil {
		t.Fatalf("DeleteSubAPIKey(self) error = %v", err)
	}
	call = extSingleCall(t, recorder, http.MethodPost, "/v5/user/delete-sub-api")
	assertJSONEqual(t, call.Body, `{}`)
}

func TestUserReads(t *testing.T) {
	t.Parallel()

	client, recorder, limiter := newExtTestClient(t, extReadPermissions, extResult(
		`{"subMembers":[{"uid":"53888001","username":"xxx001","memberType":1,"status":1,"remark":"test","accountMode":3},`+
			`{"uid":"53888002","username":"xxx002","memberType":6,"status":4,"remark":"","accountMode":1}]}`,
	))
	members, err := client.QuerySubMembers(context.Background())
	want := SubMembers{SubMembers: []SubMember{
		{UID: "53888001", Username: "xxx001", MemberType: 1, Status: 1, AccountMode: 3, Remark: "test"},
		{UID: "53888002", Username: "xxx002", MemberType: 6, Status: 4, AccountMode: 1},
	}}
	if err != nil || !reflect.DeepEqual(members, want) {
		t.Fatalf("QuerySubMembers() = %+v, error = %v", members, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/user/query-sub-members")
	if !reflect.DeepEqual(call.Query, url.Values{}) || call.Body != "" {
		t.Fatalf("query = %v body = %q, want empty", call.Query, call.Body)
	}
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/user/query-sub-members"))
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
	}

	client, recorder, _ = newExtTestClient(t, extReadPermissions, extResult(
		`{"id":"13770661","note":"rw","apiKey":"XXXXXX","readOnly":0,"secret":"",`+
			`"permissions":{"ContractTrade":["Order","Position"],"Spot":["SpotTrade"],"Wallet":["AccountTransfer","SubMemberTransfer"],`+
			`"Options":["OptionsTrade"],"Derivatives":[],"CopyTrading":[],"BlockTrade":[],"Exchange":[],"NFT":[],"Affiliate":[]},`+
			`"ips":["*"],"type":1,"deadlineDay":66,"expiredAt":"2023-12-22T07:20:25Z","createdAt":"2022-10-16T02:24:40Z",`+
			`"unified":0,"uta":0,"userID":24617703,"inviterID":0,"vipLevel":"No VIP","mktMakerLevel":"0","affiliateID":0,`+
			`"rsaPublicKey":"","isMaster":true,"parentUid":"0","kycLevel":"LEVEL_DEFAULT","kycRegion":""}`,
	))
	info, err := client.QueryAPIKey(context.Background())
	if err != nil || info.APIKey != "XXXXXX" || info.UserID != 24617703 || !info.IsMaster || info.DeadlineDay != 66 ||
		info.VIPLevel != "No VIP" || info.ParentUID != "0" || info.KYCLevel != "LEVEL_DEFAULT" ||
		!reflect.DeepEqual(info.Permissions.Wallet, []string{"AccountTransfer", "SubMemberTransfer"}) ||
		!reflect.DeepEqual(info.IPs, []string{"*"}) {
		t.Fatalf("QueryAPIKey() = %+v, error = %v", info, err)
	}
	extSingleCall(t, recorder, http.MethodGet, "/v5/user/query-api")
}

func TestUserValidation(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	valid := APIKeyPermissions{Spot: []string{"SpotTrade"}}
	readOnly := false
	for name, call := range map[string]func() error{
		"short username": func() error {
			_, err := client.CreateSubMember(context.Background(), CreateSubMemberRequest{Username: "abc1", MemberType: 1})
			return err
		},
		"member type": func() error {
			_, err := client.CreateSubMember(context.Background(), CreateSubMemberRequest{Username: "abcde123", MemberType: 2})
			return err
		},
		"short password": func() error {
			_, err := client.CreateSubMember(context.Background(), CreateSubMemberRequest{
				Username: "abcde123", MemberType: 1, Password: "Ab1",
			})
			return err
		},
		"api key uid": func() error {
			_, err := client.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{Permissions: valid})
			return err
		},
		"api key permissions": func() error {
			_, err := client.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{SubUID: 1})
			return err
		},
		"api key ip list": func() error {
			_, err := client.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{
				SubUID: 1, Permissions: valid, IPs: []string{"1.1.1.1,2.2.2.2"},
			})
			return err
		},
		"update nothing": func() error {
			_, err := client.UpdateSubAPIKey(context.Background(), UpdateSubAPIKeyRequest{APIKey: "key"})
			return err
		},
		"update empty permissions": func() error {
			_, err := client.UpdateSubAPIKey(context.Background(), UpdateSubAPIKeyRequest{
				APIKey: "key", ReadOnly: &readOnly, Permissions: &APIKeyPermissions{},
			})
			return err
		},
		"freeze uid": func() error {
			_, err := client.FreezeSubMember(context.Background(), 0, true)
			return err
		},
		"delete whitespace": func() error {
			_, err := client.DeleteSubAPIKey(context.Background(), " key")
			return err
		},
	} {
		if err := call(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want validation error", name, err)
		}
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("server received %d requests, want none", len(calls))
	}
}

func TestUserMutationsRequireTransferAndAreNotRetried(t *testing.T) {
	t.Parallel()

	permissions := APIKeyPermissions{Spot: []string{"SpotTrade"}}
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"CreateSubMember", func(c *Client) error {
			_, err := c.CreateSubMember(context.Background(), CreateSubMemberRequest{Username: "abcde123", MemberType: 1})
			return err
		}},
		{"CreateSubAPIKey", func(c *Client) error {
			_, err := c.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{SubUID: 1, Permissions: permissions})
			return err
		}},
		{"UpdateSubAPIKey", func(c *Client) error {
			_, err := c.UpdateSubAPIKey(context.Background(), UpdateSubAPIKeyRequest{APIKey: "k", Permissions: &permissions})
			return err
		}},
		{"DeleteSubAPIKey", func(c *Client) error {
			_, err := c.DeleteSubAPIKey(context.Background(), "k")
			return err
		}},
		{"FreezeSubMember", func(c *Client) error {
			_, err := c.FreezeSubMember(context.Background(), 1, true)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
			if err := test.call(client); !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("error = %v, want authorization without Transfer permission", err)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none", len(calls))
			}

			client, recorder, _ = newExtTestClient(t, assetTransferPermissions, extServerError)
			if err := test.call(client); !errors.Is(err, trade.ErrUnknownExecutionState) {
				t.Fatalf("error = %v, want unknown execution state", err)
			}
			if calls := recorder.snapshot(); len(calls) != 1 {
				t.Fatalf("server received %d requests, want exactly one without retry", len(calls))
			}
		})
	}
}
