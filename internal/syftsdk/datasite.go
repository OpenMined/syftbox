package syftsdk

import (
	"context"

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

func (d *DatasiteAPI) GetView(ctx context.Context, params *DatasiteViewParams) (resp *DatasiteViewResponse, err error) {
	res, err := d.client.R().
		SetContext(ctx).
		SetSuccessResult(&resp).
		Get(v1View)

	if err := handleAPIError(res, err, "datasite view"); err != nil {
		return nil, err
	}

	return resp, nil
}
