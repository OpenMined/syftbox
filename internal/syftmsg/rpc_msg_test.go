package syftmsg

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestSyftRPCMessage_UnmarshalJSON(t *testing.T) {
	// Test data with base64 encoded body
	jsonData := `{
		"id": "0bd4fc1a-8e0e-4075-a82a-3ef114d9254f",
		"sender": "alice@openmined.org",
		"url": "syft://alice@openmined.org/app_data/mit/rpc/search",
		"body": "eyJpZCI6IjBhNWE5MThhLWUxYzctNDNlZi05NzgxLTI3ZTUyNzc5MzBkYiIsInF1ZXJ5IjoiV2hhdCBpcyBlbmNyeXB0ZWQgcHJvbXB0ICA_ICJ9",
		"headers": {
			"content-type": "application/json"
		},
		"created": "2025-07-29T11:53:51.103784+00:00",
		"expires": "2025-07-30T11:53:50.950340+00:00",
		"status_code": 200
	}`

	var msg SyftRPCMessage
	err := json.Unmarshal([]byte(jsonData), &msg)
	assert.NoError(t, err)

	// Verify the fields were parsed correctly
	assert.Equal(t, "alice@openmined.org", msg.Sender)
	assert.Equal(t, 200, int(msg.StatusCode))

	// Verify the URL was parsed correctly
	expectedURL, err := utils.FromSyftURL("syft://alice@openmined.org/app_data/mit/rpc/search")
	assert.NoError(t, err)
	assert.Equal(t, expectedURL.Datasite, msg.URL.Datasite)
	assert.Equal(t, expectedURL.AppName, msg.URL.AppName)
	assert.Equal(t, expectedURL.Endpoint, msg.URL.Endpoint)

	// Verify the body was decoded correctly
	// The base64 string "eyJpZCI6IjBhNWE5MThhLWUxYzctNDNlZi05NzgxLTI3ZTUyNzc5MzBkYiIsInF1ZXJ5IjoiV2hhdCBpcyBlbmNyeXB0ZWQgcHJvbXB0ICA_ICJ9"
	// decodes to: {"id":"0a5a918a-e1c7-43ef-9781-27e5277930db","query":"What is encrypted prompt  ? "}
	assert.Contains(t, string(msg.Body), `"id":"0a5a918a-e1c7-43ef-9781-27e5277930db"`)
	assert.Contains(t, string(msg.Body), `"query":"What is encrypted prompt  ? "`)
}

func TestSyftRPCMessage_UnmarshalJSON_EmptyBody(t *testing.T) {
	// Test data without body
	jsonData := `{
		"id": "0bd4fc1a-8e0e-4075-a82a-3ef114d9254f",
		"sender": "alice@openmined.org",
		"url": "syft://alice@openmined.org/app_data/mit/rpc/search",
		"headers": {
			"content-type": "application/json"
		},
		"created": "2025-07-29T11:53:51.103784+00:00",
		"expires": "2025-07-30T11:53:50.950340+00:00",
		"status_code": 200
	}`

	var msg SyftRPCMessage
	err := json.Unmarshal([]byte(jsonData), &msg)
	assert.NoError(t, err)

	// Verify the body is empty
	assert.Empty(t, msg.Body)
}

func TestSyftRPCMessage_MarshalUnmarshal_RoundTrip(t *testing.T) {
	// Create a test message with various data types
	originalID := uuid.New()
	originalSender := "test@example.com"
	originalURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/myapp/rpc/endpoint")
	assert.NoError(t, err)

	originalBody := []byte(`{"key": "value", "number": 42, "array": [1, 2, 3]}`)
	originalHeaders := map[string]string{
		"content-type":  "application/json",
		"authorization": "Bearer token123",
	}
	originalCreated := time.Now().UTC().Truncate(time.Microsecond)
	originalExpires := originalCreated.Add(24 * time.Hour)
	originalMethod := MethodPOST
	originalStatusCode := StatusOK

	originalMsg := &SyftRPCMessage{
		ID:         originalID,
		Sender:     originalSender,
		URL:        *originalURL,
		Body:       originalBody,
		Headers:    originalHeaders,
		Created:    originalCreated,
		Expires:    originalExpires,
		Method:     originalMethod,
		StatusCode: originalStatusCode,
	}

	// Marshal the message to JSON
	jsonData, err := json.Marshal(originalMsg)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Verify the marshaled JSON contains base64 encoded body
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	assert.NoError(t, err)

	// Check that body is base64 encoded
	bodyStr, ok := jsonMap["body"].(string)
	assert.True(t, ok)
	assert.NotEmpty(t, bodyStr)

	// Verify it's valid base64
	decodedBody, err := base64.URLEncoding.DecodeString(bodyStr)
	assert.NoError(t, err)
	assert.Equal(t, originalBody, decodedBody)

	// Unmarshal the JSON back to a message
	var unmarshaledMsg SyftRPCMessage
	err = json.Unmarshal(jsonData, &unmarshaledMsg)
	assert.NoError(t, err)

	// Verify all fields match the original
	assert.Equal(t, originalID, unmarshaledMsg.ID)
	assert.Equal(t, originalSender, unmarshaledMsg.Sender)
	assert.Equal(t, originalURL.Datasite, unmarshaledMsg.URL.Datasite)
	assert.Equal(t, originalURL.AppName, unmarshaledMsg.URL.AppName)
	assert.Equal(t, originalURL.Endpoint, unmarshaledMsg.URL.Endpoint)
	assert.Equal(t, originalBody, unmarshaledMsg.Body)
	assert.Equal(t, originalHeaders, unmarshaledMsg.Headers)
	assert.Equal(t, originalCreated, unmarshaledMsg.Created)
	assert.Equal(t, originalExpires, unmarshaledMsg.Expires)
	assert.Equal(t, originalMethod, unmarshaledMsg.Method)
	assert.Equal(t, originalStatusCode, unmarshaledMsg.StatusCode)
}

func TestSyftRPCMessage_MarshalUnmarshal_EmptyBody(t *testing.T) {
	// Create a test message with empty body
	originalID := uuid.New()
	originalSender := "test@example.com"
	originalURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/myapp/rpc/endpoint")
	assert.NoError(t, err)

	originalHeaders := map[string]string{
		"content-type": "application/json",
	}
	originalCreated := time.Now().UTC().Truncate(time.Microsecond)
	originalExpires := originalCreated.Add(24 * time.Hour)

	originalMsg := &SyftRPCMessage{
		ID:      originalID,
		Sender:  originalSender,
		URL:     *originalURL,
		Body:    nil, // Empty body
		Headers: originalHeaders,
		Created: originalCreated,
		Expires: originalExpires,
	}

	// Marshal the message to JSON
	jsonData, err := json.Marshal(originalMsg)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Unmarshal the JSON back to a message
	var unmarshaledMsg SyftRPCMessage
	err = json.Unmarshal(jsonData, &unmarshaledMsg)
	assert.NoError(t, err)

	// Verify all fields match the original
	assert.Equal(t, originalID, unmarshaledMsg.ID)
	assert.Equal(t, originalSender, unmarshaledMsg.Sender)
	assert.Equal(t, originalURL.Datasite, unmarshaledMsg.URL.Datasite)
	assert.Equal(t, originalURL.AppName, unmarshaledMsg.URL.AppName)
	assert.Equal(t, originalURL.Endpoint, unmarshaledMsg.URL.Endpoint)
	assert.Empty(t, unmarshaledMsg.Body) // Body should remain empty
	assert.Equal(t, originalHeaders, unmarshaledMsg.Headers)
	assert.Equal(t, originalCreated, unmarshaledMsg.Created)
	assert.Equal(t, originalExpires, unmarshaledMsg.Expires)
}

func TestSyftRPCMessage_MarshalUnmarshal_ComplexBody(t *testing.T) {
	// Create a test message with complex JSON body
	originalID := uuid.New()
	originalSender := "test@example.com"
	originalURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/myapp/rpc/search")
	assert.NoError(t, err)

	// Complex JSON body with nested structures
	originalBody := []byte(`{
		"query": "What is encrypted prompt?",
		"results": [
			{
				"id": "0a5a918a-e1c7-43ef-9781-27e5277930db",
				"score": 0.46056801080703735,
				"content": "## LLMs Can Understand Encrypted Prompt: Towards Privacy-Computing Friendly Transformers",
				"metadata": {
					"filename": "LLMs Can Understand Encrypted Prompt.pdf"
				}
			}
		],
		"providerInfo": {
			"provider": "local_rag"
		},
		"cost": 0.1
	}`)

	originalHeaders := map[string]string{
		"content-type":   "application/json",
		"content-length": "2075",
	}
	originalCreated := time.Now().UTC().Truncate(time.Microsecond)
	originalExpires := originalCreated.Add(24 * time.Hour)
	originalStatusCode := StatusOK

	originalMsg := &SyftRPCMessage{
		ID:         originalID,
		Sender:     originalSender,
		URL:        *originalURL,
		Body:       originalBody,
		Headers:    originalHeaders,
		Created:    originalCreated,
		Expires:    originalExpires,
		StatusCode: originalStatusCode,
	}

	base64encodedBody := base64.URLEncoding.EncodeToString(originalBody)

	// Marshal the message to JSON
	jsonData, err := json.Marshal(originalMsg)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Unmarshal to a map
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonMap)
	assert.Equal(t, jsonMap["body"], base64encodedBody)

	// Unmarshal the JSON back to a message
	var unmarshaledMsg SyftRPCMessage
	err = json.Unmarshal(jsonData, &unmarshaledMsg)
	assert.NoError(t, err)

	// Verify all fields match the original
	assert.Equal(t, originalID, unmarshaledMsg.ID)
	assert.Equal(t, originalSender, unmarshaledMsg.Sender)
	assert.Equal(t, originalURL.Datasite, unmarshaledMsg.URL.Datasite)
	assert.Equal(t, originalURL.AppName, unmarshaledMsg.URL.AppName)
	assert.Equal(t, originalURL.Endpoint, unmarshaledMsg.URL.Endpoint)
	assert.Equal(t, originalBody, unmarshaledMsg.Body)
	assert.Equal(t, originalHeaders, unmarshaledMsg.Headers)
	assert.Equal(t, originalCreated, unmarshaledMsg.Created)
	assert.Equal(t, originalExpires, unmarshaledMsg.Expires)
	assert.Equal(t, originalStatusCode, unmarshaledMsg.StatusCode)

	// Verify the body content is preserved correctly
	assert.Contains(t, string(unmarshaledMsg.Body), `"query"`)
	assert.Contains(t, string(unmarshaledMsg.Body), `"What is encrypted prompt?"`)
	assert.Contains(t, string(unmarshaledMsg.Body), `"0a5a918a-e1c7-43ef-9781-27e5277930db"`)
	assert.Contains(t, string(unmarshaledMsg.Body), `"local_rag"`)
}
