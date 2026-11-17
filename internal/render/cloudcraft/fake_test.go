package cloudcraft

import (
	"context"

	cc "github.com/DataDog/cloudcraft-go"
)

// fakeAPI is a blueprintAPI that returns canned results and records the
// calls made to it.
type fakeAPI struct {
	blueprints []*cc.Blueprint
	listErr    error
	// bodies holds the raw JSON Get returns, by blueprint ID.
	bodies map[string]string
	// images holds the bytes ExportImage returns, by format.
	images    map[string][]byte
	createdID string
	createErr error
	updateErr error

	calls   []string
	created *cc.Blueprint
	updated *cc.Blueprint
	params  *cc.ImageExportParams
}

var _ blueprintAPI = (*fakeAPI)(nil)

func (f *fakeAPI) List(context.Context) ([]*cc.Blueprint, *cc.Response, error) {
	f.calls = append(f.calls, "list")
	return f.blueprints, nil, f.listErr
}

func (f *fakeAPI) Get(_ context.Context, id string) (*cc.Blueprint, *cc.Response, error) {
	f.calls = append(f.calls, "get "+id)
	return &cc.Blueprint{ID: id}, &cc.Response{Body: []byte(f.bodies[id])}, nil
}

func (f *fakeAPI) Create(_ context.Context, b *cc.Blueprint) (*cc.Blueprint, *cc.Response, error) {
	f.calls = append(f.calls, "create")
	f.created = b
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	return &cc.Blueprint{ID: f.createdID}, nil, nil
}

func (f *fakeAPI) Update(_ context.Context, b *cc.Blueprint, _ string) (*cc.Response, error) {
	f.calls = append(f.calls, "update "+b.ID)
	f.updated = b
	return nil, f.updateErr
}

func (f *fakeAPI) ExportImage(
	_ context.Context,
	id, format string,
	params *cc.ImageExportParams,
) ([]byte, *cc.Response, error) {
	f.calls = append(f.calls, "image "+id+" "+format)
	f.params = params
	return f.images[format], nil, nil
}
