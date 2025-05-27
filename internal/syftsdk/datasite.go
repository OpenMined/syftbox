package syftsdk

import (
	"context"
	"fmt"

	"github.com/imroc/req/v3"
)

const (
	v1View = "/api/v1/datasite/view"
)

type DatasiteAPI struct {
	client *req.Client
}

func newDatasiteAPI(client *req.Client) *DatasiteAPI {
	return &DatasiteAPI{
		client: client,
	}
}

func (d *DatasiteAPI) GetView(ctx context.Context, params *DatasiteViewParams) (*DatasiteViewResponse, error) {
	var resp DatasiteViewResponse
	var sdkError SyftSDKError

	res, err := d.client.R().
		SetSuccessResult(&resp).
		SetErrorResult(&sdkError).
		SetContext(ctx).
		Get(v1View)

	if err != nil {
		return nil, fmt.Errorf("sdk: datasite view: %w", err)
	}

	if res.IsErrorState() {
		return nil, fmt.Errorf("sdk: datasite view: %s %s", res.StatusCode, sdkError.Error)
	}

	return &resp, nil
}
