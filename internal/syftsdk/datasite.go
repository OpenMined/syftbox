package syftsdk

import (
	"context"
	"fmt"

	"resty.dev/v3"
)

const (
	v1View = "/api/v1/datasite/view"
)

type DatasiteAPI struct {
	client *resty.Client
}

func newDatasiteAPI(client *resty.Client) *DatasiteAPI {
	return &DatasiteAPI{
		client: client,
	}
}

func (d *DatasiteAPI) GetView(ctx context.Context, params *DatasiteViewParams) (*DatasiteViewResponse, error) {
	var resp DatasiteViewResponse
	var sdkError SyftSDKError

	res, err := d.client.R().
		SetResult(&resp).
		SetError(&sdkError).
		SetContext(ctx).
		Get(v1View)

	if err != nil {
		return nil, err
	} else if res.IsError() {
		return nil, fmt.Errorf("error: %s", sdkError.Error)
	}

	return &resp, nil
}
