package send

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type SendHandler struct {
	hub  *ws.WebsocketHub
	blob *blob.BlobService
}

func New(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	return &SendHandler{hub: hub, blob: blob}
}

func (h *SendHandler) SendMsg(ctx *gin.Context) {
	var header MessageHeaders
	if err := ctx.ShouldBindHeader(&header); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	//  get the body
	body := ctx.Request.Body
	defer ctx.Request.Body.Close()

	// Read body Bytes
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// TODO: check if body bytes is not too large

	msg := syftmsg.NewHttpMsg(header.From, header.To, header.AppName, header.AppEp, header.Method, bodyBytes, header.Headers, header.Status, syftmsg.HttpMsgType(header.Type))

	slog.Info("sending message", "msg", msg)

	if header.Type == SendMsgResp {
		// TODO handler response messages
		return
	}

	// httpMsg
	httpMsg := msg.Data.(*syftmsg.HttpMsg)

	//  send the message to the websocket hub
	if ok := h.hub.SendMessageUser(header.To, msg); !ok {
		// Handle saving to blob storage
		// get the blob path
		blobPath := path.Join(
			header.From,
			"app_data",
			header.AppName,
			"rpc",
			header.AppEp,
			fmt.Sprintf("%s.%s", httpMsg.Id, httpMsg.Type),
		)

		// convert httpMsg to RPCMsg
		rpcMsg := syftmsg.NewSyftRPCMessage(*httpMsg)

		rpcMsgBytes, err := json.Marshal(rpcMsg)
		if err != nil {
			slog.Error("failed to marshal RPCMsg", "error", err)
			ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if _, err := h.blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
			Key:  blobPath,
			ETag: rpcMsg.ID.String(),
			Body: bytes.NewReader(rpcMsgBytes),
			Size: int64(len(rpcMsgBytes)),
		}); err != nil {
			slog.Error("failed to save message to blob storage", "error", err)
			ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		slog.Info("saved message to blob storage", "blobPath", blobPath)
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"message":    "Request has been accepted",
			"request_id": httpMsg.Id,
		})
		return
	}

	// create blobPath
	blobPath := path.Join(
		header.From,
		"app_data",
		header.AppName,
		"rpc",
		header.AppEp,
		fmt.Sprintf("%s.response", httpMsg.Id),
	)

	// poll for the object
	object, err := h.pollForObject(context.Background(), blobPath)

	// if the object is not found, return an accepted message
	if object == nil {
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"message":    "Request has been accepted. Please check back later.",
			"request_id": httpMsg.Id,
		})
		return
	}

	// if there was an error, return an internal server error
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{"Failed to get object": err.Error()})
		return
	}

	// read the object body
	bodyBytes, readErr := io.ReadAll(object.Body)
	if readErr != nil {
		ctx.PureJSON(
			http.StatusInternalServerError,
			gin.H{
				"Failed to read object": readErr.Error(),
				"request_id":            httpMsg.Id,
			},
		)
		return
	}

	// unmarshal the json object
	responseBody, unmarshalErr := unmarshalResponse(bodyBytes)
	if unmarshalErr != nil {
		ctx.PureJSON(
			http.StatusInternalServerError,
			gin.H{
				"Failed to unmarshal response": unmarshalErr.Error(),
				"request_id":                   httpMsg.Id,
			},
		)
		return
	}

	// send the response to the client
	ctx.PureJSON(http.StatusOK, gin.H{
		"request_id": httpMsg.Id,
		"message":    responseBody,
	})

}

func (h *SendHandler) PollForResponse(ctx *gin.Context) {
	// get the request_id from the query params
	// get the app name
	// get the app endpoint

	requestId := ctx.Query("request_id")
	if requestId == "" {
		ctx.PureJSON(
			http.StatusBadRequest, gin.H{
				"error":      "request_id is required",
				"request_id": requestId,
			},
		)
		return
	}

	// get the app name and app endpoint from the query params
	appName := ctx.Query("app_name")
	appEndpoint := ctx.Query("app_endpoint")
	user := ctx.Query("user")
	if appName == "" || appEndpoint == "" || user == "" {
		ctx.PureJSON(
			http.StatusBadRequest, gin.H{
				"error":      "app_name, app_endpoint and user are required",
				"request_id": requestId,
			},
		)
		return
	}

	// create the blob path to the app endpoint
	fileName := fmt.Sprintf("%s.response", requestId)

	// create the blob path to the app endpoint
	blobPath := path.Join(user, "app_data", appName, "rpc", appEndpoint, fileName)

	slog.Info("polling for response", "blobPath", blobPath)

	// poll the blob storage for the response
	object, err := h.pollForObject(context.Background(), blobPath)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Read the response body
	bodyBytes, err := io.ReadAll(object.Body)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Unmarshal the response
	responseBody, err := unmarshalResponse(bodyBytes)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// send the response to the client
	ctx.PureJSON(
		http.StatusOK, gin.H{
			"message":    "Response received",
			"response":   responseBody,
			"request_id": requestId,
		},
	)
}

func (h *SendHandler) pollForObject(ctx context.Context, blobPath string) (*blob.GetObjectResponse, error) {
	startTime := time.Now()
	maxTimeout := 10 * time.Second // or whatever timeout you want

	for {
		// check if timeout has been reached
		if time.Since(startTime) > maxTimeout {
			return nil, fmt.Errorf("No response exists. Polling timed out")
		}

		// try to get the object
		object, err := h.blob.Backend().GetObject(ctx, blobPath)
		if err == nil {
			return object, nil
		}

		// if we get here, there was an error but we haven't timed out yet
		// wait a bit before trying again
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond): // adjust polling interval as needed
			continue
		}
	}
}

// unmarshalResponse handles the unmarshaling of a response from blob storage
// It expects the response to have a base64 encoded body field that contains JSON
func unmarshalResponse(bodyBytes []byte) (map[string]interface{}, error) {
	// First unmarshal the outer response
	var response map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Get the base64 encoded body
	bodyStr, ok := response["body"].(string)
	if !ok {
		return nil, fmt.Errorf("body field is not a string")
	}

	// Decode the base64 body
	decodedBody, err := base64.StdEncoding.DecodeString(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 body: %w", err)
	}

	// Unmarshal the decoded body as JSON
	var responseBody map[string]interface{}
	if err := json.Unmarshal(decodedBody, &responseBody); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decoded body: %w", err)
	}

	// TODO: Clean up response and request files

	return responseBody, nil
}
