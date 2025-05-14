package messaging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/syftsdk"
)

type HttpMsgManager struct {
	sdk      *syftsdk.SyftSDK
	reqChan  chan *HttpRequestMsg
	respChan chan *HttpResponseMsg
	appSched *apps.AppScheduler
}

func NewHttpMsgManager(sdk *syftsdk.SyftSDK, appSched *apps.AppScheduler) (*HttpMsgManager, error) {
	return &HttpMsgManager{
		sdk:      sdk,
		reqChan:  make(chan *HttpRequestMsg),
		respChan: make(chan *HttpResponseMsg),
		appSched: appSched,
	}, nil
}

func (h *HttpMsgManager) Start(ctx context.Context) error {
	go h.handleRequests(ctx)
	return nil
}

func (h *HttpMsgManager) Stop() {
	close(h.reqChan)
	close(h.respChan)
}

func (h *HttpMsgManager) handleRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-h.reqChan:
			go h.processRequest(ctx, req)
		case resp := <-h.respChan:
			go h.processResponse(ctx, resp)
		}
	}
}

func (h *HttpMsgManager) processRequest(ctx context.Context, req *HttpRequestMsg) {
	app := h.appSched.GetApp(req.Message.AppName)

	if app == nil {
		h.respChan <- &HttpResponseMsg{
			Message: req.Message,
			Error:   fmt.Errorf("app not found"),
		}
		return
	}

	appUrl := app.GetEnv("SYFTBOX_APP_URL")
	appPort := app.GetEnv("SYFTBOX_APP_PORT")

	if appUrl == "" || appPort == "" {
		h.respChan <- &HttpResponseMsg{
			Message: req.Message,
			Error:   fmt.Errorf("app url or port not found"),
		}
		return
	}

	appEndpoint := fmt.Sprintf("http://%s:%s%s", appUrl, appPort, req.Message.AppEndpoint)

	httpReq, err := http.NewRequestWithContext(ctx, req.Message.Method, appEndpoint, bytes.NewReader(req.Message.Body))
	if err != nil {
		h.respChan <- &HttpResponseMsg{
			Message: req.Message,
			Error:   fmt.Errorf("failed to create request"),
		}
		return
	}

	httpReq.Header.Add("Content-Type", req.Message.ContentType)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		h.respChan <- &HttpResponseMsg{
			Message: req.Message,
			Error:   fmt.Errorf("failed to make request"),
		}
		return
	}

	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)

	if err != nil {
		h.respChan <- &HttpResponseMsg{
			Message: req.Message,
			Error:   fmt.Errorf("failed to read response body"),
		}
		return
	}

	// make a new http message to send to the server
	respMsg := &syftmsg.HttpMessage{
		From:        req.Message.To,
		To:          req.Message.From,
		SyftURI:     req.Message.SyftURI,
		AppName:     req.Message.AppName,
		AppEndpoint: req.Message.AppEndpoint,
		Method:      req.Message.Method,
		Body:        body,
		ContentType: req.Message.ContentType,
		Status:      strconv.Itoa(httpResp.StatusCode),
		RequestID:   req.Message.RequestID,
	}

	slog.Info("sending response to server", "message", respMsg.From, respMsg.To, respMsg.SyftURI, respMsg.AppName, respMsg.AppEndpoint, respMsg.Method, string(respMsg.Body), respMsg.ContentType, respMsg.Status)

	h.respChan <- &HttpResponseMsg{
		Message: respMsg,
		Error:   nil,
	}

}

func (h *HttpMsgManager) processResponse(ctx context.Context, resp *HttpResponseMsg) {
	slog.Info("processing response", "message", resp.Message)
	sendResp, err := h.sdk.SendMsg.Send(ctx, resp.Message, "response")
	if err != nil {
		slog.Error("failed to send response to server", "error", err)
	}
	slog.Info("response sent to server", "Response", sendResp)
}

func (h *HttpMsgManager) MakeAppRequest(ctx context.Context, msg *syftmsg.HttpMessage) {
	h.reqChan <- &HttpRequestMsg{
		Message: msg,
	}
}
