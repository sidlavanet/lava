package chainlib

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	websocket2 "github.com/gorilla/websocket"
	spectypes "github.com/lavanet/lava/x/spec/types"
	"github.com/stretchr/testify/assert"
)

func TestMatchSpecApiByName(t *testing.T) {
	t.Parallel()
	connectionType := ""
	testTable := []struct {
		name        string
		serverApis  map[ApiKey]ApiContainer
		inputName   string
		expectedApi spectypes.Api
		expectedOk  bool
	}{
		{
			name: "test1",
			serverApis: map[ApiKey]ApiContainer{
				{Name: "/blocks/[^\\/\\s]+", ConnectionType: connectionType}: {
					api: &spectypes.Api{
						Name: "/blocks/{height}",
						BlockParsing: spectypes.BlockParser{
							ParserArg:  []string{"0"},
							ParserFunc: spectypes.PARSER_FUNC_PARSE_BY_ARG,
						},
						ComputeUnits: 10,
						Enabled:      true,
						Category:     spectypes.SpecCategory{Deterministic: true},
					},
					collectionKey: CollectionKey{ConnectionType: connectionType},
				},
			},
			inputName:   "/blocks/10",
			expectedApi: spectypes.Api{Name: "/blocks/{height}"},
			expectedOk:  true,
		},
		{
			name: "test2",
			serverApis: map[ApiKey]ApiContainer{
				{Name: "/cosmos/base/tendermint/v1beta1/blocks/[^\\/\\s]+", ConnectionType: connectionType}: {
					api: &spectypes.Api{
						Name: "/cosmos/base/tendermint/v1beta1/blocks/{height}",
						BlockParsing: spectypes.BlockParser{
							ParserArg:  []string{"0"},
							ParserFunc: spectypes.PARSER_FUNC_PARSE_BY_ARG,
						},
						ComputeUnits: 10,
						Enabled:      true,
						Category:     spectypes.SpecCategory{Deterministic: true},
					},
					collectionKey: CollectionKey{ConnectionType: connectionType},
				},
			},
			inputName:   "/cosmos/base/tendermint/v1beta1/blocks/10",
			expectedApi: spectypes.Api{Name: "/cosmos/base/tendermint/v1beta1/blocks/{height}"},
			expectedOk:  true,
		},
		{
			name: "test3",
			serverApis: map[ApiKey]ApiContainer{
				{Name: "/cosmos/base/tendermint/v1beta1/blocks/latest", ConnectionType: connectionType}: {
					api: &spectypes.Api{
						Name: "/cosmos/base/tendermint/v1beta1/blocks/latest",
						BlockParsing: spectypes.BlockParser{
							ParserArg:  []string{"0"},
							ParserFunc: spectypes.PARSER_FUNC_DEFAULT,
						},
						ComputeUnits: 10,
						Enabled:      true,
						Category:     spectypes.SpecCategory{Deterministic: true},
					},
					collectionKey: CollectionKey{ConnectionType: connectionType},
				},
			},
			inputName:   "/cosmos/base/tendermint/v1beta1/blocks/latest",
			expectedApi: spectypes.Api{Name: "/cosmos/base/tendermint/v1beta1/blocks/latest"},
			expectedOk:  true,
		},
	}
	for _, testCase := range testTable {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api, ok := matchSpecApiByName(testCase.inputName, connectionType, testCase.serverApis)
			if ok != testCase.expectedOk {
				t.Fatalf("expected ok value %v, but got %v", testCase.expectedOk, ok)
			}
			if api.api.Name != testCase.expectedApi.Name {
				t.Fatalf("expected api %v, but got %v", testCase.expectedApi.Name, api.api.Name)
			}
		})
	}
}

func TestConvertToJsonError(t *testing.T) {
	t.Parallel()

	testTable := []struct {
		name     string
		errorMsg string
		expected string
	}{
		{
			name:     "valid json",
			errorMsg: "some error message",
			expected: `{"error":"some error message"}`,
		},
	}

	for _, testCase := range testTable {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := convertToJsonError(testCase.errorMsg)
			if result != testCase.expected {
				t.Errorf("Expected result to be %s, but got %s", testCase.expected, result)
			}
		})
	}
}

func TestAddAttributeToError(t *testing.T) {
	t.Parallel()

	testTable := []struct {
		name         string
		key          string
		value        string
		errorMessage string
		expected     string
	}{
		{
			name:         "Valid conversion",
			key:          "key1",
			value:        "value1",
			errorMessage: `"errorKey": "error_value"`,
			expected:     `"errorKey": "error_value", "key1": "value1"`,
		},
	}

	for _, testCase := range testTable {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := addAttributeToError(testCase.key, testCase.value, testCase.errorMessage)
			if result != testCase.expected {
				t.Errorf("addAttributeToError(%q, %q, %q) = %q; expected %q", testCase.key, testCase.value, testCase.errorMessage, result, testCase.expected)
			}
		})
	}
}

func TestExtractDappIDFromWebsocketConnection(t *testing.T) {
	testCases := []struct {
		name     string
		route    string
		expected string
	}{
		{
			name:     "dappId exists in params",
			route:    "/ws/DappID123",
			expected: "DappID123",
		},
		{
			name:     "dappId does not exist in params",
			route:    "/",
			expected: "NoDappID",
		},
	}

	app := fiber.New()
	app.Get("/ws/:dappId", websocket.New(func(c *websocket.Conn) {
		mt, _, _ := c.ReadMessage()
		dappID := extractDappIDFromWebsocketConnection(c)
		c.WriteMessage(mt, []byte(dappID))
	}))

	app.Get("/", websocket.New(func(c *websocket.Conn) {
		mt, _, _ := c.ReadMessage()
		dappID := extractDappIDFromWebsocketConnection(c)
		c.WriteMessage(mt, []byte(dappID))
	}))

	go app.Listen("127.0.0.1:3000")
	defer func() {
		app.Shutdown()
	}()
	time.Sleep(time.Millisecond * 20) // let the server go up
	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			url := "ws://localhost:3000" + testCase.route
			dialer := &websocket2.Dialer{}
			conn, _, err := dialer.Dial(url, nil)
			if err != nil {
				t.Fatalf("Error dialing websocket connection: %s", err)
			}
			defer conn.Close()

			err = conn.WriteMessage(websocket.TextMessage, []byte("test"))
			if err != nil {
				t.Fatalf("Error writing message to websocket connection: %s", err)
			}

			_, response, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("Error reading message from websocket connection: %s", err)
			}

			responseString := string(response)
			if responseString != testCase.expected {
				t.Errorf("Expected %s but got %s", testCase.expected, responseString)
			}
		})
	}
}

func TestExtractDappIDFromFiberContext(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		route    string
		expected string
	}{
		{
			name:     "dappId exists in params",
			route:    "/DappID123/hello",
			expected: "DappID123",
		},
		{
			name:     "dappId does not exist in params",
			route:    "/",
			expected: "NoDappID",
		},
	}

	app := fiber.New()

	// Create route with GET method for test
	app.Get("/:dappId/*", func(c *fiber.Ctx) error {
		dappID := extractDappIDFromFiberContext(c)
		return c.SendString(dappID)
	})

	app.Get("/", func(c *fiber.Ctx) error {
		dappID := extractDappIDFromFiberContext(c)
		return c.SendString(dappID)
	})

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Create a new http request with the route from the test case
			req := httptest.NewRequest("GET", testCase.route, nil)

			resp, _ := app.Test(req, 1)
			body, _ := io.ReadAll(resp.Body)
			responseString := string(body)
			if responseString != testCase.expected {
				t.Errorf("Expected %s but got %s", testCase.expected, responseString)
			}
		})
	}
}

func TestConstructFiberCallbackWithDappIDExtraction(t *testing.T) {
	var gotCtx *fiber.Ctx

	callbackToBeCalled := func(c *fiber.Ctx) error {
		gotCtx = c
		return nil
	}

	handler := constructFiberCallbackWithHeaderAndParameterExtraction(callbackToBeCalled, false)
	ctx := &fiber.Ctx{}

	err := handler(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if gotCtx != ctx {
		t.Errorf("Expected ctx %v, but got %v", ctx, gotCtx)
	}
}

func TestParsedMessage_GetServiceApi(t *testing.T) {
	pm := parsedMessage{
		api: &spectypes.Api{},
	}
	assert.Equal(t, &spectypes.Api{}, pm.GetApi())
}

func TestParsedMessage_GetApiCollection(t *testing.T) {
	pm := parsedMessage{
		apiCollection: &spectypes.ApiCollection{},
	}
	assert.Equal(t, &spectypes.ApiCollection{}, pm.GetApiCollection())
}

func TestParsedMessage_RequestedBlock(t *testing.T) {
	pm := parsedMessage{
		requestedBlock: 123,
	}
	assert.Equal(t, int64(123), pm.RequestedBlock())
}

func TestParsedMessage_GetRPCMessage(t *testing.T) {
	rpcInput := &mockRPCInput{}

	pm := parsedMessage{
		msg: rpcInput,
	}
	assert.Equal(t, rpcInput, pm.GetRPCMessage())
}

type mockRPCInput struct{}

func (m *mockRPCInput) GetParams() interface{} {
	return nil
}

func (m *mockRPCInput) GetResult() json.RawMessage {
	return nil
}

func (m *mockRPCInput) ParseBlock(block string) (int64, error) {
	return 0, nil
}

func TestGetServiceApis(t *testing.T) {
	spec := spectypes.Spec{
		Enabled: true,
		ApiCollections: []*spectypes.ApiCollection{
			{
				Enabled: true,
				CollectionData: spectypes.CollectionData{
					ApiInterface: spectypes.APIInterfaceRest,
				},
				Apis: []*spectypes.Api{
					{
						Enabled: true,
						Name:    "test-api",
					},
					{
						Enabled: true,
						Name:    "test-api-2",
					},
					{
						Enabled: false,
						Name:    "test-api-disabled",
					},
					{
						Enabled: true,
						Name:    "test-api-3",
					},
				},
			},
			{
				Enabled: true,
				CollectionData: spectypes.CollectionData{
					ApiInterface: spectypes.APIInterfaceGrpc,
				},
				Apis: []*spectypes.Api{
					{
						Enabled: true,
						Name:    "gtest-api",
					},
					{
						Enabled: true,
						Name:    "gtest-api-2",
					},
					{
						Enabled: false,
						Name:    "gtest-api-disabled",
					},
					{
						Enabled: true,
						Name:    "gtest-api-3",
					},
				},
			},
		},
	}

	rpcInterface := spectypes.APIInterfaceRest
	serverApis, _, _ := getServiceApis(spec, rpcInterface)

	// Test serverApis
	if len(serverApis) != 3 {
		t.Errorf("Expected serverApis length to be 3, but got %d", len(serverApis))
	}
}
